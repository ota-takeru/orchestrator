package artifactgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/storage"
)

type Artifact struct {
	Path    string
	Type    storage.ArtifactType
	Content []byte
}

type VerificationCommand struct {
	ID               string
	WorkingDir       string
	Argv             []string
	RequiredForMerge bool
}

func BuildInitialArtifacts(root string, concept string, includePRD bool) []Artifact {
	commands := DetectVerificationCommands(root)
	artifacts := make([]Artifact, 0, 4)
	if includePRD {
		artifacts = append(artifacts, Artifact{
			Path:    ".devagent/prd.md",
			Type:    storage.ArtifactPRD,
			Content: BuildPRDArtifact(concept, commands),
		})
	}
	artifacts = append(artifacts, BuildPlanArtifactsWithCommands(concept, commands)...)
	return artifacts
}

func BuildPlanArtifacts(root string, concept string) []Artifact {
	return BuildPlanArtifactsWithCommands(concept, DetectVerificationCommands(root))
}

func BuildPlanArtifactsWithCommands(concept string, commands []VerificationCommand) []Artifact {
	return []Artifact{
		{Path: ".devagent/architecture.md", Type: storage.ArtifactArchitecture, Content: buildArchitectureArtifact(concept, commands)},
		{Path: ".devagent/roadmap.yaml", Type: storage.ArtifactRoadmap, Content: buildRoadmapArtifact(concept, commands)},
		{Path: ".devagent/tasks/TASK-001.yaml", Type: storage.ArtifactTaskYAML, Content: buildTaskYAMLArtifact(concept, commands)},
	}
}

func BuildPRDArtifact(concept string, commands []VerificationCommand) []byte {
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

func buildArchitectureArtifact(concept string, commands []VerificationCommand) []byte {
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
	if len(commands) == 0 {
		b.WriteString("- Required verification is not configured yet; setup must block real merge until commands are added.\n")
	}
	return []byte(b.String())
}

func buildRoadmapArtifact(concept string, commands []VerificationCommand) []byte {
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
	if len(commands) == 0 {
		b.WriteString("        - add-required-verification\n")
	}
	return []byte(b.String())
}

func buildTaskYAMLArtifact(concept string, commands []VerificationCommand) []byte {
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
	if len(commands) == 0 {
		b.WriteString("  - id: add-required-verification\n")
		b.WriteString("    environment: primary\n")
		b.WriteString("    runner: auto\n")
		b.WriteString("    required_for_merge: true\n")
		b.WriteString("    working_dir: project_root\n")
		b.WriteString("    command:\n")
		b.WriteString("      argv: [\"sh\", \"-c\", \"echo 'configure required verification' && exit 1\"]\n")
		b.WriteString("    network: false\n")
		return []byte(b.String())
	}
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

func DetectVerificationCommands(root string) []VerificationCommand {
	commands := []VerificationCommand{}
	if fileExists(filepath.Join(root, "go.mod")) {
		commands = append(commands, VerificationCommand{ID: "go-test", WorkingDir: "project_root", Argv: []string{"go", "test", "./..."}, RequiredForMerge: true})
	}
	if fileExists(filepath.Join(root, "ui", "package.json")) {
		commands = append(commands,
			VerificationCommand{ID: "ui-test", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "test"}, RequiredForMerge: true},
			VerificationCommand{ID: "ui-lint", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "lint"}, RequiredForMerge: true},
			VerificationCommand{ID: "ui-build", WorkingDir: "project_root", Argv: []string{"corepack", "pnpm", "--dir", "ui", "build"}, RequiredForMerge: true},
		)
	}
	return commands
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
