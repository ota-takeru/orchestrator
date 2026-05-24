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
	SchemaKindTaskYAML          SchemaKind = "task_yaml"
	SchemaKindCodexFinalMessage SchemaKind = "codex_final_message"
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
