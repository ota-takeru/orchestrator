package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/decisions"
)

func (db *DB) SaveGateResults(ctx context.Context, projectID string, taskID *string, runID string, results []decisions.GateResult) error {
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, result := range results {
		gateID := gateResultID(runID, result.Detector, i)
		if err := insertGateResult(ctx, tx, projectID, taskID, runID, gateID, result, now); err != nil {
			return err
		}
		if itemType, ok := gateInboxItemType(result.Status); ok {
			if err := upsertGateInboxItem(ctx, tx, projectID, taskID, gateID, result, itemType, now); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func insertGateResult(ctx context.Context, tx *sql.Tx, projectID string, taskID *string, runID string, gateID string, result decisions.GateResult, now string) error {
	evidence, err := json.Marshal(result.Evidence)
	if err != nil {
		return err
	}
	var taskValue any
	if taskID != nil {
		taskValue = *taskID
	}
	var actionType any
	if result.HumanActionType != nil {
		actionType = *result.HumanActionType
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO gate_results(
  id, project_id, task_id, run_id, status, severity, detector,
  human_action_type, evidence_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gateID, projectID, taskValue, runID, result.Status, result.Severity,
		result.Detector, actionType, string(evidence), now,
	)
	return err
}

func upsertGateInboxItem(ctx context.Context, tx *sql.Tx, projectID string, taskID *string, gateID string, result decisions.GateResult, itemType string, now string) error {
	dedupeKey := strings.Join([]string{projectID, "gate_result", gateID}, ":")
	itemID := "INBOX-" + stableShortHash(dedupeKey)
	var taskValue any
	if taskID != nil {
		taskValue = *taskID
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, ?, ?, 'open', 'gate_result', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  priority = excluded.priority,
  updated_at = excluded.updated_at`,
		itemID, projectID, taskValue, itemType, gateID, dedupeKey,
		projectID+":gate_result:"+itemType+":"+result.Detector,
		gateInboxPriority(result.Status), gateInboxTitle(result.Status, result.Detector), gateInboxBody(result.Status, result.Detector), now, now,
	)
	return err
}

func gateInboxItemType(status decisions.GateStatus) (string, bool) {
	switch status {
	case decisions.GateReportOnly:
		return "report", true
	case decisions.GateHumanInput:
		return "human_input", true
	case decisions.GateHumanDecision:
		return "human_decision", true
	case decisions.GateHardBlock:
		return "hard_block", true
	default:
		return "", false
	}
}

func gateInboxPriority(status decisions.GateStatus) int {
	switch status {
	case decisions.GateHardBlock:
		return 100
	case decisions.GateHumanDecision:
		return 80
	case decisions.GateHumanInput:
		return 70
	default:
		return 20
	}
}

func gateInboxTitle(status decisions.GateStatus, detector string) string {
	switch status {
	case decisions.GateHumanInput:
		return "Input required: " + detector
	case decisions.GateHumanDecision:
		return "Decision required: " + detector
	case decisions.GateHardBlock:
		return "Hard block: " + detector
	default:
		return "Report: " + detector
	}
}

func gateInboxBody(status decisions.GateStatus, detector string) string {
	switch status {
	case decisions.GateHumanInput:
		return "Gate requires human input before the workflow can continue: " + detector
	case decisions.GateHumanDecision:
		return "Gate requires a human decision before the workflow can continue: " + detector
	case decisions.GateHardBlock:
		return "Gate blocked this workflow on policy and approval alone is not sufficient: " + detector
	default:
		return "Non-blocking gate report"
	}
}

func gateResultID(runID string, detector string, index int) string {
	return fmt.Sprintf("GATE-%s-%02d", stableShortHash(runID+"|"+detector), index+1)
}
