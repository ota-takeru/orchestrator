package schemas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SchemaKind string

const (
	SchemaKindTaskYAML           SchemaKind = "task_yaml"
	SchemaKindCodexFinalMessage  SchemaKind = "codex_final_message"
	SchemaKindSemanticDiff       SchemaKind = "semantic_behavior_diff"
	SchemaKindDependencyRisk     SchemaKind = "dependency_risk_ledger"
	SchemaKindHumanInboxSnapshot SchemaKind = "human_inbox_snapshot"
	SchemaKindGateResult         SchemaKind = "gate_result"
	SchemaKindToolchainReport    SchemaKind = "toolchain_report"
	SchemaKindCodexReadiness     SchemaKind = "codex_runtime_readiness"
)

type Definition struct {
	ID      string     `json:"id"`
	Kind    SchemaKind `json:"kind"`
	Version string     `json:"version"`
	File    string     `json:"file"`
	Content []byte     `json:"-"`
}

type Manifest struct {
	GeneratedAt string                `json:"generated_at"`
	Schemas     []ManifestSchemaEntry `json:"schemas"`
}

type ManifestSchemaEntry struct {
	ID           string     `json:"id"`
	Kind         SchemaKind `json:"kind"`
	Version      string     `json:"version"`
	File         string     `json:"file"`
	SHA256       string     `json:"sha256"`
	OwnedBy      string     `json:"owned_by"`
	TrustLevel   string     `json:"trust_level"`
	WritableByAI bool       `json:"writable_by_ai"`
}

type InstallResult struct {
	Root         string   `json:"root"`
	ManifestPath string   `json:"manifest_path"`
	CreatedPaths []string `json:"created_paths"`
	UpdatedPaths []string `json:"updated_paths"`
}

type ValidationResult struct {
	Root     string   `json:"root"`
	Valid    bool     `json:"valid"`
	Findings []string `json:"findings,omitempty"`
}

func Definitions() []Definition {
	return []Definition{
		{
			ID:      "devos.task-yaml",
			Kind:    SchemaKindTaskYAML,
			Version: "2",
			File:    "task-yaml.v2.schema.json",
			Content: []byte(taskYAMLSchema),
		},
		{
			ID:      "devos.codex-final-message",
			Kind:    SchemaKindCodexFinalMessage,
			Version: "1",
			File:    "codex-final-message.v1.schema.json",
			Content: []byte(codexFinalMessageSchema),
		},
		{
			ID:      "devos.semantic-behavior-diff",
			Kind:    SchemaKindSemanticDiff,
			Version: "1",
			File:    "semantic-behavior-diff.v1.schema.json",
			Content: []byte(semanticBehaviorDiffSchema),
		},
		{
			ID:      "devos.dependency-risk-ledger",
			Kind:    SchemaKindDependencyRisk,
			Version: "1",
			File:    "dependency-risk-ledger.v1.schema.json",
			Content: []byte(dependencyRiskLedgerSchema),
		},
		{
			ID:      "devos.human-inbox-snapshot",
			Kind:    SchemaKindHumanInboxSnapshot,
			Version: "1",
			File:    "human-inbox-snapshot.v1.schema.json",
			Content: []byte(humanInboxSnapshotSchema),
		},
		{
			ID:      "devos.gate-result",
			Kind:    SchemaKindGateResult,
			Version: "1",
			File:    "gate-result.v1.schema.json",
			Content: []byte(gateResultSchema),
		},
		{
			ID:      "devos.toolchain-report",
			Kind:    SchemaKindToolchainReport,
			Version: "1",
			File:    "toolchain-report.v1.schema.json",
			Content: []byte(toolchainReportSchema),
		},
		{
			ID:      "devos.codex-runtime-readiness",
			Kind:    SchemaKindCodexReadiness,
			Version: "1",
			File:    "codex-runtime-readiness.v1.schema.json",
			Content: []byte(codexRuntimeReadinessSchema),
		},
	}
}

type CodexFinalMessage struct {
	Status   string                 `json:"status"`
	Summary  string                 `json:"summary"`
	Tests    []CodexFinalTestResult `json:"tests,omitempty"`
	Blockers []string               `json:"blockers,omitempty"`
}

