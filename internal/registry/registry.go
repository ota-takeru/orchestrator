package registry

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/storage"
	_ "modernc.org/sqlite"
)

type AuthorityRuntime string

const (
	AuthorityWindows AuthorityRuntime = "windows"
	AuthorityWSL     AuthorityRuntime = "wsl"
)

type ProjectStatus string

const (
	StatusActive   ProjectStatus = "active"
	StatusMissing  ProjectStatus = "missing"
	StatusInvalid  ProjectStatus = "invalid"
	StatusDisabled ProjectStatus = "disabled"
)

type RegisteredProject struct {
	ID                 string           `json:"id"`
	DisplayName        string           `json:"display_name"`
	AuthorityRuntime   AuthorityRuntime `json:"authority_runtime"`
	PrimaryEnvironment string           `json:"primary_environment_id"`
	ProjectRoot        string           `json:"project_root"`
	DataRoot           string           `json:"data_root,omitempty"`
	WindowsDisplayRoot string           `json:"windows_display_root,omitempty"`
	WSLDistro          string           `json:"wsl_distro,omitempty"`
	WSLProjectRoot     string           `json:"wsl_project_root,omitempty"`
	Status             ProjectStatus    `json:"status"`
	LastSeenAt         string           `json:"last_seen_at,omitempty"`
	CreatedAt          string           `json:"created_at"`
	UpdatedAt          string           `json:"updated_at"`
}

type AddProjectInput struct {
	DisplayName        string
	AuthorityRuntime   AuthorityRuntime
	ProjectRoot        string
	DataRoot           string
	WindowsDisplayRoot string
	WSLDistro          string
	WSLProjectRoot     string
}

type DB struct {
	sql *sql.DB
}

func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is required for default Windows registry path")
		}
		return filepath.Join(base, "DevOS", "registry.sqlite"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "devos", "registry.sqlite"), nil
}

