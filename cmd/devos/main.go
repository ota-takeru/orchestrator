package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/api"
	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/preflight"
	"github.com/ota-takeru/orchestrator/internal/projecthub"
	"github.com/ota-takeru/orchestrator/internal/registry"
	"github.com/ota-takeru/orchestrator/internal/schemas"
	"github.com/ota-takeru/orchestrator/internal/storage"
	"github.com/ota-takeru/orchestrator/internal/toolchains"
)

const (
	exitValidation = 1
	exitStorage    = 2
	exitPolicy     = 4
	exitInternal   = 9
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitValidation
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch args[0] {
	case "init":
		return runInit(ctx, args[1:], stdout)
	case "preflight":
		return runPreflight(ctx, args[1:], stdout)
	case "spec":
		return runSpec(ctx, args[1:], stdout)
	case "plan":
		return runPlan(ctx, args[1:], stdout)
	case "artifacts":
		return runArtifacts(ctx, args[1:], stdout, stderr)
	case "tasks":
		return runTasks(ctx, args[1:], stdout, stderr)
	case "task":
		return runTask(ctx, args[1:], stdout, stderr)
	case "status":
		return runStatusCommand(ctx, args[1:], stdout)
	case "doctor":
		return runDoctorCommand(ctx, args[1:], stdout)
	case "request":
		return runRequest(ctx, args[1:], stdout)
	case "requests":
		return runRequests(ctx, args[1:], stdout)
	case "project":
		return runProject(ctx, args[1:], stdout)
	case "queue":
		return runQueue(ctx, args[1:], stdout)
	case "work":
		return runWork(ctx, args[1:], stdout)
	case "change":
		return runChange(ctx, args[1:], stdout)
	case "run":
		return runTaskCommand(ctx, args[1:], stdout)
	case "verify":
		return runVerify(ctx, args[1:], stdout)
	case "bootstrap":
		return runBootstrap(ctx, args[1:], stdout)
	case "platform":
		return runPlatform(ctx, args[1:], stdout, stderr)
	case "inbox":
		return runInbox(ctx, args[1:], stdout)
	case "decisions":
		return runDecisions(ctx, args[1:], stdout)
	case "approve":
		return runApproveDecision(ctx, args[1:], stdout)
	case "memory":
		return runMemory(ctx, args[1:], stdout)
	case "dependency":
		return runDependency(ctx, args[1:], stdout, stderr)
	case "ui":
		return runUI(ctx, args[1:], stdout, stderr)
	case "serve":
		return runServe(ctx, args[1:], stdout)
	case "start":
		return runServe(ctx, append([]string{"--ui", "--open"}, args[1:]...), stdout)
	case "env":
		return runEnv(ctx, args[1:], stdout, stderr)
	case "review":
		return runReview(ctx, args[1:], stdout, stderr)
	case "merge":
		return runMerge(ctx, args[1:], stdout, stderr)
	case "patch":
		return runPatch(ctx, args[1:], stdout, stderr)
	case "cleanup":
		return runCleanup(ctx, args[1:], stdout)
	case "publish":
		return runPublish(ctx, args[1:], stdout)
	case "check":
		return runCheck(ctx, args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return exitValidation
	}
}

func runBootstrap(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	adapter := fs.String("adapter", "fake", "fake or codex")
	profile := fs.String("profile", "", "single-environment, windows-primary, wsl-primary, or hybrid")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if *adapter != "fake" {
		return writeError(stdout, *jsonOut, exitValidation, "unsupported_adapter", errors.New("only --adapter fake is implemented"))
	}
	concept := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if concept == "" {
		concept = "Bootstrap fake project"
	}
	initResult, err := preflight.InitProject(ctx, *projectRoot, concept)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	toolchainReport := toolchains.RunDoctor(ctx, initResult.PreflightReport.Environment, toolchains.Options{IncludeCodex: false})
	db, _, err := openProjectDB(ctx, initResult.ProjectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	defer db.Close()
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	project, err := db.SaveProjectInit(ctx, storage.ProjectInitInput{
		RootPath:        initResult.ProjectRoot,
		Environment:     initResult.PreflightReport.Environment,
		PreflightReport: initResult.PreflightReport,
		ToolchainReport: &toolchainReport,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	if err := db.SaveToolchainReport(ctx, project.ID, toolchainReport); err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	var runProfile any
	if strings.TrimSpace(*profile) != "" {
		mode, err := parsePlatformMode(*profile)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
		}
		runProfile, err = db.ConfigureFakeRunProfile(ctx, project.ID, mode, initResult.ProjectRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
		}
	}

	records, err := generateBootstrapArtifacts(ctx, db, project.ID, initResult.ProjectRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "bootstrap_failed", err)
	}
	for _, record := range records {
		if _, err := db.ApproveArtifactVersion(ctx, project.ID, record.ArtifactID, record.Version, "approved", ""); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
		}
	}
	tasks, err := db.MaterializeApprovedTasks(ctx, project.ID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	if len(tasks) == 0 {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", errors.New("no tasks materialized"))
	}
	runResult, err := db.RunFakeTask(ctx, project.ID, tasks[0].ID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, storage.ApprovalInput{ProjectID: project.ID, TaskID: tasks[0].ID, ApprovalType: storage.ApprovalFinalReview}); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	if _, err := db.ApproveTaskEvidence(ctx, storage.ApprovalInput{ProjectID: project.ID, TaskID: tasks[0].ID, ApprovalType: storage.ApprovalMerge}); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	queueEntry, err := db.QueueTaskForMerge(ctx, project.ID, tasks[0].ID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	mergeResult, err := db.ProcessNextFakeMerge(ctx, project.ID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "bootstrap_failed", err)
	}
	result := map[string]any{
		"project":     project,
		"profile":     runProfile,
		"artifacts":   records,
		"tasks":       tasks,
		"run":         runResult,
		"merge_queue": queueEntry,
		"merge":       mergeResult,
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Bootstrap fake workflow complete: %s %s\n", tasks[0].ID, mergeResult.TaskStatus)
	return 0
}

func generateBootstrapArtifacts(ctx context.Context, db *storage.DB, projectID string, root string) ([]storage.ArtifactVersionRecord, error) {
	concept, err := os.ReadFile(filepath.Join(root, ".devagent", "concept.md"))
	if err != nil {
		return nil, err
	}
	artifacts := buildInitialArtifacts(root, string(concept), true)
	records := make([]storage.ArtifactVersionRecord, 0, len(artifacts))
	for _, artifact := range artifacts {
		record, err := writeArtifactAndSave(ctx, db, projectID, root, artifact.path, artifact.typ, artifact.content)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

type generatedArtifact struct {
	path    string
	typ     storage.ArtifactType
	content []byte
}

type generatedVerificationCommand struct {
	ID               string
	WorkingDir       string
	Argv             []string
	RequiredForMerge bool
}

func buildInitialArtifacts(root string, concept string, includePRD bool) []generatedArtifact {
	commands := detectVerificationCommands(root)
	artifacts := make([]generatedArtifact, 0, 4)
	if includePRD {
		artifacts = append(artifacts, generatedArtifact{
			path:    ".devagent/prd.md",
			typ:     storage.ArtifactPRD,
			content: buildPRDArtifact(concept, commands),
		})
	}
	artifacts = append(artifacts, buildPlanArtifactsWithCommands(concept, commands)...)
	return artifacts
}

func buildPlanArtifacts(root string, concept string) []generatedArtifact {
	return buildPlanArtifactsWithCommands(concept, detectVerificationCommands(root))
}

func buildPlanArtifactsWithCommands(concept string, commands []generatedVerificationCommand) []generatedArtifact {
	if len(commands) == 0 {
		commands = defaultSmokeVerificationCommands()
	}
	return []generatedArtifact{
		{path: ".devagent/architecture.md", typ: storage.ArtifactArchitecture, content: buildArchitectureArtifact(concept, commands)},
		{path: ".devagent/roadmap.yaml", typ: storage.ArtifactRoadmap, content: buildRoadmapArtifact(concept, commands)},
		{path: ".devagent/tasks/TASK-001.yaml", typ: storage.ArtifactTaskYAML, content: buildTaskYAMLArtifact(concept, commands)},
	}
}

func buildPRDArtifact(concept string, commands []generatedVerificationCommand) []byte {
	title := conceptTitle(concept)
	var b strings.Builder
	fmt.Fprintf(&b, "# PRD\n\n## Product Concept\n\n%s\n\n", markdownParagraph(concept))
	fmt.Fprintf(&b, "## Goal\n\nDeliver a functional local-first application increment for: %s.\n\n", title)
	b.WriteString("## Users\n\n- Primary user: local developer operating DevOS from CLI or the local web UI.\n")
	b.WriteString("- Reviewer: human approver who needs concise evidence, diffs, and merge readiness.\n\n")
	b.WriteString("## Core Workflow\n\n")
	b.WriteString("1. Capture the concept as canonical project context.\n")
	b.WriteString("2. Generate approved PRD, architecture, roadmap, and task artifacts.\n")
	b.WriteString("3. Materialize implementation tasks only from approved artifacts.\n")
	b.WriteString("4. Run implementation in an isolated task worktree.\n")
	b.WriteString("5. Verify with Orchestrator-owned commands and evidence.\n")
	b.WriteString("6. Require Human Inbox approval before merge or manual application.\n\n")
	b.WriteString("## Acceptance Criteria\n\n")
	b.WriteString("- The generated task can be materialized only after PRD, architecture, roadmap, and task YAML approval.\n")
	b.WriteString("- Implementation evidence includes run logs, diff, summary, verification results, and gate results.\n")
	b.WriteString("- Required verification commands are recorded in Task YAML and reused for merge reverify.\n")
	b.WriteString("- Human approval is blocked when required verification or gate evidence is missing.\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "- Verification `%s` passes: `%s`.\n", command.ID, strings.Join(command.Argv, " "))
	}
	return []byte(b.String())
}

func buildArchitectureArtifact(concept string, commands []generatedVerificationCommand) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Architecture\n\n## Scope\n\n%s\n\n", markdownParagraph(concept))
	b.WriteString("## Runtime Shape\n\n")
	b.WriteString("- Backend, CLI, worker, state machine, and evidence storage are implemented in Go.\n")
	b.WriteString("- UI is implemented in React and TypeScript, served by the local DevOS API when requested.\n")
	b.WriteString("- SQLite stores canonical state, approvals, runs, command events, verification results, and merge queue records.\n")
	b.WriteString("- Markdown/YAML artifacts remain human-readable project context and are versioned through the artifact repository.\n\n")
	b.WriteString("## Execution Boundaries\n\n")
	b.WriteString("- Coding runs execute in task worktrees, not directly in the canonical worktree.\n")
	b.WriteString("- Verification commands are executed by Orchestrator runners and are the source of truth for test results.\n")
	b.WriteString("- Human Inbox items are projections; decisions, approvals, and patch applications remain the source of truth.\n\n")
	b.WriteString("## Verification Plan\n\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "- `%s`: `%s` from `%s`, required_for_merge=%t.\n", command.ID, strings.Join(command.Argv, " "), command.WorkingDir, command.RequiredForMerge)
	}
	return []byte(b.String())
}

func buildRoadmapArtifact(concept string, commands []generatedVerificationCommand) []byte {
	title := yamlQuote(conceptTitle(concept))
	var b strings.Builder
	b.WriteString("roadmap:\n")
	fmt.Fprintf(&b, "  title: %s\n", title)
	b.WriteString("  planning_unit: feature_chunk\n")
	b.WriteString("  slices:\n")
	b.WriteString("    - id: TASK-001\n")
	fmt.Fprintf(&b, "      title: %s\n", yamlQuote("Implement "+conceptTitle(concept)))
	b.WriteString("      status: proposed\n")
	b.WriteString("      depends_on: []\n")
	b.WriteString("      verification:\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "        - %s\n", yamlQuote(command.ID))
	}
	return []byte(b.String())
}

func buildTaskYAMLArtifact(concept string, commands []generatedVerificationCommand) []byte {
	var b strings.Builder
	b.WriteString("id: TASK-001\n")
	fmt.Fprintf(&b, "title: %s\n", yamlQuote("Implement "+conceptTitle(concept)))
	b.WriteString("status: proposed\n")
	b.WriteString("base_branch: main\n")
	b.WriteString("planning_unit: feature_chunk\n")
	b.WriteString("description: ")
	b.WriteString(yamlQuote(conceptSummary(concept)))
	b.WriteString("\n")
	b.WriteString("acceptance_criteria:\n")
	b.WriteString("  - Implementation diff is captured as an Orchestrator-owned artifact.\n")
	b.WriteString("  - Required verification commands pass before human review or merge.\n")
	b.WriteString("  - Decision Gate results are stored before approval.\n")
	b.WriteString("verification_commands:\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "  - id: %s\n", yamlQuote(command.ID))
		b.WriteString("    environment: primary\n")
		b.WriteString("    runner: auto\n")
		fmt.Fprintf(&b, "    required_for_merge: %t\n", command.RequiredForMerge)
		fmt.Fprintf(&b, "    working_dir: %s\n", yamlQuote(command.WorkingDir))
		b.WriteString("    command:\n")
		fmt.Fprintf(&b, "      argv: %s\n", jsonArgv(command.Argv))
		b.WriteString("    network: false\n")
	}
	return []byte(b.String())
}

func detectVerificationCommands(root string) []generatedVerificationCommand {
	commands := []generatedVerificationCommand{}
	if fileExists(filepath.Join(root, "go.mod")) {
		commands = append(commands, generatedVerificationCommand{ID: "go-test", WorkingDir: "project_root", Argv: []string{"go", "test", "./..."}, RequiredForMerge: true})
	}
	if fileExists(filepath.Join(root, "ui", "package.json")) {
		commands = append(commands,
			generatedVerificationCommand{ID: "ui-test", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "test"}, RequiredForMerge: true},
			generatedVerificationCommand{ID: "ui-lint", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "lint"}, RequiredForMerge: true},
			generatedVerificationCommand{ID: "ui-build", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "build"}, RequiredForMerge: true},
		)
	}
	return commands
}

func defaultSmokeVerificationCommands() []generatedVerificationCommand {
	return []generatedVerificationCommand{
		{
			ID:               "implementation-files-present",
			WorkingDir:       "project_root",
			Argv:             []string{"node", "-e", "const fs=require('fs'); const dataDir=['orches','trator-data'].join(''); const ignored=new Set(['.git','.devagent','.devagent-worktrees',dataDir]); const entries=fs.readdirSync('.').filter((name)=>!ignored.has(name)); if(entries.length===0){console.error('no implementation files generated'); process.exit(1)} console.log(entries.join('\\n'));"},
			RequiredForMerge: true,
		},
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func conceptTitle(concept string) string {
	summary := conceptSummary(concept)
	if summary == "" {
		return "Initial Application Workflow"
	}
	words := strings.Fields(summary)
	if len(words) > 12 {
		words = words[:12]
	}
	return strings.Join(words, " ")
}

func conceptSummary(concept string) string {
	trimmed := strings.TrimSpace(concept)
	trimmed = strings.TrimPrefix(trimmed, "# Concept")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "Build the initial local-first application workflow."
	}
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return "Build the initial local-first application workflow."
}

func markdownParagraph(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Build the initial local-first application workflow."
	}
	return value
}

func yamlQuote(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(raw)
}

func jsonArgv(argv []string) string {
	raw, err := json.Marshal(argv)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func runTaskCommand(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	adapter := fs.String("adapter", "local", "local or fake")
	realCodex := fs.Bool("real-codex", false, "run Linux/current-environment real Codex adapter")
	dryRun := fs.Bool("dry-run", false, "preview real Codex execution without starting Codex")
	verifyAfter := fs.Bool("verify", false, "run orchestrator verification after implementation succeeds")
	verifyAdapter := fs.String("verify-adapter", "local", "verification adapter when --verify is set")
	verifyEnvironmentID := fs.String("verify-env", "", "verification environment id when --verify is set")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	if *realCodex {
		*adapter = "real-codex"
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "run_failed", err)
	}
	defer db.Close()
	switch *adapter {
	case "fake":
		result, err := db.RunFakeTask(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "run_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Run complete: %s -> %s\n", result.TaskID, result.TaskStatus)
		return 0
	case "real-codex", "codex":
		if *dryRun {
			result, err := db.PreviewRealCodexTask(ctx, projectID, fs.Arg(0))
			if err != nil {
				return writeError(stdout, *jsonOut, exitValidation, "run_dry_run_failed", err)
			}
			if *jsonOut {
				return writeJSON(stdout, result, 0)
			}
			fmt.Fprintf(stdout, "Real Codex dry-run: %s %s\n", result.TaskID, result.Classification)
			if len(result.Blockers) > 0 {
				fmt.Fprintf(stdout, "Blockers: %s\n", strings.Join(result.Blockers, ", "))
			}
			return 0
		}
		result, err := db.RunRealCodexTask(ctx, projectID, fs.Arg(0), nil)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "run_failed", err)
		}
		var verification *storage.VerifyTaskResult
		if *verifyAfter && result.TaskStatus == "verifying" {
			verifyResult, err := db.VerifyTask(ctx, projectID, fs.Arg(0), storage.VerifyTaskInput{
				Adapter:       *verifyAdapter,
				EnvironmentID: *verifyEnvironmentID,
			})
			if err != nil {
				return writeError(stdout, *jsonOut, exitValidation, "verify_failed", err)
			}
			verification = &verifyResult
		}
		if *jsonOut {
			if verification != nil {
				return writeJSON(stdout, map[string]any{"run": result, "verification": verification}, 0)
			}
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Real Codex run complete: %s -> %s\n", result.TaskID, result.TaskStatus)
		if verification != nil {
			fmt.Fprintf(stdout, "Verification complete: %s -> %s\n", verification.TaskID, verification.TaskStatus)
			fmt.Fprintf(stdout, "Verification run: %s\n", verification.VerificationRun)
		}
		return 0
	default:
		return writeError(stdout, *jsonOut, exitValidation, "unsupported_adapter", fmt.Errorf("unsupported adapter: %s", *adapter))
	}
}