type CodexFinalTestResult struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Notes   string `json:"notes,omitempty"`
}

func CodexFinalMessageSchema() []byte {
	return []byte(codexFinalMessageSchema)
}

func ValidateCodexFinalMessage(raw string) error {
	var message CodexFinalMessage
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		return fmt.Errorf("codex final message must be JSON: %w", err)
	}
	switch message.Status {
	case "succeeded", "blocked", "failed":
	default:
		return fmt.Errorf("codex final message has invalid status: %s", message.Status)
	}
	if strings.TrimSpace(message.Summary) == "" {
		return fmt.Errorf("codex final message requires summary")
	}
	for i, test := range message.Tests {
		if strings.TrimSpace(test.Command) == "" {
			return fmt.Errorf("codex final message test %d requires command", i)
		}
		switch test.Status {
		case "passed", "failed", "not_run":
		default:
			return fmt.Errorf("codex final message test %d has invalid status: %s", i, test.Status)
		}
	}
	return nil
}

type SemanticBehaviorDiffItem struct {
	Category   string                         `json:"category"`
	Summary    string                         `json:"summary"`
	Confidence string                         `json:"confidence"`
	Evidence   []SemanticBehaviorDiffEvidence `json:"evidence"`
}

type SemanticBehaviorDiffEvidence struct {
	File       string `json:"file"`
	ChangeType string `json:"change_type"`
	Source     string `json:"source"`
	Generated  bool   `json:"generated"`
}

func SemanticBehaviorDiffSchema() []byte {
	return []byte(semanticBehaviorDiffSchema)
}

func ValidateSemanticBehaviorDiff(raw string) error {
	var items []SemanticBehaviorDiffItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return fmt.Errorf("semantic behavior diff must be JSON array: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("semantic behavior diff requires at least one item")
	}
	for i, item := range items {
		switch item.Category {
		case "user_visible", "non_user_visible", "risk", "test_change":
		default:
			return fmt.Errorf("semantic behavior diff item %d has invalid category: %s", i, item.Category)
		}
		if strings.TrimSpace(item.Summary) == "" {
			return fmt.Errorf("semantic behavior diff item %d requires summary", i)
		}
		switch item.Confidence {
		case "high", "medium", "low":
		default:
			return fmt.Errorf("semantic behavior diff item %d has invalid confidence: %s", i, item.Confidence)
		}
		for j, evidence := range item.Evidence {
			if strings.TrimSpace(evidence.File) == "" {
				return fmt.Errorf("semantic behavior diff item %d evidence %d requires file", i, j)
			}
			switch evidence.ChangeType {
			case "added", "modified", "deleted", "renamed":
			default:
				return fmt.Errorf("semantic behavior diff item %d evidence %d has invalid change type: %s", i, j, evidence.ChangeType)
			}
			if strings.TrimSpace(evidence.Source) == "" {
				return fmt.Errorf("semantic behavior diff item %d evidence %d requires source", i, j)
			}
		}
	}
	return nil
}

