package storage

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TaskRecord struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

func (db *DB) MaterializeApprovedTasks(ctx context.Context, projectID string) ([]TaskRecord, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	approved, err := approvedArtifactVersions(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	required := []ArtifactType{ArtifactPRD, ArtifactArchitecture, ArtifactRoadmap, ArtifactTaskYAML}
	for _, typ := range required {
		if _, ok := approved[typ]; !ok {
			return nil, fmt.Errorf("approved %s artifact is required", typ)
		}
	}
	projectRoot, err := projectRoot(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	taskArtifact := approved[ArtifactTaskYAML]
	task, err := parseGeneratedTaskYAML(filepath.Join(projectRoot, filepath.FromSlash(taskArtifact.Path)))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks(
  id, project_id, artifact_version_id, status, title, base_branch, created_at, updated_at
) VALUES (?, ?, ?, 'ready', ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  artifact_version_id = excluded.artifact_version_id,
  status = CASE
    WHEN tasks.status = 'proposed' THEN 'ready'
    ELSE tasks.status
  END,
  title = excluded.title,
  base_branch = excluded.base_branch,
  updated_at = excluded.updated_at`,
		task.ID, projectID, taskArtifact.VersionID, task.Title, task.BaseBranch, now, now,
	); err != nil {
		return nil, err
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "tasks_materialized", map[string]any{
		"task_id":             task.ID,
		"artifact_version_id": taskArtifact.VersionID,
	}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return []TaskRecord{{ID: task.ID, Status: "ready", Title: task.Title}}, nil
}

func (db *DB) ListTasks(ctx context.Context, projectID string, status string) ([]TaskRecord, error) {
	query := "SELECT id, status, title FROM tasks WHERE project_id = ?"
	args := []any{projectID}
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY id"
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []TaskRecord
	for rows.Next() {
		var task TaskRecord
		if err := rows.Scan(&task.ID, &task.Status, &task.Title); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

type approvedArtifact struct {
	ArtifactID string
	VersionID  string
	Path       string
	Status     string
}

func approvedArtifactVersions(ctx context.Context, tx *sql.Tx, projectID string) (map[ArtifactType]approvedArtifact, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT a.artifact_type, a.id, av.id, av.path, av.status
FROM artifacts a
JOIN artifact_versions av ON av.id = a.approved_version_id
WHERE a.project_id = ?
  AND av.status IN ('approved', 'approved_with_notes')`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[ArtifactType]approvedArtifact{}
	for rows.Next() {
		var typ ArtifactType
		var artifact approvedArtifact
		if err := rows.Scan(&typ, &artifact.ArtifactID, &artifact.VersionID, &artifact.Path, &artifact.Status); err != nil {
			return nil, err
		}
		out[typ] = artifact
	}
	return out, rows.Err()
}

func projectRoot(ctx context.Context, tx *sql.Tx, projectID string) (string, error) {
	var root string
	if err := tx.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("project not found: %s", projectID)
		}
		return "", err
	}
	return root, nil
}

type parsedTaskYAML struct {
	ID         string
	Title      string
	BaseBranch string
}

func parseGeneratedTaskYAML(path string) (parsedTaskYAML, error) {
	file, err := os.Open(path)
	if err != nil {
		return parsedTaskYAML{}, err
	}
	defer file.Close()
	task := parsedTaskYAML{BaseBranch: "main"}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.TrimSpace(key) {
		case "id":
			task.ID = value
		case "title":
			task.Title = value
		case "base_branch":
			task.BaseBranch = value
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedTaskYAML{}, err
	}
	if task.ID == "" || task.Title == "" {
		return parsedTaskYAML{}, fmt.Errorf("task yaml requires id and title")
	}
	return task, nil
}