func runVerify(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	adapter := fs.String("adapter", "local", "local or fake")
	environmentID := fs.String("env", "", "execution environment id")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "verify_failed", err)
	}
	defer db.Close()
	result, err := db.VerifyTask(ctx, projectID, fs.Arg(0), storage.VerifyTaskInput{
		Adapter:       *adapter,
		EnvironmentID: *environmentID,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "verify_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Verification complete: %s -> %s\n", result.TaskID, result.TaskStatus)
	fmt.Fprintf(stdout, "Run: %s\n", result.VerificationRun)
	return 0
}

func runTasks(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "materialize" {
		fs := flag.NewFlagSet("tasks materialize", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "tasks_materialize_failed", err)
		}
		defer db.Close()
		tasks, err := db.MaterializeApprovedTasks(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "tasks_materialize_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"tasks": tasks}, 0)
		}
		for _, task := range tasks {
			fmt.Fprintf(stdout, "Task ready: %s %s\n", task.ID, task.Title)
		}
		return 0
	}

	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	status := fs.String("status", "", "task status")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "tasks_list_failed", err)
	}
	defer db.Close()
	tasks, err := db.ListTasks(ctx, projectID, *status)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "tasks_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"tasks": tasks}, 0)
	}
	if len(tasks) == 0 {
		fmt.Fprintln(stdout, "No tasks.")
		return 0
	}
	for _, task := range tasks {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", task.ID, task.Status, task.Title)
	}
	return 0
}

func runTask(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing task subcommand")
		return exitValidation
	}
	switch args[0] {
	case "show":
		return runTaskShow(ctx, args[1:], stdout)
	case "artifacts":
		return runTaskArtifacts(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown task subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runTaskShow(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("task show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("TASK_ID is required"))
	}
	taskID := fs.Arg(0)
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "task_show_failed", err)
	}
	defer db.Close()
	detail, err := taskDetail(ctx, db, projectID, taskID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "task_show_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, detail, 0)
	}
	fmt.Fprintf(stdout, "Task: %s\n", detail["id"])
	fmt.Fprintf(stdout, "Title: %s\n", detail["title"])
	fmt.Fprintf(stdout, "Status: %s\n", detail["status"])
	fmt.Fprintf(stdout, "Latest run: %s\n", detail["latest_run_id"])
	fmt.Fprintf(stdout, "Worktree: %s\n", detail["worktree_path"])
	fmt.Fprintf(stdout, "Candidate commit: %s\n", detail["candidate_commit"])
	fmt.Fprintf(stdout, "Diff hash: %s\n", detail["diff_hash"])
	fmt.Fprintf(stdout, "Verification: %s\n", detail["verification_status"])
	fmt.Fprintf(stdout, "Merge queue: %s\n", detail["merge_queue_status"])
	return 0
}

func runTaskArtifacts(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("task artifacts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	includeContent := fs.Bool("include-content", false, "include safe text artifact content")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("TASK_ID is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "task_artifacts_failed", err)
	}
	defer db.Close()
	artifacts, err := db.ListTaskRunArtifacts(ctx, projectID, fs.Arg(0), *includeContent)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "task_artifacts_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"artifacts": artifacts}, 0)
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", artifact.RunID, artifact.ArtifactType, artifact.ArtifactKey, artifact.Path)
	}
	return 0
}

func runStatusCommand(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "status_failed", err)
	}
	defer db.Close()
	status, err := projectStatusSummary(ctx, db, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "status_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, status, 0)
	}
	fmt.Fprintf(stdout, "Project: %s\n", status["project"])
	fmt.Fprintf(stdout, "Open tasks: %v\n", status["open_tasks"])
	fmt.Fprintf(stdout, "Blocked tasks: %v\n", status["blocked_tasks"])
	fmt.Fprintf(stdout, "Inbox: %v\n", status["open_inbox"])
	fmt.Fprintf(stdout, "Merge queue: %v\n", status["merge_queue"])
	fmt.Fprintf(stdout, "Last run: %s\n", status["last_run"])
	fmt.Fprintf(stdout, "Next action: %s\n", status["next_action"])
	return 0
}

func runDoctorCommand(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	envID := fs.String("env", "", "execution environment id")
	save := fs.Bool("save", false, "save toolchain report")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "doctor_failed", err)
	}
	defer db.Close()
	env := platform.DetectHostEnvironment(root)
	if strings.TrimSpace(*envID) != "" {
		envs, err := db.ListExecutionEnvironments(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
		}
		found := false
		for _, candidate := range envs {
			if candidate.ID == *envID {
				env = candidate
				found = true
				break
			}
		}
		if !found {
			return writeError(stdout, *jsonOut, exitValidation, "doctor_failed", fmt.Errorf("execution environment not found: %s", *envID))
		}
	}
	toolchainReport := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: true, IncludeUI: true})
	if *save {
		if err := db.SaveToolchainReport(ctx, projectID, toolchainReport); err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
		}
	}
	setup, err := db.LoadSetupStatus(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
	}
	status, err := projectStatusSummary(ctx, db, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
	}
	mergeStatus, err := db.MergeGateStatus(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
	}
	profiles, err := db.ListRunProfiles(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "doctor_failed", err)
	}
	activeProfile := ""
	for _, profile := range profiles {
		if profile.Status == "active" {
			activeProfile = profile.ID
			break
		}
	}
	schemaValidation := schemas.ValidateInstalled(root)
	result := map[string]any{
		"project_root":      root,
		"project_id":        projectID,
		"active_profile":    activeProfile,
		"environment":       env,
		"toolchain_report":  toolchainReport,
		"setup_status":      setup,
		"status":            status,
		"schema_registry":   schemaValidation,
		"merge_status":      mergeStatus,
		"last_real_run":     lastRunSummary(ctx, db, projectID, "implementation"),
		"last_verification": lastRunSummary(ctx, db, projectID, "verification"),
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Project: %s\n", status["project"])
	fmt.Fprintf(stdout, "Project ID: %s\n", projectID)
	fmt.Fprintf(stdout, "Root: %s\n", root)
	fmt.Fprintf(stdout, "Environment: %s %s\n", env.ID, env.OSFamily)
	fmt.Fprintf(stdout, "Git clean: %t\n", setup.GitClean)
	fmt.Fprintf(stdout, "Active profile: %s\n", activeProfile)
	fmt.Fprintf(stdout, "Schema registry valid: %t\n", schemaValidation.Valid)
	fmt.Fprintf(stdout, "Required verification configured: %t\n", setup.RequiredVerificationConfigured)
	fmt.Fprintf(stdout, "Merge ready: %t\n", mergeStatus.Ready)
	fmt.Fprintf(stdout, "Open inbox: %v\n", status["open_inbox"])
	return 0
}

func runRequest(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if body == "" {
		return writeError(stdout, *jsonOut, exitValidation, "request_failed", errors.New("request text is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "request_failed", err)
	}
	defer db.Close()
	result, err := db.CreateFeatureRequest(ctx, projectID, body)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "request_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Feature request queued: %s\n", result.FeatureRequest.ID)
	fmt.Fprintf(stdout, "Work queue item: %s %s\n", result.QueueItem.ID, result.QueueItem.Lane)
	return 0
}

func runRequests(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("requests", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	status := fs.String("status", "", "feature request status")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "requests_failed", err)
	}
	defer db.Close()
	requests, err := db.ListFeatureRequests(ctx, projectID, *status)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "requests_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"feature_requests": requests}, 0)
	}
	if len(requests) == 0 {
		fmt.Fprintln(stdout, "No feature requests.")
		return 0
	}
	for _, request := range requests {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", request.ID, request.Status, request.Title)
	}
	return 0
}

func runProject(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) == 0 {
		return writeError(stdout, false, exitValidation, "invalid_arguments", errors.New("project subcommand is required"))
	}
	switch args[0] {
	case "add":
		return runProjectAdd(ctx, args[1:], stdout)
	case "list":
		return runProjectList(ctx, args[1:], stdout)
	case "remove":
		return runProjectRemove(ctx, args[1:], stdout)
	case "refresh":
		return runProjectRefresh(ctx, args[1:], stdout)
	default:
		return writeError(stdout, false, exitValidation, "invalid_arguments", fmt.Errorf("unknown project subcommand: %s", args[0]))
	}
}

func runProjectAdd(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registryPath := fs.String("registry", "", "global registry DB path")
	name := fs.String("name", "", "project display name")
	authority := fs.String("authority", "", "windows or wsl")
	projectRoot := fs.String("project-root", "", "Windows project root")
	dataRoot := fs.String("data-root", "", "project-local data root")
	windowsDisplayRoot := fs.String("windows-display-root", "", "Windows display root")
	wslDistro := fs.String("wsl-distro", "", "WSL distro")
	wslRoot := fs.String("wsl-root", "", "WSL project root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	input := registry.AddProjectInput{
		DisplayName:        *name,
		AuthorityRuntime:   registry.AuthorityRuntime(strings.TrimSpace(*authority)),
		ProjectRoot:        *projectRoot,
		DataRoot:           *dataRoot,
		WindowsDisplayRoot: *windowsDisplayRoot,
		WSLDistro:          *wslDistro,
		WSLProjectRoot:     *wslRoot,
	}
	if input.AuthorityRuntime == registry.AuthorityWindows {
		root, err := filepath.Abs(strings.TrimSpace(input.ProjectRoot))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "project_add_failed", err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "project_add_failed", err)
		}
		if !info.IsDir() {
			return writeError(stdout, *jsonOut, exitValidation, "project_add_failed", fmt.Errorf("project root is not a directory: %s", root))
		}
		input.ProjectRoot = root
	}
	regDB, errCode, err := openRegistryDB(ctx, *registryPath)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "project_add_failed", err)
	}
	defer regDB.Close()
	project, err := regDB.AddProject(ctx, input)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "project_add_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"project": project}, 0)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", project.ID, project.AuthorityRuntime, project.Status, project.DisplayName)
	return 0
}

