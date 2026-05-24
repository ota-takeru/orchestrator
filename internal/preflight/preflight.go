package preflight

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
	"github.com/ota-takeru/orchestrator/internal/schemas"
)

type Severity string

const (
	SeverityPass  Severity = "pass"
	SeverityWarn  Severity = "warn"
	SeverityBlock Severity = "block"
)

type Finding struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Details  []string `json:"details,omitempty"`
}

type Report struct {
	ProjectRoot string                        `json:"project_root"`
	Environment platform.ExecutionEnvironment `json:"environment"`
	Findings    []Finding                     `json:"findings"`
}

func (r Report) HasBlocks() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

func ResolveProjectRoot(start string) (string, error) {
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("git repository root not found from %s", start)
		}
		abs = parent
	}
}

func Run(ctx context.Context, projectRoot string) (Report, error) {
	root, err := ResolveProjectRoot(projectRoot)
	if err != nil {
		return Report{}, err
	}
	env := platform.DetectHostEnvironment(root)
	report := Report{ProjectRoot: root, Environment: env}

	report.add(checkGitRepository(ctx, root))
	report.add(checkGitAttributes(root))
	report.add(checkGitConfig(ctx, root, "core.autocrlf"))
	report.add(checkGitConfig(ctx, root, "core.filemode"))
	report.add(checkCaseCollisions(root))
	report.add(checkSymlinkPolicy(root))
	report.add(checkGitignore(root, ".env.local", ".env.local or .env.* must be gitignored"))
	report.add(checkGitignore(root, ".devagent-worktrees/", ".devagent-worktrees/ must be gitignored"))
	report.add(checkGitignore(root, "orchestrator-data/", "orchestrator-data/ must be gitignored when placed inside the repo"))
	report.add(checkProtectedSchemaLocation(root))
	report.add(checkSchemaRegistry(root))

	if err := platform.ValidatePrimaryEnvironment([]platform.ExecutionEnvironment{env}); err != nil {
		report.add(Finding{ID: "primary_environment", Severity: SeverityBlock, Message: err.Error()})
	} else {
		report.add(Finding{ID: "primary_environment", Severity: SeverityPass, Message: "exactly one primary environment detected"})
	}
	return report, nil
}

func (r *Report) add(f Finding) {
	r.Findings = append(r.Findings, f)
}

func checkGitRepository(ctx context.Context, root string) Finding {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return Finding{ID: "git_repository", Severity: SeverityBlock, Message: "project root is not a usable git repository"}
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return Finding{ID: "git_repository", Severity: SeverityBlock, Message: "git returned an empty repository root"}
	}
	return Finding{ID: "git_repository", Severity: SeverityPass, Message: "git repository detected", Details: []string{top}}
}

func checkGitAttributes(root string) Finding {
	if _, err := os.Stat(filepath.Join(root, ".gitattributes")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Finding{ID: "gitattributes", Severity: SeverityWarn, Message: ".gitattributes is missing; line ending policy is not fixed"}
		}
		return Finding{ID: "gitattributes", Severity: SeverityWarn, Message: err.Error()}
	}
	return Finding{ID: "gitattributes", Severity: SeverityPass, Message: ".gitattributes exists"}
}

func checkGitConfig(ctx context.Context, root string, key string) Finding {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", key)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return Finding{ID: "git_config_" + strings.ReplaceAll(key, ".", "_"), Severity: SeverityWarn, Message: key + " is not set"}
	}
	value := strings.TrimSpace(string(out))
	return Finding{
		ID:       "git_config_" + strings.ReplaceAll(key, ".", "_"),
		Severity: SeverityPass,
		Message:  key + " is set",
		Details:  []string{value},
	}
}

