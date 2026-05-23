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
	case "platform":
		return runPlatform(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return exitValidation
	}
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

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  devos init [--project-root PATH] [--data-root PATH] [--json] CONCEPT")
	fmt.Fprintln(w, "  devos preflight [--project-root PATH] [--json]")
	fmt.Fprintln(w, "  devos platform detect [--project-root PATH] [--json]")
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