func runProjectList(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("project list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registryPath := fs.String("registry", "", "global registry DB path")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	regDB, errCode, err := openRegistryDB(ctx, *registryPath)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "project_list_failed", err)
	}
	defer regDB.Close()
	projects, err := regDB.ListProjects(ctx)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "project_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"projects": projects}, 0)
	}
	for _, project := range projects {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", project.ID, project.AuthorityRuntime, project.Status, project.DisplayName)
	}
	return 0
}

func runProjectRemove(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("project remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registryPath := fs.String("registry", "", "global registry DB path")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("project remove requires PROJECT_ID"))
	}
	regDB, errCode, err := openRegistryDB(ctx, *registryPath)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "project_remove_failed", err)
	}
	defer regDB.Close()
	if err := regDB.RemoveProject(ctx, fs.Arg(0)); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "project_remove_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"removed": fs.Arg(0)}, 0)
	}
	fmt.Fprintf(stdout, "Project removed: %s\n", fs.Arg(0))
	return 0
}

func runProjectRefresh(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("project refresh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registryPath := fs.String("registry", "", "global registry DB path")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("project refresh requires PROJECT_ID"))
	}
	regDB, errCode, err := openRegistryDB(ctx, *registryPath)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "project_refresh_failed", err)
	}
	defer regDB.Close()
	updated, snapshot, err := projecthub.NewDefaultHub(regDB).Refresh(ctx, fs.Arg(0))
	if err != nil {
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"project": updated, "error": err.Error()}, exitValidation)
		}
		return writeError(stdout, *jsonOut, exitValidation, "project_refresh_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"project": updated, "snapshot": snapshot}, 0)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", updated.ID, updated.AuthorityRuntime, updated.Status, updated.DisplayName)
	return 0
}

func runQueue(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	status := fs.String("status", "", "queue item status")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "queue_failed", err)
	}
	defer db.Close()
	items, err := db.ListWorkQueueItems(ctx, projectID, *status)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "queue_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"items": items}, 0)
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No queue items.")
		return 0
	}
	for _, item := range items {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", item.ID, item.Status, item.Lane, item.ItemType, item.ItemID)
	}
	return 0
}

func runWork(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) == 0 {
		return writeError(stdout, false, exitValidation, "invalid_arguments", errors.New("work subcommand is required"))
	}
	switch args[0] {
	case "start":
		return runWorkStart(ctx, args[1:], stdout)
	case "status":
		return runWorkStatus(ctx, args[1:], stdout)
	case "pause":
		return runWorkPause(ctx, args[1:], stdout)
	case "resume":
		return runWorkResume(ctx, args[1:], stdout)
	default:
		return writeError(stdout, false, exitValidation, "invalid_arguments", fmt.Errorf("unknown work subcommand: %s", args[0]))
	}
}

func runWorkStart(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("work start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	mode := fs.String("mode", "sequential", "worker mode")
	adapter := fs.String("adapter", "fake", "implementation adapter: fake or real-codex")
	planningConcurrency := fs.Int("planning-concurrency", 3, "planning concurrency")
	implementationConcurrency := fs.Int("implementation-concurrency", 1, "implementation concurrency")
	until := fs.String("until", "", "stop condition")
	budget := fs.String("budget", "", "budget duration")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if strings.TrimSpace(*budget) != "" {
		if _, err := time.ParseDuration(*budget); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_budget", err)
		}
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "work_start_failed", err)
	}
	defer db.Close()
	result, err := db.StartWork(ctx, storage.WorkStartInput{
		ProjectID:                 projectID,
		Mode:                      *mode,
		ImplementationAdapter:     *adapter,
		PlanningConcurrency:       *planningConcurrency,
		ImplementationConcurrency: *implementationConcurrency,
		Until:                     *until,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "work_start_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Worker run: %s %s\n", result.WorkerRun.ID, result.WorkerRun.Status)
	fmt.Fprintf(stdout, "Planning runs: %d\n", len(result.Planning.StartedRuns))
	fmt.Fprintf(stdout, "Task groups: %d\n", len(result.Consolidation.TaskGroups))
	return 0
}

func runWorkStatus(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("work status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "work_status_failed", err)
	}
	defer db.Close()
	status, err := db.GetWorkStatus(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "work_status_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, status, 0)
	}
	fmt.Fprintf(stdout, "Worker runs: %d\n", len(status.WorkerRuns))
	return 0
}

func runWorkPause(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("work pause", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("worker run id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "work_pause_failed", err)
	}
	defer db.Close()
	record, err := db.PauseWorkerRun(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "work_pause_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "Worker paused: %s %s\n", record.ID, record.Status)
	return 0
}

func runWorkResume(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("work resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("worker run id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "work_resume_failed", err)
	}
	defer db.Close()
	record, err := db.ResumeWorkerRun(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "work_resume_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "Worker resumed: %s %s\n", record.ID, record.Status)
	return 0
}

func runChange(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) == 0 {
		return writeError(stdout, false, exitValidation, "invalid_arguments", errors.New("change subcommand is required"))
	}
	switch args[0] {
	case "request":
		return runChangeRequest(ctx, args[1:], stdout)
	case "analyze":
		return runChangeAnalyze(ctx, args[1:], stdout)
	case "approve":
		return runChangeApprove(ctx, args[1:], stdout)
	default:
		return writeError(stdout, false, exitValidation, "invalid_arguments", fmt.Errorf("unknown change subcommand: %s", args[0]))
	}
}

func runChangeRequest(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("change request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if body == "" {
		return writeError(stdout, *jsonOut, exitValidation, "change_request_failed", errors.New("change request text is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "change_request_failed", err)
	}
	defer db.Close()
	result, err := db.CreateChangeRequest(ctx, projectID, body)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "change_request_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Change request proposed: %s\n", result.ChangeRequest.ID)
	fmt.Fprintf(stdout, "Work queue item: %s %s\n", result.QueueItem.ID, result.QueueItem.Lane)
	return 0
}

func runChangeAnalyze(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("change analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("change request id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "change_analyze_failed", err)
	}
	defer db.Close()
	result, err := db.AnalyzeChangeRequest(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "change_analyze_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Change request analyzed: %s\n", result.ChangeRequest.ID)
	fmt.Fprintf(stdout, "Planning run: %s %s\n", result.Run.ID, result.Run.Status)
	return 0
}

func runChangeApprove(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("change approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	option := fs.String("option", "", "selected change option")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("change request id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "change_approve_failed", err)
	}
	defer db.Close()
	record, err := db.ApproveChangeRequest(ctx, storage.ChangeApproveInput{
		ProjectID:       projectID,
		ChangeRequestID: fs.Arg(0),
		Option:          *option,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "change_approve_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "Change request approved: %s\n", record.ID)
	return 0
}

func runSpec(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("spec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "spec_failed", err)
	}
	defer db.Close()
	concept, err := os.ReadFile(filepath.Join(root, ".devagent", "concept.md"))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "spec_failed", err)
	}
	content := buildPRDArtifact(string(concept), detectVerificationCommands(root))
	record, err := writeArtifactAndSave(ctx, db, projectID, root, ".devagent/prd.md", storage.ArtifactPRD, content)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "spec_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "PRD artifact proposed: %s v%d\n", record.ArtifactID, record.Version)
	return 0
}

func runPlan(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "start":
			return runPlanStart(ctx, args[1:], stdout)
		case "status":
			return runPlanStatus(ctx, args[1:], stdout)
		case "consolidate":
			return runPlanConsolidate(ctx, args[1:], stdout)
		case "checkpoint":
			return runPlanCheckpoint(ctx, args[1:], stdout)
		}
	}

	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "plan_failed", err)
	}
	defer db.Close()
	concept, err := os.ReadFile(filepath.Join(root, ".devagent", "concept.md"))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "plan_failed", err)
	}
	records := make([]storage.ArtifactVersionRecord, 0, 3)
	artifacts := buildPlanArtifacts(root, string(concept))
	for _, artifact := range artifacts {
		record, err := writeArtifactAndSave(ctx, db, projectID, root, artifact.path, artifact.typ, artifact.content)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "plan_failed", err)
		}
		records = append(records, record)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"artifacts": records}, 0)
	}
	for _, record := range records {
		fmt.Fprintf(stdout, "Artifact proposed: %s v%d %s\n", record.ArtifactID, record.Version, record.Path)
	}
	return 0
}

func runPlanStart(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("plan start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	concurrency := fs.Int("concurrency", 3, "maximum planning items to process")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "plan_start_failed", err)
	}
	defer db.Close()
	result, err := db.StartPlanning(ctx, storage.PlanStartInput{ProjectID: projectID, Concurrency: *concurrency})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "plan_start_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	if len(result.StartedRuns) == 0 {
		fmt.Fprintln(stdout, "No queued planning work.")
		return 0
	}
	for _, run := range result.StartedRuns {
		fmt.Fprintf(stdout, "Planning run complete: %s %s\n", run.ID, run.Status)
	}
	return 0
}

func runPlanStatus(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("plan status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "plan_status_failed", err)
	}
	defer db.Close()
	status, err := db.GetPlanningStatus(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "plan_status_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, status, 0)
	}
	fmt.Fprintf(stdout, "Planning runs: %d\n", len(status.Runs))
	fmt.Fprintf(stdout, "Planning artifacts: %d\n", len(status.Artifacts))
	return 0
}

func runPlanConsolidate(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("plan consolidate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "plan_consolidate_failed", err)
	}
	defer db.Close()
	result, err := db.ConsolidatePlanning(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "plan_consolidate_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	if len(result.TaskGroups) == 0 {
		fmt.Fprintln(stdout, "No planning artifacts to consolidate.")
		return 0
	}
	for _, group := range result.TaskGroups {
		fmt.Fprintf(stdout, "Task group proposed: %s %s\n", group.ID, group.Title)
	}
	return 0
}

func runPlanCheckpoint(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("plan checkpoint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	taskID := fs.String("task", "", "task id to checkpoint")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if strings.TrimSpace(*taskID) == "" {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("--task is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "plan_checkpoint_failed", err)
	}
	defer db.Close()
	result, err := db.CreateRollingCheckpoint(ctx, storage.RollingCheckpointInput{
		ProjectID: projectID,
		TaskID:    *taskID,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "plan_checkpoint_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Rolling checkpoint saved: %s %s\n", result.Run.ID, result.Artifact.Path)
	return 0
}

func runArtifacts(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runArtifactsList(ctx, args, stdout)
	}
	switch args[0] {
	case "approve":
		fs := flag.NewFlagSet("artifacts approve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		version := fs.Int("version", 1, "artifact version")
		status := fs.String("status", "approved", "approved, approved_with_notes, or rejected")
		notes := fs.String("notes", "", "approval notes or rejected reason")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("artifact id is required"))
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "artifact_approve_failed", err)
		}
		defer db.Close()
		record, err := db.ApproveArtifactVersion(ctx, projectID, fs.Arg(0), *version, *status, *notes)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "artifact_approve_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, record, 0)
		}
		fmt.Fprintf(stdout, "Artifact reviewed: %s v%d %s\n", record.ArtifactID, record.Version, record.Status)
		return 0
	case "revise":
		fs := flag.NewFlagSet("artifacts revise", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		contentStdin := fs.Bool("content-stdin", false, "read revised artifact content from stdin")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("artifact id is required"))
		}
		if !*contentStdin {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("--content-stdin is required"))
		}
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "artifact_revise_failed", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "artifact_revise_failed", err)
		}
		defer db.Close()
		record, err := db.SaveArtifactRevision(ctx, projectID, fs.Arg(0), content)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "artifact_revise_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, record, 0)
		}
		fmt.Fprintf(stdout, "Artifact revision saved: %s v%d %s\n", record.ArtifactID, record.Version, record.Status)
		return 0
	case "revise-with-codex":
		fs := flag.NewFlagSet("artifacts revise-with-codex", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		instructionStdin := fs.Bool("instruction-stdin", false, "read Codex revision instruction from stdin")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("artifact id is required"))
		}
		if !*instructionStdin {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("--instruction-stdin is required"))
		}
		instruction, err := io.ReadAll(os.Stdin)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "artifact_codex_revise_failed", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "artifact_codex_revise_failed", err)
		}
		defer db.Close()
		result, err := db.ReviseArtifactWithCodex(ctx, storage.ArtifactCodexRevisionInput{
			ProjectID:   projectID,
			ArtifactID:  fs.Arg(0),
			Instruction: string(instruction),
		})
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "artifact_codex_revise_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Codex artifact revision saved: %s v%d %s\n", result.Artifact.ArtifactID, result.Artifact.Version, result.Artifact.Status)
		return 0
	case "trusted":
		return runArtifactsTrusted(ctx, args[1:], stdout)
	case "check":
		return runArtifactsCheck(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown artifacts subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runArtifactsList(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("artifacts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	artifactType := fs.String("type", "", "artifact type")
	includeContent := fs.Bool("include-content", false, "include latest artifact content in JSON output")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "artifacts_list_failed", err)
	}
	defer db.Close()
	var artifacts []storage.ArtifactRecord
	if *includeContent {
		artifacts, err = db.ListArtifactsWithContent(ctx, projectID, *artifactType)
	} else {
		artifacts, err = db.ListArtifacts(ctx, projectID, *artifactType)
	}
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "artifacts_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"artifacts": artifacts}, 0)
	}
	if len(artifacts) == 0 {
		fmt.Fprintln(stdout, "No artifacts.")
		return 0
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(stdout, "%s\t%s\t%s\tlatest=%d\tapproved=%d\t%s\n", artifact.ArtifactID, artifact.ArtifactType, artifact.Status, artifact.LatestVersion, artifact.ApprovedVersion, artifact.Path)
	}
	return 0
}

