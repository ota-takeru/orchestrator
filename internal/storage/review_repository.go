package storage

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/schemas"
)

type TaskReviewResult struct {
	TaskID         string                       `json:"task_id"`
	TaskStatus     string                       `json:"task_status"`
	ReviewRunID    string                       `json:"review_run_id"`
	ReviewArtifact RunArtifactRecord            `json:"review_artifact"`
	SemanticDiffs  []SemanticBehaviorDiffRecord `json:"semantic_diffs"`
}

type SemanticBehaviorDiffRecord struct {
	ID             string                 `json:"id"`
	TaskID         string                 `json:"task_id"`
	RunID          string                 `json:"run_id"`
	DiffArtifactID string                 `json:"diff_artifact_id"`
	Status         string                 `json:"status"`
	Category       string                 `json:"category"`
	Summary        string                 `json:"summary"`
	Confidence     string                 `json:"confidence"`
	Evidence       []SemanticDiffEvidence `json:"evidence"`
	CreatedAt      string                 `json:"created_at"`
}

type SemanticDiffEvidence struct {
	File       string `json:"file"`
	ChangeType string `json:"change_type"`
	Source     string `json:"source"`
	Generated  bool   `json:"generated"`
}

type latestDiffArtifact struct {
	RunID          string
	BaseCommit     string
	HeadCommit     string
	DiffHash       string
	DiffArtifactID string
	Path           string
	ContentHash    string
}

