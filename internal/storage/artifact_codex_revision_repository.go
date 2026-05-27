package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/schemas"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

type ArtifactCodexRevisionInput struct {
	ProjectID   string
	ArtifactID  string
	Instruction string
	Executor    CodexExecutor
}

type ArtifactCodexRevisionResult struct {
	Artifact       ArtifactVersionRecord `json:"artifact"`
	EnvironmentID  string                `json:"environment_id,omitempty"`
	WorkspaceRoot  string                `json:"workspace_root,omitempty"`
	Classification string                `json:"classification"`
	Summary        string                `json:"summary,omitempty"`
}

func (db *DB) ReviseArtifactWithCodex(ctx context.Context, input ArtifactCodexRevisionInput) (ArtifactCodexRevisionResult, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	artifactID := strings.TrimSpace(input.ArtifactID)
	instruction := strings.TrimSpace(input.Instruction)
	if projectID == "" || artifactID == "" {
		return ArtifactCodexRevisionResult{}, fmt.Errorf("project id and artifact id are required")
	}
	if instruction == "" {
		return ArtifactCodexRevisionResult{}, fmt.Errorf("revision instruction is required")
	}
	executor := input.Executor
	if executor == nil {
		executor = LocalCodexExecutor{}
	}
	artifact, err := db.latestArtifactWithContent(ctx, projectID, artifactID)
	if err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	if strings.TrimSpace(artifact.Content) == "" {
		return ArtifactCodexRevisionResult{}, fmt.Errorf("artifact content is unavailable: %s", artifactID)
	}
	env, err := db.ResolveImplementationEnvironment(ctx, projectID)
	if err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	if usesLocalCodexRuntime(executor) || env.OSFamily == platform.OSFamilyWindows {
		classification, blockers := evaluateRealCodexEnvironment(env, realCodexRuntimeGOOS)
		if len(blockers) > 0 {
			return ArtifactCodexRevisionResult{EnvironmentID: env.ID, Classification: classification}, fmt.Errorf("%s: %s", classification, strings.Join(blockers, "; "))
		}
	}
	runPolicy := db.activeRunProfileNetworkPolicy(ctx, projectID)
	if usesLocalCodexRuntime(executor) {
		doctorReport := runRealCodexDoctor(ctx, env, toolchains.Options{IncludeCodex: true})
		if err := db.SaveToolchainReport(ctx, projectID, doctorReport); err != nil {
			return ArtifactCodexRevisionResult{}, err
		}
		if blockers := realCodexToolchainBlockers(doctorReport); len(blockers) > 0 {
			return ArtifactCodexRevisionResult{EnvironmentID: env.ID, Classification: "toolchain_required"}, fmt.Errorf("toolchain_required: %s", strings.Join(blockers, "; "))
		}
	}
	workspaceRoot, artifactFile, err := db.prepareArtifactRevisionWorkspace(projectID, artifact)
	if err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	prompt := artifactRevisionPrompt(artifact, filepath.Base(artifactFile), instruction)
	execResult, err := executor.ExecCodex(ctx, CodexExecRequest{
		ProjectRoot:   workspaceRoot,
		Prompt:        prompt,
		NetworkPolicy: runPolicy,
		SandboxMode:   codexSandboxMode(env),
	})
	if err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	if execResult.ExitCode != 0 {
		classification, blockers := classifyCodexExecResult(execResult)
		_ = db.insertWorkflowEventNow(ctx, projectID, "artifact_codex_revision_blocked", map[string]any{
			"artifact_id":    artifactID,
			"environment_id": env.ID,
			"classification": classification,
			"blockers":       blockers,
		})
		return ArtifactCodexRevisionResult{EnvironmentID: env.ID, WorkspaceRoot: workspaceRoot, Classification: classification}, fmt.Errorf("%s: %s", classification, strings.TrimSpace(execResult.Stderr))
	}
	if err := validateArtifactRevisionFinalMessage(execResult.FinalMessage); err != nil {
		return ArtifactCodexRevisionResult{EnvironmentID: env.ID, WorkspaceRoot: workspaceRoot, Classification: "schema_validation_failed"}, err
	}
	revised, err := os.ReadFile(artifactFile)
	if err != nil {
		return ArtifactCodexRevisionResult{}, fmt.Errorf("read revised artifact: %w", err)
	}
	if bytes.Equal(revised, []byte(artifact.Content)) {
		return ArtifactCodexRevisionResult{EnvironmentID: env.ID, WorkspaceRoot: workspaceRoot, Classification: "no_change"}, fmt.Errorf("codex did not change artifact content")
	}
	record, err := db.SaveArtifactRevision(ctx, projectID, artifactID, revised)
	if err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	summary := codexFinalSummary(execResult.FinalMessage)
	if err := db.insertWorkflowEventNow(ctx, projectID, "artifact_codex_revision_created", map[string]any{
		"artifact_id":    artifactID,
		"version_id":     record.VersionID,
		"environment_id": env.ID,
		"instruction":    truncateForEvidence(instruction, 500),
		"summary":        truncateForEvidence(summary, 500),
	}); err != nil {
		return ArtifactCodexRevisionResult{}, err
	}
	return ArtifactCodexRevisionResult{
		Artifact:       record,
		EnvironmentID:  env.ID,
		WorkspaceRoot:  workspaceRoot,
		Classification: "succeeded",
		Summary:        summary,
	}, nil
}