func runArtifactsTrusted(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("artifacts trusted", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "artifacts_trusted_failed", err)
	}
	defer db.Close()
	artifacts, err := db.TrustedArtifactContentBundle(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "artifacts_trusted_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"artifacts": artifacts}, 0)
	}
	if len(artifacts) == 0 {
		fmt.Fprintln(stdout, "No trusted artifacts.")
		return 0
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(stdout, "%s\t%s\tv%d\t%s\t%s\n", artifact.ArtifactID, artifact.ArtifactType, artifact.Version, artifact.Status, artifact.Path)
	}
	return 0
}

func runArtifactsCheck(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("artifacts check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "artifacts_check_failed", err)
	}
	defer db.Close()
	violations, err := db.CheckArtifactInvariants(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "artifacts_check_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"violations": violations}, 0)
	}
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "Artifact invariants OK.")
		return 0
	}
	for _, violation := range violations {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", violation.Scope, violation.ID, violation.Code, violation.Message)
	}
	return exitValidation
}

func runCheck(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "check_failed", err)
	}
	defer db.Close()
	violations, err := db.CheckProjectInvariants(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "check_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"violations": violations}, validationExitForViolations(violations))
	}
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "Project invariants OK.")
		return 0
	}
	for _, violation := range violations {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", violation.Scope, violation.ID, violation.Code, violation.Message)
	}
	return exitValidation
}

func validationExitForViolations(violations []storage.InvariantViolation) int {
	if len(violations) > 0 {
		return exitValidation
	}
	return 0
}

func runReview(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing review subcommand")
		return exitValidation
	}
	switch args[0] {
	case "approve":
		return runApproveEvidence(ctx, args[1:], stdout, storage.ApprovalFinalReview)
	case "reject":
		return runRejectFinalReview(ctx, args[1:], stdout)
	default:
		return runGenerateReview(ctx, args, stdout)
	}
}

func runGenerateReview(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "review_failed", err)
	}
	defer db.Close()
	result, err := db.ReviewTask(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "review_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Review generated: %s\n", result.ReviewRunID)
	fmt.Fprintf(stdout, "Semantic diffs: %d\n", len(result.SemanticDiffs))
	return 0
}

func runRejectFinalReview(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("review reject", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	notes := fs.String("notes", "", "rejection notes")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "review_reject_failed", err)
	}
	defer db.Close()
	result, err := db.RejectTaskFinalReview(ctx, storage.ApprovalInput{
		ProjectID: projectID,
		TaskID:    fs.Arg(0),
		Notes:     *notes,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "review_reject_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Review rejected: %s\n", result.ID)
	fmt.Fprintf(stdout, "Task status: %s\n", result.TaskStatus)
	return 0
}

func runMerge(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing merge subcommand")
		return exitValidation
	}
	switch args[0] {
	case "approve":
		return runApproveEvidence(ctx, args[1:], stdout, storage.ApprovalMerge)
	case "queue":
		return runMergeQueue(ctx, args[1:], stdout)
	default:
		return runQueueTaskForMerge(ctx, args, stdout)
	}
}

func runQueueTaskForMerge(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	dryRun := fs.Bool("dry-run", false, "validate merge queue entry without writing")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "merge_queue_failed", err)
	}
	defer db.Close()
	if *dryRun {
		entry, err := db.PreviewTaskMerge(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "merge_dry_run_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, entry, 0)
		}
		fmt.Fprintf(stdout, "Merge dry-run: %s can be queued as %s\n", entry.TaskID, entry.ID)
		return 0
	}
	entry, err := db.QueueTaskForMerge(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "merge_queue_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, entry, 0)
	}
	fmt.Fprintf(stdout, "Queued for merge: %s\n", entry.ID)
	return 0
}

func runMergeQueue(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("merge queue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	processFake := fs.Bool("process-fake", false, "process next queued merge with fake runner")
	simulateConflict := fs.Bool("simulate-conflict", false, "simulate a fake merge conflict while processing")
	conflictReason := fs.String("conflict-reason", "", "fake conflict reason")
	retryConflict := fs.String("retry-conflict", "", "retry a merge_conflict entry with fake runner")
	cancelConflict := fs.String("cancel-conflict", "", "cancel a merge_conflict entry")
	dryRunRealGit := fs.Bool("dry-run-real-git", false, "run non-mutating git checks for the next queued merge")
	processRealGit := fs.Bool("process-real-git", false, "process next queued merge with local real git")
	execute := fs.Bool("execute", false, "execute real local merge")
	ffOnly := fs.Bool("ff-only", false, "require fast-forward only real merge")
	noPush := fs.Bool("no-push", false, "do not push after real merge")
	target := fs.String("target", "main", "target branch for real local merge")
	entryID := fs.String("entry", "", "merge queue entry id")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "merge_queue_failed", err)
	}
	defer db.Close()
	if *retryConflict != "" {
		result, err := db.RetryFakeMergeConflict(ctx, projectID, *retryConflict)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "merge_queue_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Merge conflict retried: %s -> %s\n", result.TaskID, result.TaskStatus)
		return 0
	}
	if *cancelConflict != "" {
		entry, err := db.CancelMergeConflict(ctx, projectID, *cancelConflict)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "merge_queue_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, entry, 0)
		}
		fmt.Fprintf(stdout, "Merge conflict cancelled: %s\n", entry.ID)
		return 0
	}
	if *dryRunRealGit {
		result, err := db.RunMergeGitDryRun(ctx, projectID, *entryID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "merge_git_dry_run_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Git merge dry-run: %s %s\n", result.TaskID, result.Status)
		for _, blocker := range result.Blockers {
			fmt.Fprintf(stdout, "blocker: %s\n", blocker)
		}
		return 0
	}
	if *processRealGit {
		result, err := db.ProcessRealGitMerge(ctx, projectID, storage.RealGitMergeInput{
			EntryID: *entryID,
			Target:  *target,
			Execute: *execute,
			FFOnly:  *ffOnly,
			NoPush:  *noPush,
		})
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "real_git_merge_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Real git merge: %s %s\n", result.TaskID, result.Status)
		for _, blocker := range result.Blockers {
			fmt.Fprintf(stdout, "blocker: %s\n", blocker)
		}
		return 0
	}
	if *processFake {
		if *simulateConflict {
			result, err := db.ProcessNextFakeMergeConflict(ctx, projectID, *conflictReason)
			if err != nil {
				return writeError(stdout, *jsonOut, exitValidation, "merge_queue_failed", err)
			}
			if *jsonOut {
				return writeJSON(stdout, result, 0)
			}
			fmt.Fprintf(stdout, "Merge conflict: %s\n", result.TaskID)
			return 0
		}
		result, err := db.ProcessNextFakeMerge(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "merge_queue_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Merged: %s\n", result.TaskID)
		return 0
	}
	entries, err := db.ListMergeQueue(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "merge_queue_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"entries": entries}, 0)
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "Merge queue is empty.")
		return 0
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", entry.ID, entry.Status, entry.TaskID, entry.HeadCommit)
	}
	return 0
}

func runPatch(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing patch subcommand")
		return exitValidation
	}
	switch args[0] {
	case "export":
		return runPatchExport(ctx, args[1:], stdout)
	case "mark-applied":
		return runPatchMarkApplied(ctx, args[1:], stdout)
	case "verify-applied":
		return runPatchVerifyApplied(ctx, args[1:], stdout)
	case "status":
		return runPatchStatus(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown patch subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runPatchExport(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("patch export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "patch_export_failed", err)
	}
	defer db.Close()
	patch, err := db.ExportPatch(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "patch_export_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, patch, 0)
	}
	fmt.Fprintf(stdout, "Patch exported: %s\n", patch.ID)
	fmt.Fprintf(stdout, "Task status: patch_exported\n")
	return 0
}

func runPatchMarkApplied(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("patch mark-applied", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	commit := fs.String("commit", "", "applied commit")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "patch_mark_applied_failed", err)
	}
	defer db.Close()
	patch, err := db.MarkPatchApplied(ctx, projectID, fs.Arg(0), *commit)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "patch_mark_applied_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, patch, 0)
	}
	fmt.Fprintf(stdout, "Patch marked applied: %s\n", patch.ID)
	fmt.Fprintf(stdout, "Task status: manually_applied\n")
	return 0
}

func runPatchVerifyApplied(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("patch verify-applied", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	adapter := fs.String("adapter", "local", "local or fake")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "patch_verify_applied_failed", err)
	}
	defer db.Close()
	patch, err := db.VerifyAppliedPatch(ctx, projectID, fs.Arg(0), *adapter)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "patch_verify_applied_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, patch, 0)
	}
	fmt.Fprintf(stdout, "Patch verified: %s\n", patch.ID)
	fmt.Fprintf(stdout, "Task status: applied\n")
	return 0
}

func runPatchStatus(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("patch status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	var taskID string
	if fs.NArg() > 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("at most one task id is allowed"))
	}
	if fs.NArg() == 1 {
		taskID = fs.Arg(0)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "patch_status_failed", err)
	}
	defer db.Close()
	patches, err := db.ListPatchApplications(ctx, projectID, taskID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "patch_status_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"patches": patches}, 0)
	}
	if len(patches) == 0 {
		fmt.Fprintln(stdout, "No patch applications.")
		return 0
	}
	for _, patch := range patches {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", patch.ID, patch.Status, patch.TaskID, patch.AppliedCommit)
	}
	return 0
}

func runCleanup(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) > 0 && args[0] == "quarantine" {
		return runCleanupQuarantine(ctx, args[1:], stdout)
	}
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	dryRun := fs.Bool("dry-run", true, "show cleanup plan without deleting")
	merged := fs.Bool("merged", false, "include merged tasks")
	applied := fs.Bool("applied", false, "include applied tasks")
	cancelled := fs.Bool("cancelled", false, "include cancelled tasks")
	failed := fs.Bool("failed", false, "include failed tasks")
	olderThan := fs.String("older-than", "", "minimum age, for example 14d or 72h")
	execute := fs.Bool("execute", false, "run cleanup execute guard without deleting")
	quarantine := fs.Bool("quarantine", false, "move eligible worktrees to quarantine instead of deleting")
	quarantineRoot := fs.String("quarantine-root", "", "directory for quarantined worktrees")
	deleteWorktrees := fs.Bool("delete", false, "permanently remove eligible worktrees")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if *quarantine && !*execute {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_failed", errors.New("--quarantine requires --execute"))
	}
	if *deleteWorktrees && !*execute {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_failed", errors.New("--delete requires --execute"))
	}
	if *deleteWorktrees && *quarantine {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_failed", errors.New("--delete and --quarantine are mutually exclusive"))
	}
	if !*dryRun && !*execute {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_failed", errors.New("cleanup changes require --execute or --dry-run"))
	}
	age, err := parseCleanupAge(*olderThan)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_failed", err)
	}
	db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "cleanup_failed", err)
	}
	defer db.Close()
	plan, err := db.BuildCleanupDryRunPlan(ctx, projectID, storage.CleanupPlanOptions{
		IncludeMerged:    *merged,
		IncludeApplied:   *applied,
		IncludeCancelled: *cancelled,
		IncludeFailed:    *failed,
		OlderThan:        age,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
	}
	record, err := db.SaveCleanupDryRunEvidence(ctx, projectID, plan)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
	}
	for _, item := range plan {
		safety, err := db.RunWorktreeSafetyCheck(ctx, projectID, item.TaskID, "")
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
		}
		record.WorktreeSafety = append(record.WorktreeSafety, safety)
	}
	if *execute {
		if *deleteWorktrees {
			deleteRecord, err := db.DeleteCleanupCandidates(ctx, projectID, record.Items, record.WorktreeSafety)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
			}
			if *jsonOut {
				return writeJSON(stdout, deleteRecord, 0)
			}
			fmt.Fprintf(stdout, "Cleanup delete: %s\n", deleteRecord.Status)
			fmt.Fprintf(stdout, "Actual delete enabled: %t\n", deleteRecord.ActualDeleteEnabled)
			for _, del := range deleteRecord.Deletes {
				fmt.Fprintf(stdout, "delete: %s\t%s\t%s\n", del.TaskID, del.Status, del.WorktreePath)
			}
			for _, blocker := range deleteRecord.Blockers {
				fmt.Fprintf(stdout, "blocker: %s\n", blocker)
			}
			return 0
		}
		if *quarantine {
			qRoot := strings.TrimSpace(*quarantineRoot)
			if qRoot == "" {
				dataRootForQuarantine := *dataRoot
				if strings.TrimSpace(dataRootForQuarantine) == "" {
					dataRootForQuarantine = filepath.Join(root, "orchestrator-data")
				}
				qRoot = filepath.Join(dataRootForQuarantine, "quarantine")
			}
			quarantineRecord, err := db.QuarantineCleanupCandidates(ctx, projectID, record.Items, record.WorktreeSafety, qRoot)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
			}
			if *jsonOut {
				return writeJSON(stdout, quarantineRecord, 0)
			}
			fmt.Fprintf(stdout, "Cleanup quarantine: %s\n", quarantineRecord.Status)
			fmt.Fprintf(stdout, "Actual delete enabled: %t\n", quarantineRecord.ActualDeleteEnabled)
			for _, move := range quarantineRecord.Moves {
				fmt.Fprintf(stdout, "quarantine: %s\t%s\t%s\n", move.TaskID, move.Status, move.QuarantinePath)
			}
			for _, blocker := range quarantineRecord.Blockers {
				fmt.Fprintf(stdout, "blocker: %s\n", blocker)
			}
			return 0
		}
		guard, err := db.SaveCleanupExecuteGuardEvidence(ctx, projectID, record.Items, record.WorktreeSafety)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "cleanup_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"items": guard.Items, "run_id": guard.RunID, "worktree_safety": guard.WorktreeSafety, "execute": true, "status": guard.Status, "actual_delete_enabled": guard.ActualDeleteEnabled, "blockers": guard.Blockers}, 0)
		}
		fmt.Fprintf(stdout, "Cleanup execute guard: %s\n", guard.Status)
		fmt.Fprintf(stdout, "Actual delete enabled: %t\n", guard.ActualDeleteEnabled)
		for _, blocker := range guard.Blockers {
			fmt.Fprintf(stdout, "blocker: %s\n", blocker)
		}
		return 0
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"items": record.Items, "run_id": record.RunID, "worktree_safety": record.WorktreeSafety, "dry_run": true}, 0)
	}
	if len(plan) == 0 {
		fmt.Fprintf(stdout, "No cleanup candidates. Evidence run: %s\n", record.RunID)
		return 0
	}
	fmt.Fprintf(stdout, "Evidence run: %s\n", record.RunID)
	for _, item := range plan {
		fmt.Fprintf(stdout, "%s\t%s\teligible=%t\t%s\n", item.TaskID, item.Status, item.Eligible, strings.Join(item.Blockers, "; "))
	}
	for _, safety := range record.WorktreeSafety {
		fmt.Fprintf(stdout, "worktree safety: %s\t%s\t%s\n", safety.TaskID, safety.Status, strings.Join(safety.Blockers, "; "))
	}
	return 0
}