func (db *DB) ReviewTask(ctx context.Context, projectID string, taskID string) (TaskReviewResult, error) {
	projectID = strings.TrimSpace(projectID)
	taskID = strings.TrimSpace(taskID)
	if projectID == "" || taskID == "" {
		return TaskReviewResult{}, fmt.Errorf("project id and task id are required")
	}
	taskStatus, err := db.taskStatus(ctx, projectID, taskID)
	if err != nil {
		return TaskReviewResult{}, err
	}
	if taskStatus != "reviewing" && taskStatus != "ready_for_human_review" {
		return TaskReviewResult{}, fmt.Errorf("task %s is not ready for review: %s", taskID, taskStatus)
	}
	diff, err := db.latestImplementationDiff(ctx, projectID, taskID)
	if err != nil {
		return TaskReviewResult{}, err
	}
	diffContent, err := os.ReadFile(filepath.Join(db.dataRoot, diff.Path))
	if err != nil {
		return TaskReviewResult{}, err
	}
	evidence := semanticEvidenceFromDiff(string(diffContent))
	records := semanticDiffRecordsFromEvidence(projectID, taskID, "", diff.DiffArtifactID, evidence)
	attemptNo, err := db.nextRunAttempt(ctx, projectID, taskID, "review")
	if err != nil {
		return TaskReviewResult{}, err
	}
	runID := "RUN-" + stableShortHash(taskID+"|review|"+time.Now().UTC().Format(time.RFC3339Nano))
	for i := range records {
		records[i].RunID = runID
		records[i].ID = semanticBehaviorDiffID(projectID, taskID, runID, records[i].Category)
	}
	semanticPayload, err := semanticValidationPayload(records)
	if err != nil {
		return TaskReviewResult{}, err
	}
	if err := schemas.ValidateSemanticBehaviorDiff(string(semanticPayload)); err != nil {
		return TaskReviewResult{}, err
	}
	reviewContent, err := json.MarshalIndent(map[string]any{
		"task_id":                taskID,
		"review_run_id":          runID,
		"source_run_id":          diff.RunID,
		"diff_artifact_id":       diff.DiffArtifactID,
		"semantic_behavior_diff": records,
	}, "", "  ")
	if err != nil {
		return TaskReviewResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return TaskReviewResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO runs(
  id, project_id, task_id, run_type, status, attempt_no, base_commit, head_commit,
  diff_hash, repair_of_run_id, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, 'review', 'succeeded', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, projectID, taskID, attemptNo, diff.BaseCommit, diff.HeadCommit, diff.DiffHash,
		diff.RunID, now, now, now, now,
	); err != nil {
		return TaskReviewResult{}, err
	}
	reviewArtifact, err := db.saveRunArtifactInTx(ctx, tx, RunArtifactInput{
		ProjectID:    projectID,
		RunID:        runID,
		ArtifactType: "review",
		ArtifactKey:  "review.json",
		Content:      reviewContent,
	}, now)
	if err != nil {
		return TaskReviewResult{}, err
	}
	for _, record := range records {
		summaryJSON, evidenceJSON, err := semanticDiffJSON(record)
		if err != nil {
			return TaskReviewResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO semantic_behavior_diffs(
  id, project_id, task_id, run_id, diff_artifact_id, status,
  summary_json, evidence_json, category, summary, confidence, created_at
) VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?, ?)`,
			record.ID, projectID, taskID, runID, diff.DiffArtifactID, summaryJSON, evidenceJSON,
			record.Category, record.Summary, record.Confidence, now,
		); err != nil {
			return TaskReviewResult{}, err
		}
	}
	nextStatus := taskStatus
	if taskStatus == "reviewing" {
		nextStatus = "ready_for_human_review"
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status = ?, updated_at = ? WHERE project_id = ? AND id = ?", nextStatus, now, projectID, taskID); err != nil {
			return TaskReviewResult{}, err
		}
	}
	if err := insertWorkflowEvent(ctx, tx, projectID, "task_review_completed", map[string]any{
		"task_id":          taskID,
		"review_run_id":    runID,
		"diff_artifact_id": diff.DiffArtifactID,
		"semantic_diffs":   len(records),
	}, now); err != nil {
		return TaskReviewResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskReviewResult{}, err
	}
	committed = true
	for i := range records {
		records[i].CreatedAt = now
		records[i].Status = "ready"
	}
	return TaskReviewResult{
		TaskID:         taskID,
		TaskStatus:     nextStatus,
		ReviewRunID:    runID,
		ReviewArtifact: reviewArtifact,
		SemanticDiffs:  records,
	}, nil
}

func (db *DB) latestImplementationDiff(ctx context.Context, projectID string, taskID string) (latestDiffArtifact, error) {
	var record latestDiffArtifact
	var headCommit, diffHash sql.NullString
	row := db.sql.QueryRowContext(ctx, `
SELECT r.id, r.base_commit, r.head_commit, r.diff_hash, ra.id, ra.path, ra.content_hash
FROM runs r
JOIN run_artifacts ra ON ra.project_id = r.project_id AND ra.run_id = r.id
WHERE r.project_id = ?
  AND r.task_id = ?
  AND r.run_type IN ('implementation', 'repair')
  AND r.status = 'succeeded'
  AND ra.artifact_type = 'diff'
ORDER BY COALESCE(r.completed_at, r.created_at) DESC, r.created_at DESC
LIMIT 1`, projectID, taskID)
	if err := row.Scan(&record.RunID, &record.BaseCommit, &headCommit, &diffHash, &record.DiffArtifactID, &record.Path, &record.ContentHash); err != nil {
		if err == sql.ErrNoRows {
			return latestDiffArtifact{}, fmt.Errorf("diff artifact not found for task %s", taskID)
		}
		return latestDiffArtifact{}, err
	}
	if headCommit.Valid {
		record.HeadCommit = headCommit.String
	}
	if diffHash.Valid {
		record.DiffHash = diffHash.String
	}
	return record, nil
}

func semanticEvidenceFromDiff(diff string) []SemanticDiffEvidence {
	scanner := bufio.NewScanner(strings.NewReader(diff))
	seen := map[string]SemanticDiffEvidence{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		path := strings.TrimPrefix(fields[3], "b/")
		if path == "/dev/null" {
			path = strings.TrimPrefix(fields[2], "a/")
		}
		if path == "" || path == "/dev/null" {
			continue
		}
		seen[path] = SemanticDiffEvidence{
			File:       path,
			ChangeType: "modified",
			Source:     "git_diff",
			Generated:  looksGeneratedPath(path),
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	evidence := make([]SemanticDiffEvidence, 0, len(files))
	for _, file := range files {
		evidence = append(evidence, seen[file])
	}
	return evidence
}

func semanticDiffRecordsFromEvidence(projectID string, taskID string, runID string, diffArtifactID string, evidence []SemanticDiffEvidence) []SemanticBehaviorDiffRecord {
	if len(evidence) == 0 {
		return []SemanticBehaviorDiffRecord{{
			ID:             semanticBehaviorDiffID(projectID, taskID, runID, "non_user_visible"),
			TaskID:         taskID,
			RunID:          runID,
			DiffArtifactID: diffArtifactID,
			Status:         "ready",
			Category:       "non_user_visible",
			Summary:        "差分artifactにファイル変更が見つかりませんでした。",
			Confidence:     "low",
			Evidence:       nil,
		}}
	}
	grouped := map[string][]SemanticDiffEvidence{}
	for _, item := range evidence {
		category := semanticCategoryForPath(item.File)
		grouped[category] = append(grouped[category], item)
	}
	categories := make([]string, 0, len(grouped))
	for category := range grouped {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	records := make([]SemanticBehaviorDiffRecord, 0, len(categories))
	for _, category := range categories {
		items := grouped[category]
		records = append(records, SemanticBehaviorDiffRecord{
			ID:             semanticBehaviorDiffID(projectID, taskID, runID, category),
			TaskID:         taskID,
			RunID:          runID,
			DiffArtifactID: diffArtifactID,
			Status:         "ready",
			Category:       category,
			Summary:        semanticSummary(category, items),
			Confidence:     semanticConfidence(items),
			Evidence:       items,
		})
	}
	return records
}

func semanticBehaviorDiffID(projectID string, taskID string, runID string, category string) string {
	return "SBD-" + stableShortHash(projectID+"|"+taskID+"|"+runID+"|"+category)
}

func semanticDiffJSON(record SemanticBehaviorDiffRecord) (string, string, error) {
	summaryJSON, err := json.Marshal(map[string]any{
		"category":   record.Category,
		"summary":    record.Summary,
		"confidence": record.Confidence,
		"files":      evidenceFiles(record.Evidence),
	})
	if err != nil {
		return "", "", err
	}
	evidenceJSON, err := json.Marshal(record.Evidence)
	if err != nil {
		return "", "", err
	}
	return string(summaryJSON), string(evidenceJSON), nil
}

func semanticValidationPayload(records []SemanticBehaviorDiffRecord) ([]byte, error) {
	items := make([]schemas.SemanticBehaviorDiffItem, 0, len(records))
	for _, record := range records {
		evidence := make([]schemas.SemanticBehaviorDiffEvidence, 0, len(record.Evidence))
		for _, item := range record.Evidence {
			evidence = append(evidence, schemas.SemanticBehaviorDiffEvidence{
				File:       item.File,
				ChangeType: item.ChangeType,
				Source:     item.Source,
				Generated:  item.Generated,
			})
		}
		items = append(items, schemas.SemanticBehaviorDiffItem{
			Category:   record.Category,
			Summary:    record.Summary,
			Confidence: record.Confidence,
			Evidence:   evidence,
		})
	}
	return json.Marshal(items)
}

func evidenceFiles(evidence []SemanticDiffEvidence) []string {
	files := make([]string, 0, len(evidence))
	for _, item := range evidence {
		files = append(files, item.File)
	}
	return files
}

func semanticCategoryForPath(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "test") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".spec.ts") || strings.HasSuffix(lower, ".test.tsx") || strings.HasSuffix(lower, ".spec.tsx"):
		return "test_change"
	case strings.HasPrefix(lower, "ui/") || strings.HasPrefix(lower, "web/") || strings.HasPrefix(lower, "frontend/") || strings.HasSuffix(lower, ".tsx") || strings.HasSuffix(lower, ".jsx") || strings.HasSuffix(lower, ".css") || strings.HasSuffix(lower, ".html"):
		return "user_visible"
	case strings.HasPrefix(lower, "internal/storage/migrations/") || strings.HasSuffix(lower, "go.mod") || strings.HasSuffix(lower, "go.sum") || strings.Contains(lower, "auth") || strings.Contains(lower, "security"):
		return "risk"
	default:
		return "non_user_visible"
	}
}

func semanticSummary(category string, evidence []SemanticDiffEvidence) string {
	count := len(evidence)
	switch category {
	case "user_visible":
		return fmt.Sprintf("ユーザー表示またはUI関連の変更が%dファイルで検出されました。", count)
	case "test_change":
		return fmt.Sprintf("テストまたは検証関連の変更が%dファイルで検出されました。", count)
	case "risk":
		return fmt.Sprintf("schema、依存、認証、またはセキュリティに関わる可能性がある変更が%dファイルで検出されました。", count)
	default:
		return fmt.Sprintf("ユーザー表示以外の実装変更が%dファイルで検出されました。", count)
	}
}

func semanticConfidence(evidence []SemanticDiffEvidence) string {
	if len(evidence) == 0 {
		return "low"
	}
	for _, item := range evidence {
		if item.Generated {
			return "medium"
		}
	}
	return "high"
}

func looksGeneratedPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "generated") ||
		strings.Contains(lower, "/dist/") ||
		strings.Contains(lower, "/build/") ||
		strings.HasSuffix(lower, ".pb.go")
}