func checkCaseCollisions(root string) Finding {
	seen := map[string]string{}
	var collisions []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if shouldSkipPreflightWalkDir(d) {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		key := strings.ToLower(filepath.ToSlash(rel))
		if first, ok := seen[key]; ok && first != filepath.ToSlash(rel) {
			collisions = append(collisions, first+" <-> "+filepath.ToSlash(rel))
			return nil
		}
		seen[key] = filepath.ToSlash(rel)
		return nil
	})
	if len(collisions) > 0 {
		return Finding{
			ID:       "case_sensitive_filename_collision",
			Severity: SeverityBlock,
			Message:  "case-sensitive filename collisions would be unsafe on Windows",
			Details:  collisions,
		}
	}
	return Finding{ID: "case_sensitive_filename_collision", Severity: SeverityPass, Message: "no case-sensitive filename collisions detected"}
}

func checkSymlinkPolicy(root string) Finding {
	var symlinks []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if shouldSkipPreflightWalkDir(d) {
			return filepath.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				symlinks = append(symlinks, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if len(symlinks) > 0 {
		return Finding{ID: "symlink_support", Severity: SeverityWarn, Message: "symlinks exist and require platform-specific support", Details: symlinks}
	}
	return Finding{ID: "symlink_support", Severity: SeverityPass, Message: "no symlinks detected"}
}

func shouldSkipPreflightWalkDir(d fs.DirEntry) bool {
	if !d.IsDir() {
		return false
	}
	switch d.Name() {
	case ".git", ".devagent", ".devagent-worktrees", "orchestrator-data", "node_modules", "dist":
		return true
	default:
		return false
	}
}

func checkGitignore(root string, pattern string, message string) Finding {
	ignored, err := gitCheckIgnore(root, pattern)
	if err == nil && ignored {
		return Finding{ID: "gitignore_" + normalizeID(pattern), Severity: SeverityPass, Message: message}
	}

	patterns, readErr := readGitignorePatterns(filepath.Join(root, ".gitignore"))
	if readErr == nil && patternCovered(patterns, pattern) {
		return Finding{ID: "gitignore_" + normalizeID(pattern), Severity: SeverityPass, Message: message}
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Finding{ID: "gitignore_" + normalizeID(pattern), Severity: SeverityWarn, Message: readErr.Error()}
	}
	return Finding{ID: "gitignore_" + normalizeID(pattern), Severity: SeverityWarn, Message: message}
}

func gitCheckIgnore(root string, pattern string) (bool, error) {
	probe := strings.TrimSuffix(pattern, "/")
	if probe == ".env.local" {
		probe = ".env.local"
	}
	cmd := exec.Command("git", "check-ignore", "--quiet", probe)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	return false, err
}

func readGitignorePatterns(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

func patternCovered(patterns []string, pattern string) bool {
	for _, p := range patterns {
		switch pattern {
		case ".env.local":
			if p == ".env.local" || p == ".env.*" || p == ".env*" {
				return true
			}
		default:
			if strings.TrimSuffix(p, "/") == strings.TrimSuffix(pattern, "/") {
				return true
			}
		}
	}
	return false
}

func checkProtectedSchemaLocation(root string) Finding {
	if _, err := os.Stat(filepath.Join(root, ".devagent", "schemas")); err == nil {
		return Finding{
			ID:       "protected_schema_location",
			Severity: SeverityPass,
			Message:  ".devagent/schemas exists and must be excluded from coding writable roots",
		}
	}
	return Finding{ID: "protected_schema_location", Severity: SeverityPass, Message: "protected schema directory is not present in the worktree"}
}

func checkSchemaRegistry(root string) Finding {
	schemaRoot := filepath.Join(root, ".devagent", "schemas")
	if _, err := os.Stat(schemaRoot); errors.Is(err, os.ErrNotExist) {
		return Finding{ID: "schema_registry", Severity: SeverityWarn, Message: "schema registry has not been installed"}
	}
	validation := schemas.ValidateInstalled(root)
	if !validation.Valid {
		return Finding{ID: "schema_registry", Severity: SeverityBlock, Message: "schema registry checksum validation failed", Details: validation.Findings}
	}
	return Finding{ID: "schema_registry", Severity: SeverityPass, Message: "schema registry checksum validation passed"}
}

func normalizeID(s string) string {
	replacer := strings.NewReplacer(".", "", "/", "", "*", "star", "-", "_")
	return replacer.Replace(s)
}