func parseCleanupAge(input string) (time.Duration, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, nil
	}
	if strings.HasSuffix(input, "d") {
		daysText := strings.TrimSuffix(input, "d")
		days, err := time.ParseDuration(daysText + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid --older-than value: %s", input)
		}
		return days * 24, nil
	}
	duration, err := time.ParseDuration(input)
	if err != nil {
		return 0, fmt.Errorf("invalid --older-than value: %s", input)
	}
	return duration, nil
}

func runCleanupQuarantine(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) == 0 {
		return writeError(stdout, false, exitValidation, "invalid_arguments", errors.New("cleanup quarantine requires list or restore"))
	}
	switch args[0] {
	case "list":
		return runCleanupQuarantineList(ctx, args[1:], stdout)
	case "restore":
		return runCleanupQuarantineRestore(ctx, args[1:], stdout)
	default:
		return writeError(stdout, false, exitValidation, "invalid_arguments", fmt.Errorf("unknown cleanup quarantine subcommand: %s", args[0]))
	}
}

func runCleanupQuarantineList(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("cleanup quarantine list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "cleanup_quarantine_list_failed", err)
	}
	defer db.Close()
	entries, err := db.ListCleanupQuarantine(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "cleanup_quarantine_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"entries": entries}, 0)
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No quarantined cleanup worktrees.")
		return 0
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", entry.RunID, entry.TaskID, entry.Status, entry.QuarantinePath)
	}
	return 0
}

func runCleanupQuarantineRestore(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("cleanup quarantine restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	runID := fs.String("run", "", "cleanup quarantine run id")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "cleanup_quarantine_restore_failed", err)
	}
	defer db.Close()
	record, err := db.RestoreCleanupQuarantine(ctx, projectID, fs.Arg(0), *runID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "cleanup_quarantine_restore_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "Cleanup quarantine restore: %s\n", record.Status)
	fmt.Fprintf(stdout, "Task: %s\n", record.TaskID)
	for _, blocker := range record.Blockers {
		fmt.Fprintf(stdout, "blocker: %s\n", blocker)
	}
	return 0
}

func runPublish(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	remote := fs.String("remote", "origin", "git remote")
	branch := fs.String("branch", "main", "branch")
	dryRun := fs.Bool("dry-run", true, "only capture publish readiness evidence")
	execute := fs.Bool("execute", false, "execute publish")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "publish_failed", err)
	}
	defer db.Close()
	if *execute {
		result, err := db.PublishExecute(ctx, projectID, storage.PublishExecuteInput{Remote: *remote, Branch: *branch})
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "publish_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Publish execute: %s\n", result.Status)
		fmt.Fprintf(stdout, "Relation before: %s\n", result.RelationBefore)
		for _, blocker := range result.Blockers {
			fmt.Fprintf(stdout, "blocker: %s\n", blocker)
		}
		return 0
	}
	if !*dryRun {
		return writeError(stdout, *jsonOut, exitValidation, "publish_failed", errors.New("publish requires --dry-run or --execute"))
	}
	result, err := db.PublishDryRun(ctx, projectID, storage.PublishDryRunInput{Remote: *remote, Branch: *branch})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "publish_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Publish dry-run: %s\n", result.Status)
	fmt.Fprintf(stdout, "Relation: %s\n", result.Relation)
	for _, blocker := range result.Blockers {
		fmt.Fprintf(stdout, "blocker: %s\n", blocker)
	}
	return 0
}

func runApproveEvidence(ctx context.Context, args []string, stdout io.Writer, approvalType storage.ApprovalType) int {
	fs := flag.NewFlagSet(string(approvalType)+" approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	notes := fs.String("notes", "", "approval notes")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "approval_failed", err)
	}
	defer db.Close()
	result, err := db.ApproveTaskEvidence(ctx, storage.ApprovalInput{
		ProjectID:    projectID,
		TaskID:       fs.Arg(0),
		ApprovalType: approvalType,
		Notes:        *notes,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "approval_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Approval recorded: %s\n", result.ID)
	fmt.Fprintf(stdout, "Task status: %s\n", result.TaskStatus)
	return 0
}

func runInbox(ctx context.Context, args []string, stdout io.Writer) int {
	if len(args) > 0 && args[0] == "approve" {
		return runInboxApprove(ctx, args[1:], stdout)
	}
	fs := flag.NewFlagSet("inbox", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	status := fs.String("status", "open", "inbox item status")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if err := storage.ValidateInboxStatus(*status); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_inbox_status", err)
	}
	root, err := preflight.ResolveProjectRoot(*projectRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "project_root_failed", err)
	}
	db, _, err := openProjectDB(ctx, root, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "storage_open_failed", err)
	}
	defer db.Close()
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "migration_registry_failed", err)
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "migration_failed", err)
	}
	items, err := db.ListInboxItems(ctx, storage.ProjectIDForRoot(root), *status)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "inbox_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"items": items}, 0)
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "Inbox is empty.")
		return 0
	}
	for _, item := range items {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", item.ID, item.Status, item.ItemType, item.Title)
	}
	return 0
}

func runInboxApprove(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("inbox approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	option := fs.String("option", "", "selected option for decision sources")
	notes := fs.String("notes", "", "approval notes")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("inbox approve requires INBOX_ID"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "inbox_approve_failed", err)
	}
	defer db.Close()
	result, err := db.ApproveInboxItem(ctx, storage.InboxApprovalInput{
		ProjectID: projectID,
		InboxID:   fs.Arg(0),
		Option:    *option,
		Notes:     *notes,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "inbox_approve_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\n", result.InboxID, result.SourceType, result.SourceID)
	return 0
}

func runDecisions(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("decisions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	status := fs.String("status", "", "decision status")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "decisions_list_failed", err)
	}
	defer db.Close()
	decisions, err := db.ListDecisions(ctx, projectID, *status)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "decisions_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"decisions": decisions}, 0)
	}
	if len(decisions) == 0 {
		fmt.Fprintln(stdout, "No decisions.")
		return 0
	}
	for _, decision := range decisions {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", decision.ID, decision.Status, decision.Title)
	}
	return 0
}

func runApproveDecision(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	option := fs.String("option", "", "selected option")
	notes := fs.String("notes", "", "approval notes")
	remember := fs.Bool("remember", false, "record approved decision as policy memory")
	memoryKey := fs.String("memory-key", "", "policy memory key")
	memoryScope := fs.String("memory-scope", "project", "policy memory scope")
	memoryScopeID := fs.String("memory-scope-id", "", "policy memory scope id")
	memoryExpiresAt := fs.String("memory-expires-at", "", "policy memory expiry timestamp")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("approve requires DECISION_ID"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "decision_approve_failed", err)
	}
	defer db.Close()
	decision, err := db.ApproveDecision(ctx, storage.DecisionApprovalInput{
		ProjectID:  projectID,
		DecisionID: fs.Arg(0),
		Option:     *option,
		Notes:      *notes,
		Remember:   *remember,
		Memory: storage.RememberDecisionInput{
			Key:       *memoryKey,
			Scope:     *memoryScope,
			ScopeID:   *memoryScopeID,
			ExpiresAt: *memoryExpiresAt,
		},
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "decision_approve_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, decision, 0)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\n", decision.ID, decision.Status, decision.SelectedOption)
	return 0
}

func runMemory(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("memory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	memoryType := fs.String("type", "", "memory type")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "memory_list_failed", err)
	}
	defer db.Close()
	memories, err := db.ListMemories(ctx, projectID, *memoryType)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "memory_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"memories": memories}, 0)
	}
	if len(memories) == 0 {
		fmt.Fprintln(stdout, "No memories.")
		return 0
	}
	for _, memory := range memories {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", memory.ID, memory.MemoryType, memory.Scope, memory.Key)
	}
	return 0
}

func runDependency(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing dependency subcommand")
		return exitValidation
	}
	switch args[0] {
	case "approval":
		return runDependencyApproval(ctx, args[1:], stdout, stderr)
	case "risk":
		return runDependencyRisk(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown dependency subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runDependencyApproval(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing dependency approval subcommand")
		return exitValidation
	}
	switch args[0] {
	case "request":
		return runDependencyApprovalRequest(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown dependency approval subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runDependencyApprovalRequest(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("dependency approval request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	name := fs.String("name", "", "dependency package name")
	manager := fs.String("manager", "", "package manager")
	dependencyType := fs.String("type", "", "production, development, or tool")
	reason := fs.String("reason", "", "reason for adding dependency")
	risk := fs.String("risk", "medium", "low, medium, high, or critical")
	alternatives := fs.String("alternatives", "", "alternatives considered")
	filesAffected := fs.String("files-affected", "", "package or lock files affected")
	lifecycleScripts := fs.String("lifecycle-scripts", "unknown", "none_detected, detected, or unknown")
	currentVersion := fs.String("current-version", "", "current/resolved version")
	approvedScope := fs.String("approved-scope", "project", "project, task, one_time, or dependency_family")
	taskID := fs.String("task", "", "introducing task id")
	runID := fs.String("run", "", "introducing run id")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "dependency_approval_request_failed", err)
	}
	defer db.Close()
	result, err := db.RequestDependencyApproval(ctx, storage.DependencyApprovalRequestInput{
		ProjectID:        projectID,
		Name:             *name,
		PackageManager:   *manager,
		DependencyType:   *dependencyType,
		Reason:           *reason,
		Risk:             *risk,
		Alternatives:     *alternatives,
		FilesAffected:    *filesAffected,
		LifecycleScripts: *lifecycleScripts,
		CurrentVersion:   *currentVersion,
		ApprovedScope:    *approvedScope,
		IntroducedTaskID: *taskID,
		IntroducedRunID:  *runID,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "dependency_approval_request_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Dependency approval requested: %s inbox=%s\n", result.DecisionID, result.InboxID)
	return 0
}

func runDependencyRisk(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing dependency risk subcommand")
		return exitValidation
	}
	switch args[0] {
	case "add":
		return runDependencyRiskAdd(ctx, args[1:], stdout)
	case "list":
		return runDependencyRiskList(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown dependency risk subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runDependencyRiskAdd(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("dependency risk add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	name := fs.String("name", "", "dependency package name")
	manager := fs.String("manager", "", "package manager")
	dependencyType := fs.String("type", "", "production, development, or tool")
	taskID := fs.String("task", "", "introducing task id")
	runID := fs.String("run", "", "introducing run id")
	decisionID := fs.String("decision", "", "decision id")
	reason := fs.String("reason", "", "reason for adding dependency")
	approvedBy := fs.String("approved-by", "", "approver")
	risk := fs.String("risk", "", "low, medium, high, or critical")
	lockfileChanged := fs.Bool("lockfile-changed", false, "whether lockfile changed")
	lifecycleScripts := fs.String("lifecycle-scripts", "unknown", "none_detected, detected, or unknown")
	currentVersion := fs.String("current-version", "", "current/resolved version")
	approvedScope := fs.String("approved-scope", "project", "project, task, one_time, or dependency_family")
	expiresAt := fs.String("expires-at", "", "RFC3339 expiry")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "dependency_risk_add_failed", err)
	}
	defer db.Close()
	record, err := db.RecordDependencyRisk(ctx, storage.DependencyRiskInput{
		ProjectID:          projectID,
		Name:               *name,
		PackageManager:     *manager,
		DependencyType:     *dependencyType,
		IntroducedByTaskID: *taskID,
		IntroducedByRunID:  *runID,
		DecisionID:         *decisionID,
		Reason:             *reason,
		ApprovedBy:         *approvedBy,
		Risk:               *risk,
		LockfileChanged:    *lockfileChanged,
		LifecycleScripts:   *lifecycleScripts,
		CurrentVersion:     *currentVersion,
		ApprovedScope:      *approvedScope,
		ExpiresAt:          *expiresAt,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "dependency_risk_add_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", record.ID, record.PackageManager, record.Name, record.Risk)
	return 0
}

func runDependencyRiskList(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("dependency risk list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	manager := fs.String("manager", "", "package manager")
	dependencyType := fs.String("type", "", "production, development, or tool")
	risk := fs.String("risk", "", "low, medium, high, or critical")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "dependency_risk_list_failed", err)
	}
	defer db.Close()
	records, err := db.ListDependencyRisks(ctx, storage.DependencyRiskListFilter{
		ProjectID:      projectID,
		PackageManager: *manager,
		DependencyType: *dependencyType,
		Risk:           *risk,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "dependency_risk_list_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"dependencies": records}, 0)
	}
	if len(records) == 0 {
		fmt.Fprintln(stdout, "No dependency risks.")
		return 0
	}
	for _, record := range records {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", record.ID, record.PackageManager, record.Name, record.Risk)
	}
	return 0
}

func runUI(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing ui subcommand")
		return exitValidation
	}
	switch args[0] {
	case "snapshot":
		fs := flag.NewFlagSet("ui snapshot", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		limit := fs.Int("limit", 20, "maximum open inbox items")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "ui_snapshot_failed", err)
		}
		defer db.Close()
		snapshot, err := db.LoadHumanInboxSnapshot(ctx, projectID, *limit)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "ui_snapshot_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, snapshot, 0)
		}
		fmt.Fprintf(stdout, "Human Inbox snapshot: %s\n", snapshot.ProjectID)
		fmt.Fprintf(stdout, "Open inbox: %d\n", snapshot.Counts.OpenInboxItems)
		fmt.Fprintf(stdout, "Running tasks: %d\n", snapshot.Counts.RunningTasks)
		fmt.Fprintf(stdout, "Waiting for human: %d\n", snapshot.Counts.WaitingForHumanTasks)
		for _, item := range snapshot.OpenInboxItems {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", item.ID, item.ItemType, item.Title)
		}
		return 0
	case "dashboard":
		fs := flag.NewFlagSet("ui dashboard", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		limit := fs.Int("limit", 20, "maximum open inbox items")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "ui_dashboard_failed", err)
		}
		defer db.Close()
		dashboard, err := db.LoadProjectDashboard(ctx, projectID, *limit)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "ui_dashboard_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, dashboard, 0)
		}
		fmt.Fprintf(stdout, "Dashboard: %s\n", dashboard.Snapshot.ProjectID)
		fmt.Fprintf(stdout, "Tasks: %d\n", len(dashboard.Tasks))
		fmt.Fprintf(stdout, "Inbox: %d\n", len(dashboard.Snapshot.OpenInboxItems))
		return 0
	case "setup":
		fs := flag.NewFlagSet("ui setup", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "ui_setup_failed", err)
		}
		defer db.Close()
		status, err := db.LoadSetupStatus(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "ui_setup_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, status, 0)
		}
		fmt.Fprintf(stdout, "Project root: %s\n", status.ProjectRoot)
		fmt.Fprintf(stdout, "Git clean: %t\n", status.GitClean)
		fmt.Fprintf(stdout, "Required verification configured: %t\n", status.RequiredVerificationConfigured)
		return 0
	case "setup-action":
		fs := flag.NewFlagSet("ui setup-action", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", fmt.Errorf("setup action id is required"))
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "ui_setup_action_failed", err)
		}
		defer db.Close()
		result, err := db.RunSetupAction(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "ui_setup_action_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, 0)
		}
		fmt.Fprintf(stdout, "Setup action %s: %s\n", result.ActionID, result.Status)
		fmt.Fprintln(stdout, result.Message)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown ui subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runServe(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	registryPath := fs.String("registry", "", "global registry DB path")
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	allowLAN := fs.Bool("allow-lan", false, "allow non-localhost bind with warning")
	localToken := fs.String("local-token", os.Getenv("DEVOS_LOCAL_TOKEN"), "local API token required for sensitive POST routes")
	serveUI := fs.Bool("ui", false, "serve built React UI from ui/dist")
	uiDir := fs.String("ui-dir", "ui/dist", "built UI directory")
	openBrowserFlag := fs.Bool("open", false, "open the UI in the default browser")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout before serving")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if !*allowLAN && isLANBindAddress(*addr) {
		return writeError(stdout, *jsonOut, exitValidation, "unsafe_bind_address", fmt.Errorf("refusing to bind %s without --allow-lan", *addr))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "serve_failed", err)
	}
	defer db.Close()
	regDB, errCode, err := openRegistryDB(ctx, *registryPath)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "serve_failed", err)
	}
	defer regDB.Close()
	apiHandler := api.NewServerWithHub(db, projectID, projecthub.NewDefaultHub(regDB)).WithLocalToken(*localToken).Handler()
	handler := apiHandler
	if *serveUI {
		if _, err := os.Stat(filepath.Join(*uiDir, "index.html")); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "ui_dist_missing", fmt.Errorf("UI dist is not available at %s; run corepack pnpm --dir ui build", *uiDir))
		}
		handler = uiStaticHandler(apiHandler, *uiDir)
	}
	server := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}
	if *jsonOut {
		_ = writeJSON(stdout, map[string]any{"addr": *addr, "project_id": projectID, "ui": *serveUI, "local_token_required": strings.TrimSpace(*localToken) != ""}, 0)
	} else {
		label := "API"
		if *serveUI {
			label = "UI/API"
		}
		fmt.Fprintf(stdout, "DevOS %s serving on http://%s\n", label, *addr)
		if *allowLAN && isLANBindAddress(*addr) {
			fmt.Fprintf(stdout, "Warning: LAN bind enabled for %s. Use --local-token or DEVOS_LOCAL_TOKEN for sensitive routes.\n", *addr)
		}
	}
	if *openBrowserFlag {
		openBrowser("http://" + *addr)
	}
	if err := server.ListenAndServe(); err != nil {
		return writeError(stdout, *jsonOut, exitInternal, "serve_failed", err)
	}
	return 0
}

func isLANBindAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	return host == "" || host == "0.0.0.0" || host == "::"
}

func projectStatusSummary(ctx context.Context, db *storage.DB, projectID string) (map[string]any, error) {
	var projectName string
	if err := db.SQL().QueryRowContext(ctx, "SELECT name FROM projects WHERE id = ?", projectID).Scan(&projectName); err != nil {
		return nil, err
	}
	count := func(query string, args ...any) (int, error) {
		var value int
		err := db.SQL().QueryRowContext(ctx, query, args...).Scan(&value)
		return value, err
	}
	openTasks, err := count("SELECT COUNT(*) FROM tasks WHERE project_id = ? AND status NOT IN ('merged', 'applied', 'cancelled', 'failed')", projectID)
	if err != nil {
		return nil, err
	}
	blockedTasks, err := count("SELECT COUNT(*) FROM tasks WHERE project_id = ? AND status IN ('needs_input', 'needs_decision', 'blocked_on_environment', 'blocked_on_policy', 'merge_conflict', 'failed')", projectID)
	if err != nil {
		return nil, err
	}
	openInbox, err := count("SELECT COUNT(*) FROM inbox_items WHERE project_id = ? AND status = 'open'", projectID)
	if err != nil {
		return nil, err
	}
	mergeQueue, err := count("SELECT COUNT(*) FROM merge_queue_entries WHERE project_id = ? AND status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict')", projectID)
	if err != nil {
		return nil, err
	}
	lastRun := "none"
	var runID, runType, runStatus, taskID sql.NullString
	err = db.SQL().QueryRowContext(ctx, "SELECT id, run_type, status, task_id FROM runs WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1", projectID).Scan(&runID, &runType, &runStatus, &taskID)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if runID.Valid {
		lastRun = strings.TrimSpace(taskID.String + " " + runID.String + " " + runType.String + " " + runStatus.String)
	}
	nextAction := "no action"
	switch {
	case openInbox > 0:
		nextAction = "review open Human Inbox items"
	case blockedTasks > 0:
		nextAction = "inspect blocked task with devos task show"
	case mergeQueue > 0:
		nextAction = "process or review merge queue"
	case openTasks > 0:
		nextAction = "continue queued or ready tasks"
	}
	return map[string]any{
		"project":       projectName,
		"project_id":    projectID,
		"open_tasks":    openTasks,
		"blocked_tasks": blockedTasks,
		"open_inbox":    openInbox,
		"merge_queue":   mergeQueue,
		"last_run":      lastRun,
		"next_action":   nextAction,
	}, nil
}

func taskDetail(ctx context.Context, db *storage.DB, projectID string, taskID string) (map[string]any, error) {
	var title, status, baseBranch sql.NullString
	if err := db.SQL().QueryRowContext(ctx, "SELECT title, status, base_branch FROM tasks WHERE project_id = ? AND id = ?", projectID, taskID).Scan(&title, &status, &baseBranch); err != nil {
		return nil, err
	}
	detail := map[string]any{
		"id":                  taskID,
		"title":               title.String,
		"status":              status.String,
		"base_branch":         baseBranch.String,
		"latest_run_id":       "",
		"worktree_path":       "",
		"candidate_commit":    "",
		"diff_hash":           "",
		"verification_status": "",
		"merge_queue_status":  "",
		"artifacts":           []map[string]string{},
	}
	var latestRunID, runStatus, headCommit, diffHash sql.NullString
	err := db.SQL().QueryRowContext(ctx, `
SELECT id, status, COALESCE(head_commit, ''), COALESCE(diff_hash, '')
FROM runs
WHERE project_id = ? AND task_id = ?
ORDER BY updated_at DESC
LIMIT 1`, projectID, taskID).Scan(&latestRunID, &runStatus, &headCommit, &diffHash)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if latestRunID.Valid {
		detail["latest_run_id"] = latestRunID.String
		detail["candidate_commit"] = headCommit.String
		detail["diff_hash"] = diffHash.String
		var cwd sql.NullString
		if err := db.SQL().QueryRowContext(ctx, "SELECT cwd FROM command_events WHERE project_id = ? AND run_id = ? ORDER BY created_at LIMIT 1", projectID, latestRunID.String).Scan(&cwd); err == nil {
			detail["worktree_path"] = cwd.String
		}
	}
	var verificationStatus sql.NullString
	err = db.SQL().QueryRowContext(ctx, "SELECT status FROM runs WHERE project_id = ? AND task_id = ? AND run_type IN ('verification', 'reverify') ORDER BY updated_at DESC LIMIT 1", projectID, taskID).Scan(&verificationStatus)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if verificationStatus.Valid {
		detail["verification_status"] = verificationStatus.String
	}
	var mergeStatus sql.NullString
	err = db.SQL().QueryRowContext(ctx, "SELECT status FROM merge_queue_entries WHERE project_id = ? AND task_id = ? ORDER BY updated_at DESC LIMIT 1", projectID, taskID).Scan(&mergeStatus)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if mergeStatus.Valid {
		detail["merge_queue_status"] = mergeStatus.String
	}
	if latestRunID.Valid {
		rows, err := db.SQL().QueryContext(ctx, "SELECT artifact_type, artifact_key, path FROM run_artifacts WHERE project_id = ? AND run_id = ? ORDER BY artifact_type, artifact_key", projectID, latestRunID.String)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var artifacts []map[string]string
		for rows.Next() {
			var artifactType, artifactKey, path string
			if err := rows.Scan(&artifactType, &artifactKey, &path); err != nil {
				return nil, err
			}
			artifacts = append(artifacts, map[string]string{"type": artifactType, "key": artifactKey, "path": path})
		}
		detail["artifacts"] = artifacts
	}
	return detail, nil
}

func lastRunSummary(ctx context.Context, db *storage.DB, projectID string, runType string) map[string]any {
	var runID, status, taskID, headCommit, diffHash sql.NullString
	err := db.SQL().QueryRowContext(ctx, `
SELECT id, status, COALESCE(task_id, ''), COALESCE(head_commit, ''), COALESCE(diff_hash, '')
FROM runs
WHERE project_id = ? AND run_type = ?
ORDER BY updated_at DESC
LIMIT 1`, projectID, runType).Scan(&runID, &status, &taskID, &headCommit, &diffHash)
	if err != nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":          runID.String,
		"status":      status.String,
		"task_id":     taskID.String,
		"head_commit": headCommit.String,
		"diff_hash":   diffHash.String,
	}
}

func uiStaticHandler(apiHandler http.Handler, uiDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(uiDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		path := filepath.Join(uiDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(uiDir, "index.html"))
	})
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func runEnv(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing env subcommand")
		return exitValidation
	}
	switch args[0] {
	case "status":
		return runEnvStatus(ctx, args[1:], stdout)
	case "set":
		return runEnvSet(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown env subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runEnvSet(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("env set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	scope := fs.String("scope", "project", "binding scope")
	scopeID := fs.String("scope-id", "", "scope id")
	environmentID := fs.String("env", "", "environment id")
	valueStdin := fs.Bool("value-stdin", false, "read value from stdin")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("environment key is required"))
	}
	if !*valueStdin {
		return writeError(stdout, *jsonOut, exitValidation, "env_set_failed", errors.New("--value-stdin is required in non-interactive mode"))
	}
	valueBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "env_set_failed", err)
	}
	value := strings.TrimRight(string(valueBytes), "\r\n")
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "env_set_failed", err)
	}
	defer db.Close()
	record, err := db.SaveEnvBinding(ctx, storage.EnvBindingInput{
		ProjectID:     projectID,
		EnvironmentID: *environmentID,
		Key:           fs.Arg(0),
		Scope:         *scope,
		ScopeID:       *scopeID,
		Value:         value,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "env_set_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, record, 0)
	}
	fmt.Fprintf(stdout, "Environment binding configured: %s %s\n", record.Key, record.Scope)
	return 0
}

func runEnvStatus(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("env status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "env_status_failed", err)
	}
	defer db.Close()
	envs, err := db.ListExecutionEnvironments(ctx, projectID)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "env_status_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"environments": envs}, 0)
	}
	if len(envs) == 0 {
		fmt.Fprintln(stdout, "No environments.")
		return 0
	}
	for _, env := range envs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", env.ID, env.Role, env.OSFamily, env.Status)
	}
	return 0
}

func runInit(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	concept := strings.TrimSpace(strings.Join(fs.Args(), " "))
	result, err := preflight.InitProject(ctx, *projectRoot, concept)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "init_failed", err)
	}
	toolchainReport := toolchains.RunDoctor(ctx, result.PreflightReport.Environment, toolchains.Options{IncludeCodex: false})
	db, dbPath, err := openProjectDB(ctx, result.ProjectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "storage_open_failed", err)
	}
	defer db.Close()
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "migration_registry_failed", err)
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "migration_failed", err)
	}
	projectRecord, err := db.SaveProjectInit(ctx, storage.ProjectInitInput{
		RootPath:        result.ProjectRoot,
		Environment:     result.PreflightReport.Environment,
		PreflightReport: result.PreflightReport,
		ToolchainReport: &toolchainReport,
	})
	if err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "project_save_failed", err)
	}
	if err := db.SaveToolchainReport(ctx, projectRecord.ID, toolchainReport); err != nil {
		return writeError(stdout, *jsonOut, exitStorage, "toolchain_save_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{
			"project":          projectRecord,
			"database_path":    dbPath,
			"init_result":      result,
			"toolchain_report": toolchainReport,
		}, exitFromPreflight(result.PreflightReport))
	}

	fmt.Fprintf(stdout, "Project initialized: %s\n", result.ProjectRoot)
	fmt.Fprintf(stdout, "Project record: %s\n", projectRecord.ID)
	fmt.Fprintf(stdout, "Database: %s\n", dbPath)
	for _, path := range result.CreatedPaths {
		fmt.Fprintf(stdout, "created: %s\n", path)
	}
	printFindings(stdout, result.PreflightReport)
	return exitFromPreflight(result.PreflightReport)
}