func (db *DB) latestArtifactWithContent(ctx context.Context, projectID string, artifactID string) (ArtifactRecord, error) {
	artifacts, err := db.ListArtifactsWithContent(ctx, projectID, "")
	if err != nil {
		return ArtifactRecord{}, err
	}
	for _, artifact := range artifacts {
		if artifact.ArtifactID == artifactID {
			return artifact, nil
		}
	}
	return ArtifactRecord{}, fmt.Errorf("artifact not found: %s", artifactID)
}

func (db *DB) prepareArtifactRevisionWorkspace(projectID string, artifact ArtifactRecord) (string, string, error) {
	parent := filepath.Join(db.dataRoot, "projects", projectID, "artifact-codex-revisions")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", err
	}
	workspaceRoot, err := os.MkdirTemp(parent, "revision-*")
	if err != nil {
		return "", "", err
	}
	name := filepath.Base(filepath.Clean(artifact.Path))
	if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
		name = "artifact.md"
	}
	artifactFile := filepath.Join(workspaceRoot, name)
	if err := os.WriteFile(artifactFile, []byte(artifact.Content), 0o644); err != nil {
		return "", "", err
	}
	return workspaceRoot, artifactFile, nil
}

func artifactRevisionPrompt(artifact ArtifactRecord, workspaceFile string, instruction string) string {
	return fmt.Sprintf(`You are revising a local-first project artifact for human review.

Edit only the file %q in this workspace. Do not create or modify any other files.

Original artifact path: %s
Artifact type: %s
Latest version: %d

Requested change:
%s

Keep the existing file format and preserve useful structure. Apply the requested change directly to %q.
Do not introduce internal product names such as DevOS, Orchestrator, or Human Inbox unless the requested change explicitly asks for them.

When complete, respond with JSON that matches the provided final-message schema. Use status "succeeded" only after the file has been updated. Use a test entry with command "artifact revision" and status "passed" when no external command was needed.
`, workspaceFile, artifact.Path, artifact.ArtifactType, artifact.LatestVersion, instruction, workspaceFile)
}

func validateArtifactRevisionFinalMessage(raw string) error {
	final, err := schemas.ParseCodexFinalMessage(raw)
	if err != nil {
		return err
	}
	if final.Status != "succeeded" {
		return fmt.Errorf("codex final status is %s", final.Status)
	}
	if len(final.Blockers) > 0 {
		return fmt.Errorf("codex reported blockers: %s", strings.Join(final.Blockers, "; "))
	}
	for _, test := range final.Tests {
		if test.Status != "passed" {
			return fmt.Errorf("codex reported non-passing check %q: %s", test.Command, test.Status)
		}
	}
	return nil
}

func codexFinalSummary(raw string) string {
	final, err := schemas.ParseCodexFinalMessage(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(final.Summary)
}

func truncateForEvidence(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
