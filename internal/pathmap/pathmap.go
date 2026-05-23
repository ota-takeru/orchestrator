package pathmap

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

type Purpose string

const (
	PurposeRead        Purpose = "read"
	PurposeWrite       Purpose = "write"
	PurposeWorktree    Purpose = "worktree"
	PurposeRunArtifact Purpose = "run_artifact"
)

type Environment struct {
	ID          string
	OSFamily    platform.OSFamily
	AllowedRoot string
}

type Mapping struct {
	FromEnvironmentID       string
	ToEnvironmentID         string
	FromRoot                string
	ToRoot                  string
	Mode                    platform.MappingMode
	WriteOwnerEnvironmentID string
}

type Service struct {
	environments map[string]Environment
	mappings     []Mapping
}

func NewService(envs []Environment, mappings []Mapping) (*Service, error) {
	byID := make(map[string]Environment, len(envs))
	for _, env := range envs {
		if env.ID == "" {
			return nil, fmt.Errorf("environment id is required")
		}
		if !platform.ValidOSFamily(env.OSFamily) {
			return nil, fmt.Errorf("invalid os_family for %s: %s", env.ID, env.OSFamily)
		}
		if env.AllowedRoot == "" {
			return nil, fmt.Errorf("allowed root is required for %s", env.ID)
		}
		byID[env.ID] = env
	}
	for _, mapping := range mappings {
		if !platform.ValidMappingMode(mapping.Mode) {
			return nil, fmt.Errorf("invalid mapping mode: %s", mapping.Mode)
		}
		if _, ok := byID[mapping.FromEnvironmentID]; !ok {
			return nil, fmt.Errorf("unknown from environment: %s", mapping.FromEnvironmentID)
		}
		if _, ok := byID[mapping.ToEnvironmentID]; !ok {
			return nil, fmt.Errorf("unknown to environment: %s", mapping.ToEnvironmentID)
		}
		if mapping.Mode == platform.MappingSameFilesystem && mapping.WriteOwnerEnvironmentID == "" {
			return nil, fmt.Errorf("same_filesystem mapping requires write owner")
		}
	}
	return &Service{environments: byID, mappings: mappings}, nil
}

func (s *Service) ToEnvironmentPath(ctx context.Context, fromEnvID string, toEnvID string, inputPath string) (string, error) {
	_ = ctx
	fromEnv, ok := s.environments[fromEnvID]
	if !ok {
		return "", fmt.Errorf("unknown from environment: %s", fromEnvID)
	}
	toEnv, ok := s.environments[toEnvID]
	if !ok {
		return "", fmt.Errorf("unknown to environment: %s", toEnvID)
	}
	if err := ValidatePath(fromEnv, inputPath, PurposeRead); err != nil {
		return "", err
	}
	for _, mapping := range s.mappings {
		if mapping.FromEnvironmentID != fromEnvID || mapping.ToEnvironmentID != toEnvID {
			continue
		}
		if mapping.Mode == platform.MappingUnsupported {
			return "", fmt.Errorf("path mapping is unsupported: %s -> %s", fromEnvID, toEnvID)
		}
		rel, ok := relativeWithin(fromEnv.OSFamily, mapping.FromRoot, inputPath)
		if !ok {
			continue
		}
		mapped := joinForOS(toEnv.OSFamily, mapping.ToRoot, rel)
		if err := ValidatePath(toEnv, mapped, PurposeRead); err != nil {
			return "", err
		}
		return mapped, nil
	}
	return "", fmt.Errorf("path mapping not found for %s -> %s", fromEnvID, toEnvID)
}

func (s *Service) ValidatePathInEnvironment(ctx context.Context, envID string, inputPath string, purpose Purpose) error {
	_ = ctx
	env, ok := s.environments[envID]
	if !ok {
		return fmt.Errorf("unknown environment: %s", envID)
	}
	return ValidatePath(env, inputPath, purpose)
}

func ValidatePath(env Environment, inputPath string, purpose Purpose) error {
	if strings.ContainsRune(inputPath, '\x00') {
		return fmt.Errorf("path contains NUL byte")
	}
	if inputPath == "" {
		return fmt.Errorf("path is empty")
	}
	switch env.OSFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		if err := validateWindowsPath(inputPath); err != nil {
			return err
		}
	case platform.OSFamilyWSL, platform.OSFamilyLinux, platform.OSFamilyMacOS, platform.OSFamilyRemoteLinux:
		if err := validatePOSIXPath(inputPath); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported os family: %s", env.OSFamily)
	}
	if _, ok := relativeWithin(env.OSFamily, env.AllowedRoot, inputPath); !ok {
		return fmt.Errorf("path is outside allowed root for %s", env.ID)
	}
	if purpose == PurposeWrite && env.AllowedRoot == "/" {
		return fmt.Errorf("refusing broad root write validation for %s", env.ID)
	}
	return nil
}

func validateWindowsPath(p string) error {
	if strings.HasPrefix(p, `\\`) {
		return fmt.Errorf("UNC paths are not allowed in initial path validator")
	}
	matched, _ := regexp.MatchString(`^[A-Za-z]:\\`, p)
	if !matched {
		return fmt.Errorf("windows path must be absolute with drive letter")
	}
	if strings.ContainsAny(p, "\n\r\t") {
		return fmt.Errorf("windows path contains control character")
	}
	parts := strings.Split(strings.TrimPrefix(p[3:], `\`), `\`)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe windows path segment")
		}
		if isReservedWindowsName(part) {
			return fmt.Errorf("reserved windows path segment: %s", part)
		}
	}
	return nil
}

func validatePOSIXPath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("posix path must be absolute")
	}
	if strings.ContainsAny(p, "\n\r") {
		return fmt.Errorf("posix path contains control character")
	}
	clean := path.Clean(p)
	if clean != p {
		return fmt.Errorf("posix path must be normalized")
	}
	for _, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if part == ".." {
			return fmt.Errorf("unsafe posix path segment")
		}
	}
	return nil
}

func relativeWithin(osFamily platform.OSFamily, root string, p string) (string, bool) {
	switch osFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		rootNorm := normalizeWindows(root)
		pathNorm := normalizeWindows(p)
		if pathNorm == rootNorm {
			return "", true
		}
		prefix := rootNorm
		if !strings.HasSuffix(prefix, `\`) {
			prefix += `\`
		}
		if !strings.HasPrefix(pathNorm, prefix) {
			return "", false
		}
		return strings.TrimPrefix(pathNorm, prefix), true
	default:
		rootClean := path.Clean(filepath.ToSlash(root))
		pathClean := path.Clean(filepath.ToSlash(p))
		if pathClean == rootClean {
			return "", true
		}
		prefix := strings.TrimSuffix(rootClean, "/") + "/"
		if !strings.HasPrefix(pathClean, prefix) {
			return "", false
		}
		return strings.TrimPrefix(pathClean, prefix), true
	}
}

func joinForOS(osFamily platform.OSFamily, root string, rel string) string {
	if rel == "" {
		return root
	}
	switch osFamily {
	case platform.OSFamilyWindows, platform.OSFamilyRemoteWindows:
		return strings.TrimRight(root, `\`) + `\` + strings.ReplaceAll(rel, "/", `\`)
	default:
		return strings.TrimRight(root, "/") + "/" + strings.ReplaceAll(rel, `\`, "/")
	}
}

func normalizeWindows(p string) string {
	return strings.ToLower(strings.ReplaceAll(p, "/", `\`))
}

func isReservedWindowsName(part string) bool {
	name := strings.ToUpper(strings.Split(part, ".")[0])
	switch name {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}