type DependencyRiskLedgerEntry struct {
	ID                 string `json:"id"`
	ProjectID          string `json:"project_id"`
	Name               string `json:"name"`
	PackageManager     string `json:"package_manager"`
	DependencyType     string `json:"dependency_type"`
	IntroducedByTaskID string `json:"introduced_by_task_id,omitempty"`
	IntroducedByRunID  string `json:"introduced_by_run_id,omitempty"`
	DecisionID         string `json:"decision_id,omitempty"`
	Reason             string `json:"reason"`
	ApprovedBy         string `json:"approved_by,omitempty"`
	Risk               string `json:"risk"`
	LockfileChanged    bool   `json:"lockfile_changed"`
	LifecycleScripts   string `json:"lifecycle_scripts"`
	CurrentVersion     string `json:"current_version,omitempty"`
	ApprovedScope      string `json:"approved_scope"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

func DependencyRiskLedgerSchema() []byte {
	return []byte(dependencyRiskLedgerSchema)
}

func ValidateDependencyRiskLedgerEntry(raw string) error {
	var entry DependencyRiskLedgerEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return fmt.Errorf("dependency risk ledger entry must be JSON: %w", err)
	}
	for _, required := range []struct {
		field string
		value string
	}{
		{"id", entry.ID},
		{"project_id", entry.ProjectID},
		{"name", entry.Name},
		{"package_manager", entry.PackageManager},
		{"dependency_type", entry.DependencyType},
		{"reason", entry.Reason},
		{"risk", entry.Risk},
		{"lifecycle_scripts", entry.LifecycleScripts},
		{"approved_scope", entry.ApprovedScope},
		{"created_at", entry.CreatedAt},
		{"updated_at", entry.UpdatedAt},
	} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("dependency risk ledger entry requires %s", required.field)
		}
	}
	switch entry.PackageManager {
	case "go", "npm", "pnpm", "yarn", "cargo", "other":
	default:
		return fmt.Errorf("dependency risk ledger entry has invalid package manager: %s", entry.PackageManager)
	}
	switch entry.DependencyType {
	case "production", "development", "tool":
	default:
		return fmt.Errorf("dependency risk ledger entry has invalid dependency type: %s", entry.DependencyType)
	}
	switch entry.Risk {
	case "low", "medium", "high", "critical":
	default:
		return fmt.Errorf("dependency risk ledger entry has invalid risk: %s", entry.Risk)
	}
	switch entry.LifecycleScripts {
	case "none_detected", "detected", "unknown":
	default:
		return fmt.Errorf("dependency risk ledger entry has invalid lifecycle scripts: %s", entry.LifecycleScripts)
	}
	switch entry.ApprovedScope {
	case "project", "task", "one_time", "dependency_family":
	default:
		return fmt.Errorf("dependency risk ledger entry has invalid approved scope: %s", entry.ApprovedScope)
	}
	return nil
}

type HumanInboxSnapshot struct {
	ProjectID               string            `json:"project_id"`
	GeneratedAt             string            `json:"generated_at"`
	Counts                  HumanInboxCounts  `json:"counts"`
	LastSuccessfulMergeAt   string            `json:"last_successful_merge_at,omitempty"`
	OpenInboxItems          []json.RawMessage `json:"open_inbox_items"`
	RecommendedNextCommands []string          `json:"recommended_next_commands,omitempty"`
}

type HumanInboxCounts struct {
	OpenInboxItems       int `json:"open_inbox_items"`
	RunningTasks         int `json:"running_tasks"`
	WaitingForHumanTasks int `json:"waiting_for_human_tasks"`
	BlockedTasks         int `json:"blocked_tasks"`
	QueuedRequests       int `json:"queued_requests"`
	OpenDecisions        int `json:"open_decisions"`
	RunningWorkers       int `json:"running_workers"`
	OpenMergeQueue       int `json:"open_merge_queue"`
	BaselineIssues       int `json:"baseline_issues"`
}

func HumanInboxSnapshotSchema() []byte {
	return []byte(humanInboxSnapshotSchema)
}

func ValidateHumanInboxSnapshot(raw string) error {
	var snapshot HumanInboxSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return fmt.Errorf("human inbox snapshot must be JSON: %w", err)
	}
	if strings.TrimSpace(snapshot.ProjectID) == "" {
		return fmt.Errorf("human inbox snapshot requires project_id")
	}
	if strings.TrimSpace(snapshot.GeneratedAt) == "" {
		return fmt.Errorf("human inbox snapshot requires generated_at")
	}
	if _, err := time.Parse(time.RFC3339Nano, snapshot.GeneratedAt); err != nil {
		return fmt.Errorf("human inbox snapshot generated_at must be RFC3339Nano: %w", err)
	}
	if snapshot.Counts.OpenInboxItems < 0 ||
		snapshot.Counts.RunningTasks < 0 ||
		snapshot.Counts.WaitingForHumanTasks < 0 ||
		snapshot.Counts.BlockedTasks < 0 ||
		snapshot.Counts.QueuedRequests < 0 ||
		snapshot.Counts.OpenDecisions < 0 ||
		snapshot.Counts.RunningWorkers < 0 ||
		snapshot.Counts.OpenMergeQueue < 0 ||
		snapshot.Counts.BaselineIssues < 0 {
		return fmt.Errorf("human inbox snapshot counts cannot be negative")
	}
	if len(snapshot.OpenInboxItems) > snapshot.Counts.OpenInboxItems {
		return fmt.Errorf("human inbox snapshot includes more open inbox items than counted")
	}
	for i, item := range snapshot.OpenInboxItems {
		if len(item) == 0 || string(item) == "null" {
			return fmt.Errorf("human inbox snapshot open inbox item %d is empty", i)
		}
	}
	for i, command := range snapshot.RecommendedNextCommands {
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("human inbox snapshot recommended command %d is empty", i)
		}
	}
	return nil
}

type GateResult struct {
	Status          string          `json:"status"`
	Severity        string          `json:"severity"`
	Detector        string          `json:"detector"`
	HumanActionType string          `json:"human_action_type,omitempty"`
	Evidence        json.RawMessage `json:"evidence"`
}

func GateResultSchema() []byte {
	return []byte(gateResultSchema)
}

func ValidateGateResult(raw string) error {
	var result GateResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return fmt.Errorf("gate result must be JSON: %w", err)
	}
	switch result.Status {
	case "PASS", "AUTO_REPAIR", "AUTO_REPLAN", "REPORT_ONLY", "HUMAN_INPUT", "HUMAN_DECISION", "HARD_BLOCK":
	default:
		return fmt.Errorf("gate result has invalid status: %s", result.Status)
	}
	switch result.Severity {
	case "low", "medium", "high", "critical":
	default:
		return fmt.Errorf("gate result has invalid severity: %s", result.Severity)
	}
	if strings.TrimSpace(result.Detector) == "" {
		return fmt.Errorf("gate result requires detector")
	}
	if len(result.Evidence) == 0 || string(result.Evidence) == "null" {
		return fmt.Errorf("gate result requires evidence")
	}
	var evidence any
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil {
		return fmt.Errorf("gate result evidence must be JSON: %w", err)
	}
	if values, ok := evidence.([]any); ok && len(values) == 0 {
		return fmt.Errorf("gate result evidence array cannot be empty")
	}
	return nil
}

type ToolchainReport struct {
	EnvironmentID string                 `json:"environment_id"`
	Requirements  []ToolchainRequirement `json:"requirements"`
}

type ToolchainRequirement struct {
	ToolchainKey     string `json:"toolchain_key"`
	RequiredFor      string `json:"required_for"`
	RequiredForMerge bool   `json:"required_for_merge"`
	Status           string `json:"status"`
	Executable       string `json:"executable,omitempty"`
	DetectedPath     string `json:"detected_path,omitempty"`
	Message          string `json:"message"`
}

func ToolchainReportSchema() []byte {
	return []byte(toolchainReportSchema)
}

func ValidateToolchainReport(raw string) error {
	var report ToolchainReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return fmt.Errorf("toolchain report must be JSON: %w", err)
	}
	if strings.TrimSpace(report.EnvironmentID) == "" {
		return fmt.Errorf("toolchain report requires environment_id")
	}
	if len(report.Requirements) == 0 {
		return fmt.Errorf("toolchain report requires at least one requirement")
	}
	seen := map[string]struct{}{}
	for i, req := range report.Requirements {
		if strings.TrimSpace(req.ToolchainKey) == "" {
			return fmt.Errorf("toolchain report requirement %d requires toolchain_key", i)
		}
		key := req.ToolchainKey + "|" + req.RequiredFor
		if _, ok := seen[key]; ok {
			return fmt.Errorf("toolchain report has duplicate requirement: %s", key)
		}
		seen[key] = struct{}{}
		switch req.RequiredFor {
		case "implementation", "verification", "runtime", "runtime_smoke", "deployment":
		default:
			return fmt.Errorf("toolchain report requirement %d has invalid required_for: %s", i, req.RequiredFor)
		}
		switch req.Status {
		case "detected", "missing", "invalid", "setup_required", "waived", "unsupported", "revoked":
		default:
			return fmt.Errorf("toolchain report requirement %d has invalid status: %s", i, req.Status)
		}
		if strings.TrimSpace(req.Message) == "" {
			return fmt.Errorf("toolchain report requirement %d requires message", i)
		}
	}
	return nil
}

type CodexRuntimeReadiness struct {
	HostGOOS string                      `json:"host_goos"`
	Items    []CodexRuntimeReadinessItem `json:"items"`
}

type CodexRuntimeReadinessItem struct {
	EnvironmentID        string   `json:"environment_id"`
	OSFamily             string   `json:"os_family"`
	ProjectRoot          string   `json:"project_root"`
	CodexAdapter         string   `json:"codex_adapter"`
	SandboxProfile       string   `json:"sandbox_profile"`
	ExpectedHostRuntime  string   `json:"expected_host_runtime"`
	CurrentRuntimeUsable bool     `json:"current_runtime_usable"`
	Classification       string   `json:"classification"`
	Blockers             []string `json:"blockers,omitempty"`
	Argv                 []string `json:"argv,omitempty"`
}

func CodexRuntimeReadinessSchema() []byte {
	return []byte(codexRuntimeReadinessSchema)
}

func ValidateCodexRuntimeReadiness(raw string) error {
	var report CodexRuntimeReadiness
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return fmt.Errorf("codex runtime readiness must be JSON: %w", err)
	}
	if strings.TrimSpace(report.HostGOOS) == "" {
		return fmt.Errorf("codex runtime readiness requires host_goos")
	}
	seen := map[string]struct{}{}
	for i, item := range report.Items {
		if strings.TrimSpace(item.EnvironmentID) == "" {
			return fmt.Errorf("codex runtime readiness item %d requires environment_id", i)
		}
		if _, ok := seen[item.EnvironmentID]; ok {
			return fmt.Errorf("codex runtime readiness has duplicate environment: %s", item.EnvironmentID)
		}
		seen[item.EnvironmentID] = struct{}{}
		for _, required := range []struct {
			field string
			value string
		}{
			{"os_family", item.OSFamily},
			{"project_root", item.ProjectRoot},
			{"codex_adapter", item.CodexAdapter},
			{"sandbox_profile", item.SandboxProfile},
			{"expected_host_runtime", item.ExpectedHostRuntime},
			{"classification", item.Classification},
		} {
			if strings.TrimSpace(required.value) == "" {
				return fmt.Errorf("codex runtime readiness item %d requires %s", i, required.field)
			}
		}
		if item.CurrentRuntimeUsable && len(item.Blockers) > 0 {
			return fmt.Errorf("codex runtime readiness item %d cannot be usable with blockers", i)
		}
		if !item.CurrentRuntimeUsable && len(item.Blockers) == 0 {
			return fmt.Errorf("codex runtime readiness item %d requires blockers when not usable", i)
		}
		for j, blocker := range item.Blockers {
			if strings.TrimSpace(blocker) == "" {
				return fmt.Errorf("codex runtime readiness item %d blocker %d is empty", i, j)
			}
		}
		for j, arg := range item.Argv {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("codex runtime readiness item %d argv %d is empty", i, j)
			}
		}
	}
	return nil
}

func Install(projectRoot string) (InstallResult, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return InstallResult{}, fmt.Errorf("project root is required")
	}
	root := filepath.Join(projectRoot, ".devagent", "schemas")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{Root: root, ManifestPath: filepath.Join(root, "registry.manifest.json")}
	defs := Definitions()
	for _, def := range defs {
		path := filepath.Join(root, def.File)
		existing, err := os.ReadFile(path)
		switch {
		case err == nil && string(existing) == string(def.Content):
			continue
		case err == nil:
			if err := os.WriteFile(path, def.Content, 0o644); err != nil {
				return InstallResult{}, err
			}
			result.UpdatedPaths = append(result.UpdatedPaths, path)
		case os.IsNotExist(err):
			if err := os.WriteFile(path, def.Content, 0o644); err != nil {
				return InstallResult{}, err
			}
			result.CreatedPaths = append(result.CreatedPaths, path)
		default:
			return InstallResult{}, err
		}
	}
	manifest, err := json.MarshalIndent(manifestFor(defs), "", "  ")
	if err != nil {
		return InstallResult{}, err
	}
	manifest = append(manifest, '\n')
	existing, err := os.ReadFile(result.ManifestPath)
	switch {
	case err == nil && string(existing) == string(manifest):
	case err == nil:
		if err := os.WriteFile(result.ManifestPath, manifest, 0o644); err != nil {
			return InstallResult{}, err
		}
		result.UpdatedPaths = append(result.UpdatedPaths, result.ManifestPath)
	case os.IsNotExist(err):
		if err := os.WriteFile(result.ManifestPath, manifest, 0o644); err != nil {
			return InstallResult{}, err
		}
		result.CreatedPaths = append(result.CreatedPaths, result.ManifestPath)
	default:
		return InstallResult{}, err
	}
	sort.Strings(result.CreatedPaths)
	sort.Strings(result.UpdatedPaths)
	return result, nil
}

func ValidateInstalled(projectRoot string) ValidationResult {
	root := filepath.Join(projectRoot, ".devagent", "schemas")
	result := ValidationResult{Root: root, Valid: true}
	manifestPath := filepath.Join(root, "registry.manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		result.Valid = false
		result.Findings = append(result.Findings, "schema registry manifest is missing or unreadable")
		return result
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		result.Valid = false
		result.Findings = append(result.Findings, "schema registry manifest is invalid JSON")
		return result
	}
	expected := manifestFor(Definitions())
	expectedByFile := map[string]ManifestSchemaEntry{}
	for _, entry := range expected.Schemas {
		expectedByFile[entry.File] = entry
	}
	seen := map[string]struct{}{}
	for _, entry := range manifest.Schemas {
		expectedEntry, ok := expectedByFile[entry.File]
		if !ok {
			result.Valid = false
			result.Findings = append(result.Findings, "unknown schema in manifest: "+entry.File)
			continue
		}
		seen[entry.File] = struct{}{}
		if entry.ID != expectedEntry.ID || entry.Version != expectedEntry.Version || entry.SHA256 != expectedEntry.SHA256 {
			result.Valid = false
			result.Findings = append(result.Findings, "schema manifest mismatch: "+entry.File)
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, entry.File))
		if err != nil {
			result.Valid = false
			result.Findings = append(result.Findings, "schema file is missing: "+entry.File)
			continue
		}
		if sha256Hex(content) != entry.SHA256 {
			result.Valid = false
			result.Findings = append(result.Findings, "schema checksum mismatch: "+entry.File)
		}
	}
	for file := range expectedByFile {
		if _, ok := seen[file]; !ok {
			result.Valid = false
			result.Findings = append(result.Findings, "schema missing from manifest: "+file)
		}
	}
	return result
}

func manifestFor(defs []Definition) Manifest {
	entries := make([]ManifestSchemaEntry, 0, len(defs))
	for _, def := range defs {
		entries = append(entries, ManifestSchemaEntry{
			ID:           def.ID,
			Kind:         def.Kind,
			Version:      def.Version,
			File:         def.File,
			SHA256:       sha256Hex(def.Content),
			OwnedBy:      "orchestrator",
			TrustLevel:   "trusted_after_validation",
			WritableByAI: false,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].File < entries[j].File
	})
	return Manifest{GeneratedAt: "static", Schemas: entries}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func StaticGeneratedAt() string {
	return time.Unix(0, 0).UTC().Format(time.RFC3339)
}

const taskYAMLSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.task-yaml.v2",
  "title": "DevOS Task YAML v2",
  "type": "object",
  "required": ["id", "title"],
  "additionalProperties": true,
  "properties": {
    "id": { "type": "string", "pattern": "^[A-Z][A-Z0-9_-]*-[A-Z0-9_-]+$" },
    "title": { "type": "string", "minLength": 1 },
    "base_branch": { "type": "string", "minLength": 1 },
    "verification_commands": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "environment", "runner", "required_for_merge", "working_dir", "command"],
        "additionalProperties": true,
        "properties": {
          "id": { "type": "string", "minLength": 1 },
          "environment": { "type": "string", "minLength": 1 },
          "runner": { "type": "string", "minLength": 1 },
          "required_for_merge": { "type": "boolean" },
          "working_dir": { "type": "string", "minLength": 1 },
          "command": {
            "type": "object",
            "required": ["argv"],
            "properties": {
              "argv": {
                "type": "array",
                "minItems": 1,
                "items": { "type": "string", "minLength": 1 }
              }
            }
          },
          "timeout": { "type": "string" },
          "network": { "type": "boolean" },
          "required_toolchains": {
            "type": "array",
            "items": { "type": "string", "minLength": 1 }
          }
        }
      }
    }
  }
}`

const codexFinalMessageSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.codex-final-message.v1",
  "title": "DevOS Codex Final Message",
  "type": "object",
  "required": ["summary", "status"],
  "additionalProperties": false,
  "properties": {
    "status": { "type": "string", "enum": ["succeeded", "blocked", "failed"] },
    "summary": { "type": "string" },
    "tests": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["command", "status"],
        "additionalProperties": false,
        "properties": {
          "command": { "type": "string" },
          "status": { "type": "string", "enum": ["passed", "failed", "not_run"] },
          "notes": { "type": "string" }
        }
      }
    },
    "blockers": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}`

const semanticBehaviorDiffSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.semantic-behavior-diff.v1",
  "title": "DevOS Semantic Behavior Diff",
  "type": "array",
  "minItems": 1,
  "items": {
    "type": "object",
    "required": ["category", "summary", "confidence", "evidence"],
    "additionalProperties": false,
    "properties": {
      "category": { "type": "string", "enum": ["user_visible", "non_user_visible", "risk", "test_change"] },
      "summary": { "type": "string", "minLength": 1 },
      "confidence": { "type": "string", "enum": ["high", "medium", "low"] },
      "evidence": {
        "type": "array",
        "items": {
          "type": "object",
          "required": ["file", "change_type", "source", "generated"],
          "additionalProperties": false,
          "properties": {
            "file": { "type": "string", "minLength": 1 },
            "change_type": { "type": "string", "enum": ["added", "modified", "deleted", "renamed"] },
            "source": { "type": "string", "minLength": 1 },
            "generated": { "type": "boolean" }
          }
        }
      }
    }
  }
}`

const dependencyRiskLedgerSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.dependency-risk-ledger.v1",
  "title": "DevOS Dependency Risk Ledger Entry",
  "type": "object",
  "required": ["id", "project_id", "name", "package_manager", "dependency_type", "reason", "risk", "lockfile_changed", "lifecycle_scripts", "approved_scope", "created_at", "updated_at"],
  "additionalProperties": false,
  "properties": {
    "id": { "type": "string", "pattern": "^DEPRISK-[A-Z0-9]+$" },
    "project_id": { "type": "string", "minLength": 1 },
    "name": { "type": "string", "minLength": 1 },
    "package_manager": { "type": "string", "enum": ["go", "npm", "pnpm", "yarn", "cargo", "other"] },
    "dependency_type": { "type": "string", "enum": ["production", "development", "tool"] },
    "introduced_by_task_id": { "type": "string" },
    "introduced_by_run_id": { "type": "string" },
    "decision_id": { "type": "string" },
    "reason": { "type": "string", "minLength": 1 },
    "approved_by": { "type": "string" },
    "risk": { "type": "string", "enum": ["low", "medium", "high", "critical"] },
    "lockfile_changed": { "type": "boolean" },
    "lifecycle_scripts": { "type": "string", "enum": ["none_detected", "detected", "unknown"] },
    "current_version": { "type": "string" },
    "approved_scope": { "type": "string", "enum": ["project", "task", "one_time", "dependency_family"] },
    "expires_at": { "type": "string" },
    "created_at": { "type": "string", "minLength": 1 },
    "updated_at": { "type": "string", "minLength": 1 }
  }
}`

const humanInboxSnapshotSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.human-inbox-snapshot.v1",
  "title": "DevOS Human Inbox Snapshot",
  "type": "object",
  "required": ["project_id", "generated_at", "counts", "open_inbox_items"],
  "additionalProperties": false,
  "properties": {
    "project_id": { "type": "string", "minLength": 1 },
    "generated_at": { "type": "string", "minLength": 1 },
    "counts": {
      "type": "object",
      "required": [
        "open_inbox_items",
        "running_tasks",
        "waiting_for_human_tasks",
        "blocked_tasks",
        "queued_requests",
        "open_decisions",
        "running_workers",
        "open_merge_queue",
        "baseline_issues"
      ],
      "additionalProperties": false,
      "properties": {
        "open_inbox_items": { "type": "integer", "minimum": 0 },
        "running_tasks": { "type": "integer", "minimum": 0 },
        "waiting_for_human_tasks": { "type": "integer", "minimum": 0 },
        "blocked_tasks": { "type": "integer", "minimum": 0 },
        "queued_requests": { "type": "integer", "minimum": 0 },
        "open_decisions": { "type": "integer", "minimum": 0 },
        "running_workers": { "type": "integer", "minimum": 0 },
        "open_merge_queue": { "type": "integer", "minimum": 0 },
        "baseline_issues": { "type": "integer", "minimum": 0 }
      }
    },
    "last_successful_merge_at": { "type": "string" },
    "open_inbox_items": {
      "type": "array",
      "items": { "type": "object" }
    },
    "recommended_next_commands": {
      "type": "array",
      "items": { "type": "string", "minLength": 1 }
    }
  }
}`

const gateResultSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.gate-result.v1",
  "title": "DevOS Gate Result",
  "type": "object",
  "required": ["status", "severity", "detector", "evidence"],
  "additionalProperties": false,
  "properties": {
    "status": {
      "type": "string",
      "enum": ["PASS", "AUTO_REPAIR", "AUTO_REPLAN", "REPORT_ONLY", "HUMAN_INPUT", "HUMAN_DECISION", "HARD_BLOCK"]
    },
    "severity": { "type": "string", "enum": ["low", "medium", "high", "critical"] },
    "detector": { "type": "string", "minLength": 1 },
    "human_action_type": { "type": "string" },
    "evidence": {}
  }
}`

const toolchainReportSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.toolchain-report.v1",
  "title": "DevOS Toolchain Report",
  "type": "object",
  "required": ["environment_id", "requirements"],
  "additionalProperties": false,
  "properties": {
    "environment_id": { "type": "string", "minLength": 1 },
    "requirements": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "required": ["toolchain_key", "required_for", "required_for_merge", "status", "message"],
        "additionalProperties": false,
        "properties": {
          "toolchain_key": { "type": "string", "minLength": 1 },
          "required_for": {
            "type": "string",
            "enum": ["implementation", "verification", "runtime", "runtime_smoke", "deployment"]
          },
          "required_for_merge": { "type": "boolean" },
          "status": {
            "type": "string",
            "enum": ["detected", "missing", "invalid", "setup_required", "waived", "unsupported", "revoked"]
          },
          "executable": { "type": "string" },
          "detected_path": { "type": "string" },
          "message": { "type": "string", "minLength": 1 }
        }
      }
    }
  }
}`

const codexRuntimeReadinessSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "devos.codex-runtime-readiness.v1",
  "title": "DevOS Codex Runtime Readiness",
  "type": "object",
  "required": ["host_goos", "items"],
  "additionalProperties": false,
  "properties": {
    "host_goos": { "type": "string", "minLength": 1 },
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": [
          "environment_id",
          "os_family",
          "project_root",
          "codex_adapter",
          "sandbox_profile",
          "expected_host_runtime",
          "current_runtime_usable",
          "classification"
        ],
        "additionalProperties": false,
        "properties": {
          "environment_id": { "type": "string", "minLength": 1 },
          "os_family": { "type": "string", "minLength": 1 },
          "project_root": { "type": "string", "minLength": 1 },
          "codex_adapter": { "type": "string", "minLength": 1 },
          "sandbox_profile": { "type": "string", "minLength": 1 },
          "expected_host_runtime": { "type": "string", "minLength": 1 },
          "current_runtime_usable": { "type": "boolean" },
          "classification": { "type": "string", "minLength": 1 },
          "blockers": {
            "type": "array",
            "items": { "type": "string", "minLength": 1 }
          },
          "argv": {
            "type": "array",
            "items": { "type": "string", "minLength": 1 }
          }
        }
      }
    }
  }
}`
