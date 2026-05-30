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
	if len(commands) == 0 {
		commands = DefaultSmokeVerificationCommands()
	}
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
	fmt.Fprintf(&b, "## Goal\n\nDeliver a functional first version of: %s.\n\n", title)
	b.WriteString("## Users\n\n- Primary user: the person or team described by the product concept.\n")
	b.WriteString("- Reviewer: the project owner who confirms the generated scope before implementation.\n\n")
	b.WriteString("## Core Workflow\n\n")
	b.WriteString("1. Open the application and understand the current state quickly.\n")
	b.WriteString("2. Complete the primary user action described by the concept.\n")
	b.WriteString("3. See clear feedback for success, failure, and next steps.\n")
	b.WriteString("4. Return later without losing important progress when persistence is part of the concept.\n\n")
	b.WriteString("## Acceptance Criteria\n\n")
	b.WriteString("- The application presents the product concept as a usable first workflow, not a placeholder page.\n")
	b.WriteString("- The primary user can complete the main action without needing technical instructions.\n")
	b.WriteString("- Important states such as empty, loading, success, and error are visible and understandable.\n")
	b.WriteString("- Data handling follows the concept and does not assume external services, accounts, or integrations unless requested.\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "- Verification `%s` passes: `%s`.\n", command.ID, strings.Join(command.Argv, " "))
	}
	return []byte(b.String())
}

func buildArchitectureArtifact(concept string, commands []VerificationCommand) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Architecture\n\n## Scope\n\n%s\n\n", markdownParagraph(concept))
	b.WriteString("## Technical Direction\n\n")
	b.WriteString("- Do not assume a specific framework, language, database, backend, or deployment target unless the concept or existing project files require it.\n")
	b.WriteString("- Prefer the smallest structure that makes the requested workflow complete, understandable, and easy to change.\n")
	b.WriteString("- Keep user-facing behavior, state, and data model choices directly tied to the product concept.\n")
	b.WriteString("- Introduce persistence, networking, background work, or configuration only when needed for the concept.\n\n")
	b.WriteString("## Boundaries\n\n")
	b.WriteString("- Generated implementation should stay focused on the requested application behavior.\n")
	b.WriteString("- Avoid unrelated project changes, generated filler, and assumptions about future features.\n")
	b.WriteString("- Configuration and secrets should not be committed to source control.\n\n")
	b.WriteString("## Verification Plan\n\n")
	for _, command := range commands {
		fmt.Fprintf(&b, "- `%s`: `%s` from `%s`, required_for_merge=%t.\n", command.ID, strings.Join(command.Argv, " "), command.WorkingDir, command.RequiredForMerge)
	}
	if len(commands) == 0 {
		b.WriteString("- Required smoke verification confirms implementation files were generated.\n")
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
	b.WriteString("  - The first usable workflow from the concept is implemented end to end.\n")
	b.WriteString("  - The interface exposes clear empty, loading, success, and error states where relevant.\n")
	b.WriteString("  - The implementation avoids unrelated features and technology assumptions not present in the concept.\n")
	b.WriteString("  - Required verification commands pass before review.\n")
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

func DefaultSmokeVerificationCommands() []VerificationCommand {
	return []VerificationCommand{
		{
			ID:               "implementation-files-present",
			WorkingDir:       "project_root",
			Argv:             []string{"node", "-e", "const fs=require('fs'); const entries=fs.readdirSync('.', {withFileTypes:true}).filter((entry)=>!entry.name.startsWith('.') && !(entry.isDirectory() && entry.name.endsWith('-data'))).map((entry)=>entry.name); if(entries.length===0){console.error('no implementation files generated'); process.exit(1)} console.log(entries.join('\\n'));"},
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
		return "Build the initial application workflow."
	}
	lines := strings.Split(trimmed, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return "Build the initial application workflow."
}

func markdownParagraph(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Build the initial application workflow."
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