func runPreflight(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	repairSchemas := fs.Bool("repair-schemas", false, "restore orchestrator-owned schema registry files")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if *repairSchemas {
		result, err := preflight.RepairSchemas(ctx, *projectRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "preflight_repair_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, result, exitFromPreflight(result.PreflightReport))
		}
		fmt.Fprintf(stdout, "Schema registry repaired: %s\n", result.SchemaInstall.Root)
		for _, path := range result.SchemaInstall.CreatedPaths {
			fmt.Fprintf(stdout, "created: %s\n", path)
		}
		for _, path := range result.SchemaInstall.UpdatedPaths {
			fmt.Fprintf(stdout, "updated: %s\n", path)
		}
		printFindings(stdout, result.PreflightReport)
		return exitFromPreflight(result.PreflightReport)
	}
	report, err := preflight.Run(ctx, *projectRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "preflight_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, report, exitFromPreflight(report))
	}
	fmt.Fprintf(stdout, "Preflight: %s\n", report.ProjectRoot)
	fmt.Fprintf(stdout, "Environment: %s (%s)\n", report.Environment.ID, report.Environment.OSFamily)
	printFindings(stdout, report)
	return exitFromPreflight(report)
}

func runPlatform(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing platform subcommand")
		return exitValidation
	}
	switch args[0] {
	case "detect":
		fs := flag.NewFlagSet("platform detect", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		root, err := preflight.ResolveProjectRoot(*projectRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "platform_detect_failed", err)
		}
		env := platform.DetectHostEnvironment(root)
		if *jsonOut {
			return writeJSON(stdout, env, 0)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", env.ID, env.OSFamily, env.Shell, env.ProjectRoot)
		return 0
	case "profile":
		return runPlatformProfile(ctx, args[1:], stdout, stderr)
	case "map":
		return runPlatformMap(ctx, args[1:], stdout, stderr)
	case "setup":
		return runPlatformSetup(ctx, args[1:], stdout, stderr)
	case "codex-readiness":
		fs := flag.NewFlagSet("platform codex-readiness", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		fromFile := fs.String("from-file", "", "import codex readiness JSON produced in another runtime")
		save := fs.Bool("save", false, "save runtime issues to Human Inbox")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "codex_readiness_failed", err)
		}
		defer db.Close()
		var report storage.CodexRuntimeReadinessReport
		if strings.TrimSpace(*fromFile) != "" {
			report, err = readCodexReadinessReportFile(*fromFile)
			if err != nil {
				return writeError(stdout, *jsonOut, exitValidation, "codex_readiness_import_failed", err)
			}
			if err := validateCodexReadinessEnvironments(ctx, db, projectID, report); err != nil {
				return writeError(stdout, *jsonOut, exitValidation, "codex_readiness_import_failed", err)
			}
		} else {
			report, err = db.CodexRuntimeReadiness(ctx, projectID)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "codex_readiness_failed", err)
			}
		}
		if len(report.Items) == 0 {
			return writeError(stdout, *jsonOut, exitValidation, "codex_readiness_failed", fmt.Errorf("no execution environments configured for project; run devos init --project-root %s first", root))
		}
		var inboxItems []storage.InboxItem
		toolchainReportsSaved := 0
		if *save {
			inboxItems, err = db.SaveCodexRuntimeReadiness(ctx, projectID, report)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "codex_readiness_failed", err)
			}
			toolchainReportsSaved, err = saveCodexReadinessToolchainReports(ctx, db, projectID, report)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "codex_readiness_toolchain_save_failed", err)
			}
		}
		if *jsonOut {
			if *save {
				return writeJSON(stdout, map[string]any{"report": report, "inbox_items": inboxItems, "toolchain_reports_saved": toolchainReportsSaved}, 0)
			}
			return writeJSON(stdout, report, 0)
		}
		fmt.Fprintf(stdout, "Codex runtime host: %s\n", report.HostGOOS)
		for _, item := range report.Items {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", item.EnvironmentID, item.OSFamily, item.Classification, item.ExpectedHostRuntime)
		}
		if *save {
			fmt.Fprintf(stdout, "Open runtime inbox items: %d\n", len(inboxItems))
			fmt.Fprintf(stdout, "Toolchain reports saved: %d\n", toolchainReportsSaved)
		}
		return 0
	case "doctor":
		fs := flag.NewFlagSet("platform doctor", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		envID := fs.String("env", "", "execution environment id")
		includeCodex := fs.Bool("include-codex", false, "include real Codex adapter preflight")
		includeUI := fs.Bool("include-ui", false, "include UI toolchain preflight")
		save := fs.Bool("save", false, "save toolchain requirements and setup cards")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		root, err := preflight.ResolveProjectRoot(*projectRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "platform_doctor_failed", err)
		}
		env := platform.DetectHostEnvironment(root)
		var db *storage.DB
		var projectID string
		if *envID != "" || *save {
			var errCode int
			var err error
			db, projectID, errCode, err = openMigratedProjectDB(ctx, root, *dataRoot)
			if err != nil {
				return writeError(stdout, *jsonOut, errCode, "platform_doctor_failed", err)
			}
			defer db.Close()
		}
		if *envID != "" {
			envs, err := db.ListExecutionEnvironments(ctx, projectID)
			if err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "platform_doctor_failed", err)
			}
			found := false
			for _, candidate := range envs {
				if candidate.ID == *envID {
					env = candidate
					found = true
					break
				}
			}
			if !found {
				return writeError(stdout, *jsonOut, exitValidation, "platform_doctor_failed", fmt.Errorf("execution environment not found: %s", *envID))
			}
		}
		report := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: *includeCodex, IncludeUI: *includeUI})
		if *save {
			if err := db.SaveToolchainReport(ctx, projectID, report); err != nil {
				return writeError(stdout, *jsonOut, exitStorage, "platform_doctor_save_failed", err)
			}
		}
		if *jsonOut {
			return writeJSON(stdout, report, exitFromToolchainDoctor(report))
		}
		fmt.Fprintf(stdout, "Toolchain doctor: %s\n", report.EnvironmentID)
		for _, req := range report.Requirements {
			fmt.Fprintf(stdout, "[%s] %s: %s\n", req.Status, req.ToolchainKey, req.Message)
		}
		return exitFromToolchainDoctor(report)
	default:
		fmt.Fprintf(stderr, "unknown platform subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runPlatformSetup(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing platform setup subcommand")
		return exitValidation
	}
	switch args[0] {
	case "instructions":
		fs := flag.NewFlagSet("platform setup instructions", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("platform setup instructions requires INBOX_ID"))
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "setup_instructions_failed", err)
		}
		defer db.Close()
		instructions, err := db.ToolchainSetupInstructions(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "setup_instructions_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, instructions, 0)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", instructions.InboxID, instructions.EnvironmentID, instructions.ToolchainKey)
		for _, line := range instructions.Instructions {
			fmt.Fprintf(stdout, "- %s\n", line)
		}
		fmt.Fprintf(stdout, "Rerun: %s\n", instructions.RerunCommand)
		return 0
	case "mark-installed":
		fs := flag.NewFlagSet("platform setup mark-installed", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		includeCodex := fs.Bool("include-codex", false, "include real Codex adapter preflight")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("platform setup mark-installed requires INBOX_ID"))
		}
		db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "setup_mark_installed_failed", err)
		}
		defer db.Close()
		instructions, err := db.ToolchainSetupInstructions(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "setup_mark_installed_failed", err)
		}
		env := platform.DetectHostEnvironment(root)
		if env.ID != instructions.EnvironmentID {
			return writeError(stdout, *jsonOut, exitValidation, "setup_mark_installed_failed", fmt.Errorf("setup card belongs to %s, current environment is %s", instructions.EnvironmentID, env.ID))
		}
		report := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: *includeCodex})
		if err := db.SaveToolchainReport(ctx, projectID, report); err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "setup_mark_installed_failed", err)
		}
		item, err := db.GetInboxItem(ctx, projectID, fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "setup_mark_installed_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"report": report, "inbox_item": item, "resolved": item.Status == "resolved"}, exitFromToolchainDoctor(report))
		}
		fmt.Fprintf(stdout, "%s\t%s\n", item.ID, item.Status)
		return exitFromToolchainDoctor(report)
	case "waive":
		fs := flag.NewFlagSet("platform setup waive", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		reason := fs.String("reason", "", "waiver reason")
		scope := fs.String("scope", "", "waiver scope")
		expiry := fs.String("expiry", "", "waiver expiry in RFC3339")
		allowedEffect := fs.String("allowed-effect", "", "report_only, allow_non_merge_without_toolchain, or allow_merge_without_toolchain")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("platform setup waive requires INBOX_ID"))
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "setup_waive_failed", err)
		}
		defer db.Close()
		waiver, err := db.WaiveToolchainRequirement(ctx, storage.ToolchainWaiverInput{
			ProjectID:     projectID,
			InboxID:       fs.Arg(0),
			Reason:        *reason,
			Scope:         *scope,
			Expiry:        *expiry,
			AllowedEffect: *allowedEffect,
		})
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "setup_waive_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, waiver, 0)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", waiver.InboxID, waiver.Status, waiver.AllowedEffect)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown platform setup subcommand: %s\n", args[0])
		return exitValidation
	}
}

func saveCodexReadinessToolchainReports(ctx context.Context, db *storage.DB, projectID string, report storage.CodexRuntimeReadinessReport) (int, error) {
	envs, err := db.ListExecutionEnvironments(ctx, projectID)
	if err != nil {
		return 0, err
	}
	envByID := map[string]platform.ExecutionEnvironment{}
	for _, env := range envs {
		envByID[env.ID] = env
	}
	saved := 0
	for _, item := range report.Items {
		if item.Classification != "toolchain_required" {
			continue
		}
		env, ok := envByID[item.EnvironmentID]
		if !ok {
			continue
		}
		toolchainReport := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: true})
		if err := db.SaveToolchainReport(ctx, projectID, toolchainReport); err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

func readCodexReadinessReportFile(path string) (storage.CodexRuntimeReadinessReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return storage.CodexRuntimeReadinessReport{}, err
	}
	var report storage.CodexRuntimeReadinessReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return storage.CodexRuntimeReadinessReport{}, err
	}
	if err := schemas.ValidateCodexRuntimeReadiness(string(raw)); err != nil {
		return storage.CodexRuntimeReadinessReport{}, err
	}
	if strings.TrimSpace(report.HostGOOS) == "" {
		return storage.CodexRuntimeReadinessReport{}, errors.New("codex readiness report requires host_goos")
	}
	return report, nil
}

func validateCodexReadinessEnvironments(ctx context.Context, db *storage.DB, projectID string, report storage.CodexRuntimeReadinessReport) error {
	envs, err := db.ListExecutionEnvironments(ctx, projectID)
	if err != nil {
		return err
	}
	envByID := map[string]struct{}{}
	for _, env := range envs {
		envByID[env.ID] = struct{}{}
	}
	for _, item := range report.Items {
		if _, ok := envByID[item.EnvironmentID]; !ok {
			return fmt.Errorf("codex readiness report references unknown environment: %s", item.EnvironmentID)
		}
	}
	return nil
}

func runPlatformMap(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing platform map subcommand")
		return exitValidation
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("platform map list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "platform_map_list_failed", err)
		}
		defer db.Close()
		mappings, err := db.ListPathMappings(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "platform_map_list_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"mappings": mappings}, 0)
		}
		if len(mappings) == 0 {
			fmt.Fprintln(stdout, "No path mappings.")
			return 0
		}
		for _, mapping := range mappings {
			fmt.Fprintf(stdout, "%s\t%s -> %s\t%s\t%s -> %s\n", mapping.ID, mapping.FromEnvironmentID, mapping.ToEnvironmentID, mapping.Mode, mapping.FromRoot, mapping.ToRoot)
		}
		return 0
	case "add":
		fs := flag.NewFlagSet("platform map add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		fromRoot := fs.String("from-root", "", "source environment root")
		toRoot := fs.String("to-root", "", "target environment root")
		mode := fs.String("mode", "", "same_filesystem, isolated_worktree, mirrored_clone, or unsupported")
		writeOwner := fs.String("write-owner", "", "write owner environment for same_filesystem mappings")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 2 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("platform map add requires FROM_ENV and TO_ENV"))
		}
		mappingMode := platform.MappingMode(strings.TrimSpace(*mode))
		if !platform.ValidMappingMode(mappingMode) {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_mapping_mode", fmt.Errorf("invalid mapping mode: %s", *mode))
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "path_mapping_add_failed", err)
		}
		defer db.Close()
		mapping, err := db.SavePathMapping(ctx, storage.PathMappingInput{
			ProjectID:               projectID,
			FromEnvironmentID:       fs.Arg(0),
			ToEnvironmentID:         fs.Arg(1),
			FromRoot:                *fromRoot,
			ToRoot:                  *toRoot,
			Mode:                    mappingMode,
			WriteOwnerEnvironmentID: *writeOwner,
		})
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "path_mapping_add_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, mapping, 0)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", mapping.ID, mapping.FromEnvironmentID, mapping.ToEnvironmentID, mapping.Mode)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown platform map subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runPlatformProfile(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing platform profile subcommand")
		return exitValidation
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("platform profile set", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		if fs.NArg() != 1 {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("profile mode is required"))
		}
		mode, err := parsePlatformMode(fs.Arg(0))
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_profile", err)
		}
		db, projectID, root, errCode, err := openMigratedProjectDBWithRoot(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "profile_set_failed", err)
		}
		defer db.Close()
		profile, err := db.ConfigureFakeRunProfile(ctx, projectID, mode, root)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "profile_set_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, profile, 0)
		}
		fmt.Fprintf(stdout, "Profile active: %s\n", profile.Name)
		return 0
	case "list":
		fs := flag.NewFlagSet("platform profile list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		dataRoot := fs.String("data-root", "", "orchestrator data root")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, errCode, "profile_list_failed", err)
		}
		defer db.Close()
		profiles, err := db.ListRunProfiles(ctx, projectID)
		if err != nil {
			return writeError(stdout, *jsonOut, exitStorage, "profile_list_failed", err)
		}
		if *jsonOut {
			return writeJSON(stdout, map[string]any{"profiles": profiles}, 0)
		}
		for _, profile := range profiles {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", profile.Name, profile.Mode, profile.Status, profile.PrimaryEnvironmentID)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown platform profile subcommand: %s\n", args[0])
		return exitValidation
	}
}

