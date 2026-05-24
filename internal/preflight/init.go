package preflight

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/schemas"
)

type InitResult struct {
	ProjectRoot     string   `json:"project_root"`
	ConceptPath     string   `json:"concept_path"`
	PolicyPath      string   `json:"policy_path"`
	PreflightReport Report   `json:"preflight_report"`
	CreatedPaths    []string `json:"created_paths"`
}

func InitProject(ctx context.Context, projectRoot string, concept string) (InitResult, error) {
	if strings.TrimSpace(concept) == "" {
		return InitResult{}, fmt.Errorf("concept is required")
	}
	root, err := ResolveProjectRoot(projectRoot)
	if err != nil {
		return InitResult{}, err
	}
	report, err := Run(ctx, root)
	if err != nil {
		return InitResult{}, err
	}

	devagentRoot := filepath.Join(root, ".devagent")
	policyDir := filepath.Join(devagentRoot, "policies")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		return InitResult{}, err
	}

	conceptPath := filepath.Join(devagentRoot, "concept.md")
	policyPath := filepath.Join(policyDir, "project-policy.yaml")
	var created []string

	if _, err := os.Stat(conceptPath); errorsIsNotExist(err) {
		if err := os.WriteFile(conceptPath, []byte(formatConcept(concept)), 0o644); err != nil {
			return InitResult{}, err
		}
		created = append(created, conceptPath)
	} else if err != nil {
		return InitResult{}, err
	}

	if _, err := os.Stat(policyPath); errorsIsNotExist(err) {
		if err := os.WriteFile(policyPath, []byte(defaultPolicy()), 0o644); err != nil {
			return InitResult{}, err
		}
		created = append(created, policyPath)
	} else if err != nil {
		return InitResult{}, err
	}
	schemaInstall, err := schemas.Install(root)
	if err != nil {
		return InitResult{}, err
	}
	created = append(created, schemaInstall.CreatedPaths...)
	created = append(created, schemaInstall.UpdatedPaths...)
	report, err = Run(ctx, root)
	if err != nil {
		return InitResult{}, err
	}

	return InitResult{
		ProjectRoot:     root,
		ConceptPath:     conceptPath,
		PolicyPath:      policyPath,
		PreflightReport: report,
		CreatedPaths:    created,
	}, nil
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func formatConcept(concept string) string {
	return fmt.Sprintf("# Concept\n\n%s\n\nCreated: %s\n", strings.TrimSpace(concept), time.Now().UTC().Format(time.RFC3339))
}

func defaultPolicy() string {
	return `# Project Policy
retry_budget:
  max_implementation_attempts: 3
  max_repair_attempts_per_task: 2
  max_rebase_attempts: 2
platform:
  require_primary_environment: true
  forbid_cross_environment_same_worktree_writes: true
`
}
