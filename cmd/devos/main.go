package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/preflight"
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
	case "run":
		return runTaskCommand(ctx, args[1:], stdout)
	case "bootstrap":
		return runBootstrap(ctx, args[1:], stdout)
	case "platform":
		return runPlatform(ctx, args[1:], stdout, stderr)
	case "inbox":
		return runInbox(ctx, args[1:], stdout)
	case "review":
		return runReview(ctx, args[1:], stdout, stderr)
	case "merge":
		return runMerge(ctx, args[1:], stdout, stderr)
	case "patch":
		return runPatch(ctx, args[1:], stdout, stderr)
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
	artifacts := []struct {
		path    string
		typ     storage.ArtifactType
		content []byte
	}{
		{".devagent/prd.md", storage.ArtifactPRD, []byte("# PRD\n\n" + strings.TrimSpace(string(concept)) + "\n\n## Acceptance Criteria\n\n- Bootstrap workflow evidence is saved.\n")},
		{".devagent/architecture.md", storage.ArtifactArchitecture, []byte("# Architecture\n\nLocal-first Go CLI/Core with SQLite evidence store.\n")},
		{".devagent/roadmap.yaml", storage.ArtifactRoadmap, []byte("slices:\n  - id: TASK-001\n    title: Bootstrap fake workflow\n")},
		{".devagent/tasks/TASK-001.yaml", storage.ArtifactTaskYAML, []byte("id: TASK-001\ntitle: Bootstrap fake workflow\nstatus: proposed\nbase_branch: main\n")},
	}
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

func runTaskCommand(ctx context.Context, args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectRoot := fs.String("project-root", "", "project root")
	dataRoot := fs.String("data-root", "", "orchestrator data root")
	adapter := fs.String("adapter", "fake", "fake or codex")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	if *adapter != "fake" {
		return writeError(stdout, *jsonOut, exitValidation, "unsupported_adapter", errors.New("only --adapter fake is implemented"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "run_failed", err)
	}
	defer db.Close()
	result, err := db.RunFakeTask(ctx, projectID, fs.Arg(0))
	if err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "run_failed", err)
	}
	if *jsonOut {
		return writeJSON(stdout, result, 0)
	}
	fmt.Fprintf(stdout, "Run complete: %s -> %s\n", result.TaskID, result.TaskStatus)
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
	content := []byte("# PRD\n\n" + strings.TrimSpace(string(concept)) + "\n\n## Acceptance Criteria\n\n- Bootstrap workflow evidence is saved.\n")
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
	records := make([]storage.ArtifactVersionRecord, 0, 3)
	artifacts := []struct {
		path    string
		typ     storage.ArtifactType
		content []byte
	}{
		{".devagent/architecture.md", storage.ArtifactArchitecture, []byte("# Architecture\n\nLocal-first Go CLI/Core with SQLite evidence store.\n")},
		{".devagent/roadmap.yaml", storage.ArtifactRoadmap, []byte("slices:\n  - id: TASK-001\n    title: Bootstrap fake workflow\n")},
		{".devagent/tasks/TASK-001.yaml", storage.ArtifactTaskYAML, []byte("id: TASK-001\ntitle: Bootstrap fake workflow\nstatus: proposed\nbase_branch: main\n")},
	}
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

func runArtifacts(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing artifacts subcommand")
		return exitValidation
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
	default:
		fmt.Fprintf(stderr, "unknown artifacts subcommand: %s\n", args[0])
		return exitValidation
	}
}

func runReview(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing review subcommand")
		return exitValidation
	}
	switch args[0] {
	case "approve":
		return runApproveEvidence(ctx, args[1:], stdout, storage.ApprovalFinalReview)
	default:
		fmt.Fprintf(stderr, "unknown review subcommand: %s\n", args[0])
		return exitValidation
	}
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
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "merge_queue_failed", err)
	}
	defer db.Close()
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
	adapter := fs.String("adapter", "fake", "fake or codex")
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
	}
	if fs.NArg() != 1 {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", errors.New("task id is required"))
	}
	if *adapter != "fake" {
		return writeError(stdout, *jsonOut, exitValidation, "unsupported_adapter", errors.New("only --adapter fake is implemented"))
	}
	db, projectID, errCode, err := openMigratedProjectDB(ctx, *projectRoot, *dataRoot)
	if err != nil {
		return writeError(stdout, *jsonOut, errCode, "patch_verify_applied_failed", err)
	}
	defer db.Close()
	patch, err := db.VerifyAppliedPatchFake(ctx, projectID, fs.Arg(0))
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
	jsonOut := fs.Bool("json", false, "write JSON only to stdout")
	if err := fs.Parse(args); err != nil {
		return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
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
	case "doctor":
		fs := flag.NewFlagSet("platform doctor", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		projectRoot := fs.String("project-root", "", "project root")
		includeCodex := fs.Bool("include-codex", false, "include real Codex adapter preflight")
		jsonOut := fs.Bool("json", false, "write JSON only to stdout")
		if err := fs.Parse(args[1:]); err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "invalid_arguments", err)
		}
		root, err := preflight.ResolveProjectRoot(*projectRoot)
		if err != nil {
			return writeError(stdout, *jsonOut, exitValidation, "platform_doctor_failed", err)
		}
		env := platform.DetectHostEnvironment(root)
		report := toolchains.RunDoctor(ctx, env, toolchains.Options{IncludeCodex: *includeCodex})
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
	fmt.Fprintln(w, "  devos preflight [--project-root PATH] [--json]")
	fmt.Fprintln(w, "  devos spec [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos plan [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos artifacts approve [--project-root PATH] [--data-root PATH] --version N [--status approved] [--notes TEXT] ARTIFACT_ID")
	fmt.Fprintln(w, "  devos tasks materialize [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos tasks [--project-root PATH] [--data-root PATH] [--status STATUS] [--json]")
	fmt.Fprintln(w, "  devos run [--project-root PATH] [--data-root PATH] [--adapter fake] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos bootstrap [--project-root PATH] [--data-root PATH] [--adapter fake] [--profile MODE] [--json] [CONCEPT]")
	fmt.Fprintln(w, "  devos inbox [--project-root PATH] [--data-root PATH] [--status open] [--json]")
	fmt.Fprintln(w, "  devos review approve [--project-root PATH] [--data-root PATH] [--notes TEXT] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge approve [--project-root PATH] [--data-root PATH] [--notes TEXT] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge [--project-root PATH] [--data-root PATH] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos merge queue [--project-root PATH] [--data-root PATH] [--process-fake] [--simulate-conflict] [--json]")
	fmt.Fprintln(w, "  devos patch export [--project-root PATH] [--data-root PATH] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch mark-applied [--project-root PATH] [--data-root PATH] --commit SHA [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch verify-applied [--project-root PATH] [--data-root PATH] [--adapter fake] [--json] TASK_ID")
	fmt.Fprintln(w, "  devos patch status [--project-root PATH] [--data-root PATH] [--json] [TASK_ID]")
	fmt.Fprintln(w, "  devos platform detect [--project-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform profile set [--project-root PATH] [--data-root PATH] [--json] MODE")
	fmt.Fprintln(w, "  devos platform profile list [--project-root PATH] [--data-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform doctor [--project-root PATH] [--include-codex] [--json]")
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