func Open(ctx context.Context, path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	handle, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	db := &DB{sql: handle}
	if err := db.configure(ctx); err != nil {
		_ = handle.Close()
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) SQL() *sql.DB {
	return db.sql
}

func (db *DB) configure(ctx context.Context) error {
	for _, pragma := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
		if _, err := db.sql.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) Migrate(ctx context.Context) error {
	_, err := db.sql.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS registered_projects (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  authority_runtime TEXT NOT NULL CHECK (authority_runtime IN ('windows', 'wsl')),
  primary_environment_id TEXT NOT NULL,
  project_root TEXT NOT NULL,
  data_root TEXT,
  windows_display_root TEXT,
  wsl_distro TEXT,
  wsl_project_root TEXT,
  status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'invalid', 'disabled')),
  last_seen_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_registered_projects_status ON registered_projects(status);
`)
	return err
}

func (db *DB) AddProject(ctx context.Context, input AddProjectInput) (RegisteredProject, error) {
	project, err := NormalizeProject(input)
	if err != nil {
		return RegisteredProject{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	project.CreatedAt = now
	project.UpdatedAt = now
	_, err = db.sql.ExecContext(ctx, `
INSERT INTO registered_projects(
  id, display_name, authority_runtime, primary_environment_id, project_root,
  data_root, windows_display_root, wsl_distro, wsl_project_root, status,
  last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  display_name = excluded.display_name,
  authority_runtime = excluded.authority_runtime,
  primary_environment_id = excluded.primary_environment_id,
  project_root = excluded.project_root,
  data_root = excluded.data_root,
  windows_display_root = excluded.windows_display_root,
  wsl_distro = excluded.wsl_distro,
  wsl_project_root = excluded.wsl_project_root,
  status = excluded.status,
  updated_at = excluded.updated_at`,
		project.ID, project.DisplayName, project.AuthorityRuntime, project.PrimaryEnvironment,
		project.ProjectRoot, nullable(project.DataRoot), nullable(project.WindowsDisplayRoot),
		nullable(project.WSLDistro), nullable(project.WSLProjectRoot), project.Status,
		nullable(project.LastSeenAt), project.CreatedAt, project.UpdatedAt,
	)
	if err != nil {
		return RegisteredProject{}, err
	}
	return db.GetProject(ctx, project.ID)
}

func NormalizeProject(input AddProjectInput) (RegisteredProject, error) {
	name := strings.TrimSpace(input.DisplayName)
	if name == "" {
		return RegisteredProject{}, fmt.Errorf("project name is required")
	}
	switch input.AuthorityRuntime {
	case AuthorityWindows:
		root := strings.TrimSpace(input.ProjectRoot)
		if root == "" {
			return RegisteredProject{}, fmt.Errorf("project-root is required for Windows authority")
		}
		return RegisteredProject{
			ID:                 storage.ProjectIDForRoot(root),
			DisplayName:        name,
			AuthorityRuntime:   AuthorityWindows,
			PrimaryEnvironment: "windows-main",
			ProjectRoot:        root,
			DataRoot:           strings.TrimSpace(input.DataRoot),
			WindowsDisplayRoot: strings.TrimSpace(input.WindowsDisplayRoot),
			Status:             StatusActive,
		}, nil
	case AuthorityWSL:
		distro := strings.TrimSpace(input.WSLDistro)
		wslRoot := strings.TrimSpace(input.WSLProjectRoot)
		if distro == "" || wslRoot == "" {
			return RegisteredProject{}, fmt.Errorf("wsl-distro and wsl-root are required for WSL authority")
		}
		projectID := WSLProjectID(distro, wslRoot)
		displayRoot := strings.TrimSpace(input.WindowsDisplayRoot)
		return RegisteredProject{
			ID:                 projectID,
			DisplayName:        name,
			AuthorityRuntime:   AuthorityWSL,
			PrimaryEnvironment: "wsl-main",
			ProjectRoot:        wslRoot,
			DataRoot:           strings.TrimSpace(input.DataRoot),
			WindowsDisplayRoot: displayRoot,
			WSLDistro:          distro,
			WSLProjectRoot:     wslRoot,
			Status:             StatusActive,
		}, nil
	default:
		return RegisteredProject{}, fmt.Errorf("invalid authority_runtime: %s", input.AuthorityRuntime)
	}
}

func WSLProjectID(distro string, root string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(distro) + ":" + filepath.ToSlash(filepath.Clean(strings.TrimSpace(root)))))
	return "PROJECT-" + strings.ToUpper(hex.EncodeToString(sum[:])[:12])
}

func (db *DB) ListProjects(ctx context.Context) ([]RegisteredProject, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, display_name, authority_runtime, primary_environment_id, project_root,
       COALESCE(data_root, ''), COALESCE(windows_display_root, ''), COALESCE(wsl_distro, ''),
       COALESCE(wsl_project_root, ''), status, COALESCE(last_seen_at, ''), created_at, updated_at
FROM registered_projects
ORDER BY display_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]RegisteredProject, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (db *DB) GetProject(ctx context.Context, id string) (RegisteredProject, error) {
	row := db.sql.QueryRowContext(ctx, `
SELECT id, display_name, authority_runtime, primary_environment_id, project_root,
       COALESCE(data_root, ''), COALESCE(windows_display_root, ''), COALESCE(wsl_distro, ''),
       COALESCE(wsl_project_root, ''), status, COALESCE(last_seen_at, ''), created_at, updated_at
FROM registered_projects
WHERE id = ?`, id)
	project, err := scanProject(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return RegisteredProject{}, ErrNotFound
		}
		return RegisteredProject{}, err
	}
	return project, nil
}

func (db *DB) RemoveProject(ctx context.Context, id string) error {
	result, err := db.sql.ExecContext(ctx, "DELETE FROM registered_projects WHERE id = ?", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) UpdateProjectStatus(ctx context.Context, id string, status ProjectStatus, lastSeen bool) (RegisteredProject, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lastSeenAt := any(nil)
	if lastSeen {
		lastSeenAt = now
	}
	result, err := db.sql.ExecContext(ctx, `
UPDATE registered_projects
SET status = ?, last_seen_at = COALESCE(?, last_seen_at), updated_at = ?
WHERE id = ?`, status, lastSeenAt, now, id)
	if err != nil {
		return RegisteredProject{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return RegisteredProject{}, err
	}
	if affected == 0 {
		return RegisteredProject{}, ErrNotFound
	}
	return db.GetProject(ctx, id)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (RegisteredProject, error) {
	var project RegisteredProject
	err := row.Scan(
		&project.ID,
		&project.DisplayName,
		&project.AuthorityRuntime,
		&project.PrimaryEnvironment,
		&project.ProjectRoot,
		&project.DataRoot,
		&project.WindowsDisplayRoot,
		&project.WSLDistro,
		&project.WSLProjectRoot,
		&project.Status,
		&project.LastSeenAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	return project, err
}

var ErrNotFound = fmt.Errorf("project not found")

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