func parsePlatformMode(input string) (platform.PlatformMode, error) {
	switch strings.TrimSpace(input) {
	case "single-environment", "single_environment":
		return platform.PlatformModeSingleEnvironment, nil
	case "windows-primary", "windows_primary":
		return platform.PlatformModeWindowsPrimary, nil
	case "wsl-primary", "wsl_primary":
		return platform.PlatformModeWSLPrimary, nil
	case "hybrid":
		return platform.PlatformModeHybrid, nil
	default:
		return "", fmt.Errorf("unknown platform profile: %s", input)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  devos init [--project-root PATH] [--data-root PATH] [--json] CONCEPT")
	fmt.Fprintln(w, "  devos preflight [--project-root PATH] [--repair-schemas] [--json]")
	fmt.Fprintln(w, "  devos spec [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos plan [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos plan start [--project-root PATH] [--data-root PATH] [--concurrency N] [--json]")
	fmt.Fprintln(w, "  devos plan status [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos plan consolidate [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos plan checkpoint --task TASK_ID [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos artifacts [--project-root PATH] [--data-root PATH] [--type TYPE] [--include-content] [--json]")
	fmt.Fprintln(w, "  devos artifacts trusted [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos artifacts check [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos artifacts approve [--project-root PATH] [--data-root PATH] --version N [--status approved] [--notes TEXT] ARTIFACT_ID")
	fmt.Fprintln(w, "  devos artifacts revise [--project-root PATH] [--data-root PATH] --content-stdin [--json] ARTIFACT_ID")
	fmt.Fprintln(w, "  devos artifacts revise-with-codex [--project-root PATH] [--data-root PATH] --instruction-stdin [--json] ARTIFACT_ID")
	fmt.Fprintln(w, "  devos check [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos tasks materialize [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos tasks [--project-root PATH] [--data-root PATH] [--status STATUS] [--json]")
	fmt.Fprintln(w, "  devos task show [--project-root PATH] [--data-root PATH] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos task artifacts [--project-root PATH] [--data-root PATH] [--include-content] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos status [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos doctor [--project-root PATH] [--data-root PATH] [--env ENV_ID] [--save] [--json]")
	fmt.Fprintln(w, "  devos request [--project-root PATH] [--data-root PATH] [--json] TEXT")
	fmt.Fprintln(w, "  devos requests [--project-root PATH] [--data-root PATH] [--status STATUS] [--json]")
	fmt.Fprintln(w, "  devos project add --name NAME --authority windows --project-root PATH [--data-root PATH] [--registry PATH] [--json]")
	fmt.Fprintln(w, "  devos project add --name NAME --authority wsl --wsl-distro DISTRO --wsl-root PATH [--windows-display-root PATH] [--data-root PATH] [--registry PATH] [--json]")
	fmt.Fprintln(w, "  devos project list [--registry PATH] [--json]")
	fmt.Fprintln(w, "  devos project remove [--registry PATH] [--json] PROJECT_ID")
	fmt.Fprintln(w, "  devos project refresh [--registry PATH] [--json] PROJECT_ID")
	fmt.Fprintln(w, "  devos queue [--project-root PATH] [--data-root PATH] [--status STATUS] [--json]")
	fmt.Fprintln(w, "  devos work start [--project-root PATH] [--data-root PATH] [--mode sequential] [--adapter fake|real-codex] [--planning-concurrency N] [--implementation-concurrency 1] [--until inbox] [--budget DURATION] [--json]")
	fmt.Fprintln(w, "  devos work status [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos work pause [--project-root PATH] [--data-root PATH] [--json] WORKER_RUN_ID")
	fmt.Fprintln(w, "  devos work resume [--project-root PATH] [--data-root PATH] [--json] WORKER_RUN_ID")
	fmt.Fprintln(w, "  devos change request [--project-root PATH] [--data-root PATH] [--json] TEXT")
	fmt.Fprintln(w, "  devos change analyze [--project-root PATH] [--data-root PATH] [--json] CR_ID")
	fmt.Fprintln(w, "  devos change approve [--project-root PATH] [--data-root PATH] --option OPTION [--json] CR_ID")
	fmt.Fprintln(w, "  devos run [--project-root PATH] [--data-root PATH] [--adapter fake|real-codex] [--real-codex] [--dry-run] [--verify] [--verify-adapter local|fake] [--verify-env ENV_ID] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos verify [--project-root PATH] [--data-root PATH] [--adapter local|fake] [--env ENV_ID] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos bootstrap [--project-root PATH] [--data-root PATH] [--adapter fake] [--profile MODE] [--json] [CONCEPT]")
	fmt.Fprintln(w, "  devos inbox [--project-root PATH] [--data-root PATH] [--status open] [--json]")
	fmt.Fprintln(w, "  devos inbox approve [--project-root PATH] [--data-root PATH] --option OPTION [--notes TEXT] [--json] INBOX_ID")
	fmt.Fprintln(w, "  devos decisions [--project-root PATH] [--data-root PATH] [--status STATUS] [--json]")
	fmt.Fprintln(w, "  devos approve [--project-root PATH] [--data-root PATH] --option OPTION [--notes TEXT] [--remember --memory-key KEY] [--json] DECISION_ID")
	fmt.Fprintln(w, "  devos memory [--project-root PATH] [--data-root PATH] [--type TYPE] [--json]")
	fmt.Fprintln(w, "  devos dependency approval request [--project-root PATH] [--data-root PATH] --name NAME --manager npm --type production --reason TEXT [--risk medium] [--alternatives TEXT] [--files-affected PATHS] [--json]")
	fmt.Fprintln(w, "  devos dependency risk add [--project-root PATH] [--data-root PATH] --name NAME --manager npm --type production --reason TEXT --risk medium [--lockfile-changed] [--lifecycle-scripts VALUE] [--approved-scope project] [--json]")
	fmt.Fprintln(w, "  devos dependency risk list [--project-root PATH] [--data-root PATH] [--manager npm] [--type production] [--risk medium] [--json]")
	fmt.Fprintln(w, "  devos ui snapshot [--project-root PATH] [--data-root PATH] [--limit N] [--json]")
	fmt.Fprintln(w, "  devos ui dashboard [--project-root PATH] [--data-root PATH] [--limit N] [--json]")
	fmt.Fprintln(w, "  devos ui setup [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos ui setup-action [--project-root PATH] [--data-root PATH] [--json] ACTION_ID")
	fmt.Fprintln(w, "  devos serve [--project-root PATH] [--data-root PATH] [--registry PATH] [--addr 127.0.0.1:8765] [--ui] [--open] [--json]")
	fmt.Fprintln(w, "  devos start [--project-root PATH] [--data-root PATH] [--registry PATH] [--addr 127.0.0.1:8765]")
	fmt.Fprintln(w, "  devos env status [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos env set [--project-root PATH] [--data-root PATH] [--scope project] [--scope-id ID] [--env ENV_ID] --value-stdin [--json] KEY")
	fmt.Fprintln(w, "  devos review [--project-root PATH] [--data-root PATH] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos review approve [--project-root PATH] [--data-root PATH] [--notes TEXT] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos review reject [--project-root PATH] [--data-root PATH] [--notes TEXT] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge approve [--project-root PATH] [--data-root PATH] [--notes TEXT] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge [--project-root PATH] [--data-root PATH] [--dry-run] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge queue [--project-root PATH] [--data-root PATH] [--process-fake] [--simulate-conflict] [--retry-conflict ID] [--cancel-conflict ID] [--dry-run-real-git] [--process-real-git --execute --ff-only --no-push --target main] [--entry ID] [--json]")
	fmt.Fprintln(w, "  devos patch export [--project-root PATH] [--data-root PATH] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch mark-applied [--project-root PATH] [--data-root PATH] --commit SHA [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch verify-applied [--project-root PATH] [--data-root PATH] [--adapter local|fake] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch status [--project-root PATH] [--data-root PATH] [--json] [TASK_ID]")
	fmt.Fprintln(w, "  devos cleanup [--project-root PATH] [--data-root PATH] [--dry-run] [--execute] [--quarantine] [--quarantine-root PATH] [--delete] [--merged] [--applied] [--older-than AGE] [--json]")
	fmt.Fprintln(w, "  devos cleanup quarantine list [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos cleanup quarantine restore [--project-root PATH] [--data-root PATH] [--run RUN_ID] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos publish [--project-root PATH] [--data-root PATH] [--remote origin] [--branch main] [--dry-run|--execute] [--json]")
	fmt.Fprintln(w, "  devos platform detect [--project-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform profile set [--project-root PATH] [--data-root PATH] [--json] MODE")
	fmt.Fprintln(w, "  devos platform profile list [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform codex-readiness [--project-root PATH] [--data-root PATH] [--from-file PATH] [--save] [--json]")
	fmt.Fprintln(w, "  devos platform map list [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform map add [--project-root PATH] [--data-root PATH] --from-root PATH --to-root PATH --mode MODE [--write-owner ENV_ID] [--json] FROM_ENV TO_ENV")
	fmt.Fprintln(w, "  devos platform setup instructions [--project-root PATH] [--data-root PATH] [--json] INBOX_ID")
	fmt.Fprintln(w, "  devos platform setup mark-installed [--project-root PATH] [--data-root PATH] [--include-codex] [--json] INBOX_ID")
	fmt.Fprintln(w, "  devos platform setup waive [--project-root PATH] [--data-root PATH] --reason TEXT --scope SCOPE --expiry RFC3339 --allowed-effect EFFECT [--json] INBOX_ID")
	fmt.Fprintln(w, "  devos platform doctor [--project-root PATH] [--data-root PATH] [--env ENV_ID] [--include-codex] [--include-ui] [--save] [--json]")
}

func printFindings(w io.Writer, report preflight.Report) {
	for _, finding := range report.Findings {
		fmt.Fprintf(w, "[%s] %s: %s\n", finding.Severity, finding.ID, finding.Message)
		for _, detail := range finding.Details {
			fmt.Fprintf(w, "  - %s\n", detail)
		}
	}
}

func exitFromPreflight(report preflight.Report) int {
	if report.HasBlocks() {
		return exitPolicy
	}
	return 0
}

func exitFromToolchainDoctor(report toolchains.Report) int {
	if report.HasRequiredMergeFailure() {
		return exitPolicy
	}
	return 0
}

func openProjectDB(ctx context.Context, projectRoot string, dataRoot string) (*storage.DB, string, error) {
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = filepath.Join(projectRoot, "orchestrator-data")
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return nil, "", err
	}
	dbPath := filepath.Join(dataRoot, "devos.sqlite")
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		return nil, "", err
	}
	return db, dbPath, nil
}

func openMigratedProjectDB(ctx context.Context, projectRoot string, dataRoot string) (*storage.DB, string, int, error) {
	db, projectID, _, code, err := openMigratedProjectDBWithRoot(ctx, projectRoot, dataRoot)
	return db, projectID, code, err
}

func openMigratedProjectDBWithRoot(ctx context.Context, projectRoot string, dataRoot string) (*storage.DB, string, string, int, error) {
	root, err := preflight.ResolveProjectRoot(projectRoot)
	if err != nil {
		return nil, "", "", exitValidation, err
	}
	db, _, err := openProjectDB(ctx, root, dataRoot)
	if err != nil {
		return nil, "", "", exitStorage, err
	}
	migrations, err := storage.RegisteredMigrations()
	if err != nil {
		_ = db.Close()
		return nil, "", "", exitStorage, err
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		_ = db.Close()
		return nil, "", "", exitStorage, err
	}
	return db, storage.ProjectIDForRoot(root), root, 0, nil
}

func openRegistryDB(ctx context.Context, path string) (*registry.DB, int, error) {
	registryPath := strings.TrimSpace(path)
	if registryPath == "" {
		defaultPath, err := registry.DefaultPath()
		if err != nil {
			return nil, exitValidation, err
		}
		registryPath = defaultPath
	}
	regDB, err := registry.Open(ctx, registryPath)
	if err != nil {
		return nil, exitStorage, err
	}
	return regDB, 0, nil
}

func writeArtifactAndSave(ctx context.Context, db *storage.DB, projectID string, root string, relPath string, artifactType storage.ArtifactType, content []byte) (storage.ArtifactVersionRecord, error) {
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return storage.ArtifactVersionRecord{}, err
	}
	if err := os.WriteFile(absPath, content, 0o644); err != nil {
		return storage.ArtifactVersionRecord{}, err
	}
	return db.SaveArtifactVersion(ctx, storage.ArtifactVersionInput{
		ProjectID:    projectID,
		ArtifactType: artifactType,
		Path:         filepath.ToSlash(relPath),
		Content:      content,
		Status:       "proposed",
	})
}

func writeJSON(w io.Writer, v any, code int) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return exitInternal
	}
	return code
}

func writeError(w io.Writer, jsonOut bool, code int, errCode string, err error) int {
	if err == nil {
		err = errors.New("unknown error")
	}
	if jsonOut {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    errCode,
				"message": err.Error(),
			},
		})
	} else {
		fmt.Fprintf(w, "error: %s\n", err.Error())
	}
	if code == 0 {
		return exitInternal
	}
	return code
}
