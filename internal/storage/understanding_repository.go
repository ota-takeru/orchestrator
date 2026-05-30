package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ota-takeru/orchestrator/internal/artifactgen"
	"github.com/ota-takeru/orchestrator/internal/planning"
	"github.com/ota-takeru/orchestrator/internal/statemachine"
)

type IntentItemRecord struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	SourceType      string  `json:"source_type"`
	SourceID        string  `json:"source_id,omitempty"`
	RawText         string  `json:"raw_text"`
	NormalizedTitle string  `json:"normalized_title"`
	Status          string  `json:"status"`
	RiskLevel       string  `json:"risk_level"`
	Confidence      float64 `json:"confidence"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type UnderstandingSnapshotRecord struct {
	ID                string                   `json:"id"`
	ProjectID         string                   `json:"project_id"`
	IntentItemID      string                   `json:"intent_item_id"`
	ArtifactSnapshot  map[string]any           `json:"artifact_snapshot"`
	InterpretedGoal   []string                 `json:"interpreted_goal"`
	UserValue         []string                 `json:"user_value"`
	NonGoals          []string                 `json:"non_goals"`
	Assumptions       []planning.Assumption    `json:"assumptions"`
	OpenQuestions     []planning.OpenQuestion  `json:"open_questions"`
	AffectedContext   planning.AffectedContext `json:"affected_context"`
	Risk              planning.RiskAssessment  `json:"risk"`
	Confidence        float64                  `json:"confidence"`
	RecommendedGoMode string                   `json:"recommended_go_mode"`
	Status            string                   `json:"status"`
	CreatedAt         string                   `json:"created_at"`
	UpdatedAt         string                   `json:"updated_at"`
}

type ProposalBatchRecord struct {
	ID                      string         `json:"id"`
	ProjectID               string         `json:"project_id"`
	IntentItemIDs           []string       `json:"intent_item_ids"`
	UnderstandingSnapshotID string         `json:"understanding_snapshot_id"`
	Status                  string         `json:"status"`
	RecommendedOption       string         `json:"recommended_option"`
	Summary                 map[string]any `json:"summary"`
	CreatedAt               string         `json:"created_at"`
	UpdatedAt               string         `json:"updated_at"`
	ResolvedAt              string         `json:"resolved_at,omitempty"`
}

type ProposalDeltaRecord struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"project_id"`
	ProposalBatchID  string         `json:"proposal_batch_id"`
	TargetType       string         `json:"target_type"`
	TargetID         string         `json:"target_id,omitempty"`
	Delta            map[string]any `json:"delta"`
	RenderedMarkdown string         `json:"rendered_markdown"`
	RiskLevel        string         `json:"risk_level"`
	CreatedAt        string         `json:"created_at"`
}

type ScopeSummary struct {
	Included []string `json:"included"`
	Excluded []string `json:"excluded"`
}

type ApprovalRecommendation struct {
	Option string `json:"option"`
	Reason string `json:"reason"`
}

type ApprovalPacketSummary struct {
	OneLiner          string                   `json:"one_liner"`
	UserValue         []string                 `json:"user_value"`
	ExistingAlignment planning.AffectedContext `json:"existing_alignment"`
	ProposedScope     ScopeSummary             `json:"proposed_scope"`
	Assumptions       []planning.Assumption    `json:"assumptions"`
	OpenQuestions     []planning.OpenQuestion  `json:"open_questions"`
	Recommendation    ApprovalRecommendation   `json:"recommendation"`
	Risk              planning.RiskAssessment  `json:"risk"`
	TaskGroupID       string                   `json:"task_group_id,omitempty"`
	TaskID            string                   `json:"task_id,omitempty"`
	NextAction        string                   `json:"next_action"`
}

type ApprovalPacketRecord struct {
	ID                      string                `json:"id"`
	ProjectID               string                `json:"project_id"`
	SourceType              string                `json:"source_type"`
	SourceID                string                `json:"source_id,omitempty"`
	UnderstandingSnapshotID string                `json:"understanding_snapshot_id"`
	ProposalBatchID         string                `json:"proposal_batch_id"`
	Title                   string                `json:"title"`
	Status                  string                `json:"status"`
	Summary                 ApprovalPacketSummary `json:"summary"`
	Options                 []DecisionOption      `json:"options"`
	RecommendedOption       string                `json:"recommended_option"`
	RiskLevel               string                `json:"risk_level"`
	IntentRawText           string                `json:"intent_raw_text,omitempty"`
	IntentTitle             string                `json:"intent_title,omitempty"`
	CreatedAt               string                `json:"created_at"`
	UpdatedAt               string                `json:"updated_at"`
	ResolvedAt              string                `json:"resolved_at,omitempty"`
}

type UnderstandingIntakeResult struct {
	IntentItem            IntentItemRecord            `json:"intent_item"`
	UnderstandingSnapshot UnderstandingSnapshotRecord `json:"understanding_snapshot"`
	ProposalBatch         ProposalBatchRecord         `json:"proposal_batch"`
	ProposalDeltas        []ProposalDeltaRecord       `json:"proposal_deltas"`
	ApprovalPacket        ApprovalPacketRecord        `json:"approval_packet"`
}

type ApprovalPacketApprovalInput struct {
	ProjectID string
	PacketID  string
	Option    string
	Notes     string
}

type ApprovalPacketApprovalResult struct {
	ApprovalPacket     ApprovalPacketRecord    `json:"approval_packet"`
	GeneratedArtifacts []ArtifactVersionRecord `json:"generated_artifacts,omitempty"`
	PromotedTask       *TaskRecord             `json:"promoted_task,omitempty"`
}

func (db *DB) CreateInitialProjectUnderstanding(ctx context.Context, projectID string, concept string) (UnderstandingIntakeResult, error) {
	return db.createUnderstandingIntake(ctx, understandingIntakeInput{
		ProjectID:     projectID,
		SourceType:    "initial_concept",
		RawText:       concept,
		ForceApproval: true,
	})
}

type understandingIntakeInput struct {
	ProjectID     string
	SourceType    string
	SourceID      string
	RawText       string
	Title         string
	ForceApproval bool
}

func (db *DB) createUnderstandingIntake(ctx context.Context, input understandingIntakeInput) (UnderstandingIntakeResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return UnderstandingIntakeResult{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.RawText) == "" {
		return UnderstandingIntakeResult{}, fmt.Errorf("intent text is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return UnderstandingIntakeResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := createUnderstandingIntakeTx(ctx, tx, input, now)
	if err != nil {
		return UnderstandingIntakeResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return UnderstandingIntakeResult{}, err
	}
	committed = true
	return result, nil
}

func createUnderstandingIntakeTx(ctx context.Context, tx *sql.Tx, input understandingIntakeInput, now string) (UnderstandingIntakeResult, error) {
	generated := planning.GenerateUnderstanding(planning.UnderstandingInput{
		SourceType: input.SourceType,
		Title:      input.Title,
		RawText:    input.RawText,
	})
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = normalizedIntentTitle(input.RawText)
	}
	intentID := "INTENT-" + stableShortHash(input.ProjectID+"|"+input.SourceType+"|"+input.SourceID+"|"+input.RawText)
	snapshotID := "USNAP-" + stableShortHash(intentID+"|"+generated.Risk.Level+"|"+generated.RecommendedGoMode)
	batchID := "PBATCH-" + stableShortHash(snapshotID+"|proposal")
	packetID := "APKT-" + stableShortHash(batchID+"|approval")
	approvalRequired := input.ForceApproval || planning.ApprovalRequired(generated.Risk.Level)
	status := "interpreted"
	if input.SourceType == "feature_request" {
		status = "planning"
	}
	if approvalRequired {
		status = "proposal_ready"
	}
	intent := IntentItemRecord{
		ID:              intentID,
		ProjectID:       input.ProjectID,
		SourceType:      input.SourceType,
		SourceID:        input.SourceID,
		RawText:         strings.TrimSpace(input.RawText),
		NormalizedTitle: title,
		Status:          status,
		RiskLevel:       generated.Risk.Level,
		Confidence:      generated.Confidence,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO intent_items(
  id, project_id, source_type, source_id, raw_text, normalized_title,
  status, risk_level, confidence, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  raw_text = excluded.raw_text,
  normalized_title = excluded.normalized_title,
  status = excluded.status,
  risk_level = excluded.risk_level,
  confidence = excluded.confidence,
  updated_at = excluded.updated_at`,
		intent.ID, intent.ProjectID, intent.SourceType, nullableText(intent.SourceID), intent.RawText, intent.NormalizedTitle,
		intent.Status, intent.RiskLevel, intent.Confidence, now, now,
	); err != nil {
		return UnderstandingIntakeResult{}, err
	}

	artifactSnapshot := map[string]any{
		"source_type": input.SourceType,
		"source_id":   input.SourceID,
		"created_at":  now,
	}
	snapshotStatus := "approved"
	if approvalRequired {
		snapshotStatus = "proposed"
	}
	snapshot := UnderstandingSnapshotRecord{
		ID:                snapshotID,
		ProjectID:         input.ProjectID,
		IntentItemID:      intent.ID,
		ArtifactSnapshot:  artifactSnapshot,
		InterpretedGoal:   generated.InterpretedGoal,
		UserValue:         generated.UserValue,
		NonGoals:          generated.NonGoals,
		Assumptions:       generated.Assumptions,
		OpenQuestions:     generated.OpenQuestions,
		AffectedContext:   generated.AffectedContext,
		Risk:              generated.Risk,
		Confidence:        generated.Confidence,
		RecommendedGoMode: generated.RecommendedGoMode,
		Status:            snapshotStatus,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := insertUnderstandingSnapshot(ctx, tx, snapshot); err != nil {
		return UnderstandingIntakeResult{}, err
	}

	summary := approvalPacketSummary(title, generated, "", "")
	proposalSummary := map[string]any{
		"title":                  title,
		"source_type":            input.SourceType,
		"source_id":              input.SourceID,
		"risk_level":             generated.Risk.Level,
		"recommended_go_mode":    generated.RecommendedGoMode,
		"approval_required":      approvalRequired,
		"understanding_snapshot": snapshotID,
	}
	batchStatus := "approved"
	packetStatus := "approved"
	if approvalRequired {
		batchStatus = "proposed"
		packetStatus = "open"
	}
	batch := ProposalBatchRecord{
		ID:                      batchID,
		ProjectID:               input.ProjectID,
		IntentItemIDs:           []string{intent.ID},
		UnderstandingSnapshotID: snapshot.ID,
		Status:                  batchStatus,
		RecommendedOption:       "approve_recommended",
		Summary:                 proposalSummary,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := insertProposalBatch(ctx, tx, batch); err != nil {
		return UnderstandingIntakeResult{}, err
	}
	deltas := proposalDeltasFor(input.ProjectID, batch.ID, generated.Risk.Level, summary, now)
	for _, delta := range deltas {
		if err := insertProposalDelta(ctx, tx, delta); err != nil {
			return UnderstandingIntakeResult{}, err
		}
	}
	packet := ApprovalPacketRecord{
		ID:                      packetID,
		ProjectID:               input.ProjectID,
		SourceType:              input.SourceType,
		SourceID:                input.SourceID,
		UnderstandingSnapshotID: snapshot.ID,
		ProposalBatchID:         batch.ID,
		Title:                   approvalPacketTitle(input.SourceType, title),
		Status:                  packetStatus,
		Summary:                 summary,
		Options:                 approvalPacketOptions(),
		RecommendedOption:       "approve_recommended",
		RiskLevel:               generated.Risk.Level,
		IntentRawText:           intent.RawText,
		IntentTitle:             intent.NormalizedTitle,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := insertApprovalPacket(ctx, tx, packet); err != nil {
		return UnderstandingIntakeResult{}, err
	}
	if packet.Status == "open" {
		if err := upsertApprovalPacketInboxItem(ctx, tx, packet, now); err != nil {
			return UnderstandingIntakeResult{}, err
		}
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "understanding_snapshot_created", map[string]any{
		"intent_item_id":            intent.ID,
		"understanding_snapshot_id": snapshot.ID,
		"approval_packet_id":        packet.ID,
		"risk_level":                packet.RiskLevel,
	}, now); err != nil {
		return UnderstandingIntakeResult{}, err
	}
	return UnderstandingIntakeResult{
		IntentItem:            intent,
		UnderstandingSnapshot: snapshot,
		ProposalBatch:         batch,
		ProposalDeltas:        deltas,
		ApprovalPacket:        packet,
	}, nil
}

func insertUnderstandingSnapshot(ctx context.Context, tx *sql.Tx, snapshot UnderstandingSnapshotRecord) error {
	values, err := understandingSnapshotJSONValues(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO understanding_snapshots(
  id, project_id, intent_item_id, artifact_snapshot_json, interpreted_goal_json,
  user_value_json, non_goals_json, assumptions_json, open_questions_json,
  affected_context_json, risk_json, confidence, recommended_go_mode, status,
  created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  artifact_snapshot_json = excluded.artifact_snapshot_json,
  interpreted_goal_json = excluded.interpreted_goal_json,
  user_value_json = excluded.user_value_json,
  non_goals_json = excluded.non_goals_json,
  assumptions_json = excluded.assumptions_json,
  open_questions_json = excluded.open_questions_json,
  affected_context_json = excluded.affected_context_json,
  risk_json = excluded.risk_json,
  confidence = excluded.confidence,
  recommended_go_mode = excluded.recommended_go_mode,
  status = excluded.status,
  updated_at = excluded.updated_at`,
		snapshot.ID, snapshot.ProjectID, snapshot.IntentItemID, values.artifactSnapshot, values.interpretedGoal,
		values.userValue, values.nonGoals, values.assumptions, values.openQuestions,
		values.affectedContext, values.risk, snapshot.Confidence, snapshot.RecommendedGoMode, snapshot.Status,
		snapshot.CreatedAt, snapshot.UpdatedAt,
	)
	return err
}

type understandingSnapshotJSON struct {
	artifactSnapshot string
	interpretedGoal  string
	userValue        string
	nonGoals         string
	assumptions      string
	openQuestions    string
	affectedContext  string
	risk             string
}

func understandingSnapshotJSONValues(snapshot UnderstandingSnapshotRecord) (understandingSnapshotJSON, error) {
	var out understandingSnapshotJSON
	var err error
	if out.artifactSnapshot, err = jsonString(snapshot.ArtifactSnapshot); err != nil {
		return out, err
	}
	if out.interpretedGoal, err = jsonString(snapshot.InterpretedGoal); err != nil {
		return out, err
	}
	if out.userValue, err = jsonString(snapshot.UserValue); err != nil {
		return out, err
	}
	if out.nonGoals, err = jsonString(snapshot.NonGoals); err != nil {
		return out, err
	}
	if out.assumptions, err = jsonString(snapshot.Assumptions); err != nil {
		return out, err
	}
	if out.openQuestions, err = jsonString(snapshot.OpenQuestions); err != nil {
		return out, err
	}
	if out.affectedContext, err = jsonString(snapshot.AffectedContext); err != nil {
		return out, err
	}
	if out.risk, err = jsonString(snapshot.Risk); err != nil {
		return out, err
	}
	return out, nil
}

func insertProposalBatch(ctx context.Context, tx *sql.Tx, batch ProposalBatchRecord) error {
	intentIDs, err := jsonString(batch.IntentItemIDs)
	if err != nil {
		return err
	}
	summary, err := jsonString(batch.Summary)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO proposal_batches(
  id, project_id, intent_item_ids_json, understanding_snapshot_id, status,
  recommended_option, summary_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  intent_item_ids_json = excluded.intent_item_ids_json,
  status = excluded.status,
  recommended_option = excluded.recommended_option,
  summary_json = excluded.summary_json,
  updated_at = excluded.updated_at`,
		batch.ID, batch.ProjectID, intentIDs, batch.UnderstandingSnapshotID, batch.Status,
		batch.RecommendedOption, summary, batch.CreatedAt, batch.UpdatedAt,
	)
	return err
}

func insertProposalDelta(ctx context.Context, tx *sql.Tx, delta ProposalDeltaRecord) error {
	rawDelta, err := jsonString(delta.Delta)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT OR REPLACE INTO proposal_deltas(
  id, project_id, proposal_batch_id, target_type, target_id,
  delta_json, rendered_markdown, risk_level, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		delta.ID, delta.ProjectID, delta.ProposalBatchID, delta.TargetType, nullableText(delta.TargetID),
		rawDelta, delta.RenderedMarkdown, delta.RiskLevel, delta.CreatedAt,
	)
	return err
}

func insertApprovalPacket(ctx context.Context, tx *sql.Tx, packet ApprovalPacketRecord) error {
	summary, err := jsonString(packet.Summary)
	if err != nil {
		return err
	}
	options, err := jsonString(packet.Options)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO approval_packets(
  id, project_id, source_type, source_id, understanding_snapshot_id,
  proposal_batch_id, title, status, summary_json, options_json,
  recommended_option, risk_level, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  title = excluded.title,
  status = excluded.status,
  summary_json = excluded.summary_json,
  options_json = excluded.options_json,
  recommended_option = excluded.recommended_option,
  risk_level = excluded.risk_level,
  updated_at = excluded.updated_at,
  resolved_at = CASE WHEN excluded.status = 'open' THEN NULL ELSE approval_packets.resolved_at END`,
		packet.ID, packet.ProjectID, packet.SourceType, nullableText(packet.SourceID), packet.UnderstandingSnapshotID,
		packet.ProposalBatchID, packet.Title, packet.Status, summary, options,
		packet.RecommendedOption, packet.RiskLevel, packet.CreatedAt, packet.UpdatedAt,
	)
	return err
}

func upsertApprovalPacketInboxItem(ctx context.Context, tx *sql.Tx, packet ApprovalPacketRecord, now string) error {
	inboxID := "INBOX-" + stableShortHash(packet.ProjectID+"|approval_packet|"+packet.ID)
	itemType := "human_decision"
	if packet.RiskLevel == "L4" {
		itemType = "hard_block"
	}
	body := approvalPacketInboxBody(packet)
	_, err := tx.ExecContext(ctx, `
INSERT INTO inbox_items(
  id, project_id, task_id, item_type, status, source_type, source_id,
  dedupe_key, batch_key, priority, title, body, created_at, updated_at
) VALUES (?, ?, NULL, ?, 'open', 'approval_packet', ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, dedupe_key, status) DO UPDATE SET
  title = excluded.title,
  body = excluded.body,
  priority = excluded.priority,
  updated_at = excluded.updated_at`,
		inboxID, packet.ProjectID, itemType, packet.ID, "approval_packet:"+packet.ID,
		packet.ProjectID+":approval_packet:"+packet.RiskLevel, approvalPacketPriority(packet.RiskLevel),
		packet.Title, body, now, now,
	)
	return err
}

func approvalPacketForSourceTx(ctx context.Context, tx *sql.Tx, projectID string, sourceType string, sourceID string) (ApprovalPacketRecord, bool, error) {
	query := `
SELECT ap.id, ap.project_id, ap.source_type, ap.source_id, ap.understanding_snapshot_id,
       ap.proposal_batch_id, ap.title, ap.status, ap.summary_json, ap.options_json,
       ap.recommended_option, ap.risk_level, ap.created_at, ap.updated_at, ap.resolved_at,
       ii.raw_text, ii.normalized_title
FROM approval_packets ap
JOIN understanding_snapshots us ON us.project_id = ap.project_id AND us.id = ap.understanding_snapshot_id
JOIN intent_items ii ON ii.project_id = us.project_id AND ii.id = us.intent_item_id
WHERE ap.project_id = ? AND ap.source_type = ?`
	args := []any{projectID, sourceType}
	if strings.TrimSpace(sourceID) == "" {
		query += " AND ap.source_id IS NULL"
	} else {
		query += " AND ap.source_id = ?"
		args = append(args, sourceID)
	}
	query += " ORDER BY ap.created_at DESC LIMIT 1"
	packet, err := scanApprovalPacket(tx.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return ApprovalPacketRecord{}, false, nil
	}
	if err != nil {
		return ApprovalPacketRecord{}, false, err
	}
	return packet, true, nil
}

func updateApprovalPacketSummaryTx(ctx context.Context, tx *sql.Tx, packet ApprovalPacketRecord, now string) error {
	summaryJSON, err := jsonString(packet.Summary)
	if err != nil {
		return err
	}
	optionsJSON, err := jsonString(packet.Options)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE approval_packets
SET title = ?,
    status = ?,
    summary_json = ?,
    options_json = ?,
    recommended_option = ?,
    risk_level = ?,
    updated_at = ?,
    resolved_at = CASE WHEN ? = 'open' THEN NULL ELSE COALESCE(resolved_at, ?) END
WHERE project_id = ? AND id = ?`,
		packet.Title, packet.Status, summaryJSON, optionsJSON, packet.RecommendedOption, packet.RiskLevel,
		now, packet.Status, now, packet.ProjectID, packet.ID,
	)
	return err
}

func (db *DB) ListUnderstandingSnapshots(ctx context.Context, projectID string) ([]UnderstandingSnapshotRecord, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT id, project_id, intent_item_id, artifact_snapshot_json, interpreted_goal_json,
       user_value_json, non_goals_json, assumptions_json, open_questions_json,
       affected_context_json, risk_json, confidence, recommended_go_mode, status,
       created_at, updated_at
FROM understanding_snapshots
WHERE project_id = ?
ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []UnderstandingSnapshotRecord
	for rows.Next() {
		record, err := scanUnderstandingSnapshot(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) ListApprovalPackets(ctx context.Context, projectID string, status string) ([]ApprovalPacketRecord, error) {
	query := `
SELECT ap.id, ap.project_id, ap.source_type, ap.source_id, ap.understanding_snapshot_id,
       ap.proposal_batch_id, ap.title, ap.status, ap.summary_json, ap.options_json,
       ap.recommended_option, ap.risk_level, ap.created_at, ap.updated_at, ap.resolved_at,
       ii.raw_text, ii.normalized_title
FROM approval_packets ap
JOIN understanding_snapshots us ON us.project_id = ap.project_id AND us.id = ap.understanding_snapshot_id
JOIN intent_items ii ON ii.project_id = us.project_id AND ii.id = us.intent_item_id
WHERE ap.project_id = ?`
	args := []any{projectID}
	if strings.TrimSpace(status) != "" {
		query += " AND ap.status = ?"
		args = append(args, status)
	}
	query += " ORDER BY ap.created_at ASC"
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ApprovalPacketRecord
	for rows.Next() {
		record, err := scanApprovalPacket(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (db *DB) GetApprovalPacket(ctx context.Context, projectID string, packetID string) (ApprovalPacketRecord, error) {
	row := db.sql.QueryRowContext(ctx, `
SELECT ap.id, ap.project_id, ap.source_type, ap.source_id, ap.understanding_snapshot_id,
       ap.proposal_batch_id, ap.title, ap.status, ap.summary_json, ap.options_json,
       ap.recommended_option, ap.risk_level, ap.created_at, ap.updated_at, ap.resolved_at,
       ii.raw_text, ii.normalized_title
FROM approval_packets ap
JOIN understanding_snapshots us ON us.project_id = ap.project_id AND us.id = ap.understanding_snapshot_id
JOIN intent_items ii ON ii.project_id = us.project_id AND ii.id = us.intent_item_id
WHERE ap.project_id = ? AND ap.id = ?`, projectID, packetID)
	return scanApprovalPacket(row)
}

func (db *DB) ApproveApprovalPacket(ctx context.Context, input ApprovalPacketApprovalInput) (ApprovalPacketApprovalResult, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ApprovalPacketApprovalResult{}, fmt.Errorf("project id is required")
	}
	if strings.TrimSpace(input.PacketID) == "" {
		return ApprovalPacketApprovalResult{}, fmt.Errorf("approval packet id is required")
	}
	option := strings.TrimSpace(input.Option)
	if option == "" {
		option = "approve_recommended"
	}
	targetStatus, err := approvalPacketStatusForOption(option, input.Notes)
	if err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	packet, err := approvalPacketByID(ctx, tx, input.ProjectID, input.PacketID)
	if err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	if packet.Status != "open" {
		return ApprovalPacketApprovalResult{}, fmt.Errorf("approval packet %s is not open: %s", input.PacketID, packet.Status)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE approval_packets
SET status = ?, updated_at = ?, resolved_at = ?
WHERE project_id = ? AND id = ? AND status = 'open'`,
		targetStatus, now, now, input.ProjectID, input.PacketID,
	); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	batchStatus := targetStatus
	if targetStatus == "cancelled" {
		batchStatus = "rejected"
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE proposal_batches
SET status = ?, updated_at = ?, resolved_at = ?
WHERE project_id = ? AND id = ?`,
		batchStatus, now, now, input.ProjectID, packet.ProposalBatchID,
	); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	snapshotStatus := "approved"
	if targetStatus == "rejected" || targetStatus == "cancelled" {
		snapshotStatus = "superseded"
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE understanding_snapshots
SET status = ?, updated_at = ?
WHERE project_id = ? AND id = ?`,
		snapshotStatus, now, input.ProjectID, packet.UnderstandingSnapshotID,
	); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	intentStatus := "approved_for_execution"
	if targetStatus == "rejected" || targetStatus == "cancelled" {
		intentStatus = "cancelled"
	}
	if packet.RiskLevel == "L4" && (targetStatus == "approved" || targetStatus == "approved_with_notes") {
		intentStatus = "needs_clarification"
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE intent_items
SET status = ?, updated_at = ?
WHERE project_id = ?
  AND id = (SELECT intent_item_id FROM understanding_snapshots WHERE project_id = ? AND id = ?)`,
		intentStatus, now, input.ProjectID, input.ProjectID, packet.UnderstandingSnapshotID,
	); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE inbox_items
SET status = 'resolved', updated_at = ?, resolved_at = ?
WHERE project_id = ? AND source_type = 'approval_packet' AND source_id = ? AND status = 'open'`,
		now, now, input.ProjectID, input.PacketID,
	); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	var promotedTask *TaskRecord
	if (targetStatus == "approved" || targetStatus == "approved_with_notes") && packet.SourceType == "feature_request" && packet.RiskLevel != "L4" {
		task, err := promoteApprovalPacketTask(ctx, tx, packet, now)
		if err != nil {
			return ApprovalPacketApprovalResult{}, err
		}
		if task.ID != "" {
			promotedTask = &task
		}
	}
	if err := insertWorkflowEvent(ctx, tx, input.ProjectID, "approval_packet_resolved", map[string]any{
		"approval_packet_id": input.PacketID,
		"selected_option":    option,
		"status":             targetStatus,
		"risk_level":         packet.RiskLevel,
		"notes":              strings.TrimSpace(input.Notes),
	}, now); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	committed = true
	updated, err := db.GetApprovalPacket(ctx, input.ProjectID, input.PacketID)
	if err != nil {
		return ApprovalPacketApprovalResult{}, err
	}
	result := ApprovalPacketApprovalResult{ApprovalPacket: updated, PromotedTask: promotedTask}
	if (updated.Status == "approved" || updated.Status == "approved_with_notes") && updated.SourceType == "initial_concept" {
		artifacts, err := db.GenerateInitialArtifactsForApprovalPacket(ctx, input.ProjectID, input.PacketID)
		if err != nil {
			return ApprovalPacketApprovalResult{}, err
		}
		result.GeneratedArtifacts = artifacts
	}
	return result, nil
}

func promoteApprovalPacketTask(ctx context.Context, tx *sql.Tx, packet ApprovalPacketRecord, now string) (TaskRecord, error) {
	taskGroupID := packet.Summary.TaskGroupID
	taskID := packet.Summary.TaskID
	if strings.TrimSpace(taskGroupID) == "" || strings.TrimSpace(taskID) == "" {
		return TaskRecord{}, nil
	}
	if err := statemachine.Task.ValidateTransition("proposed", "ready"); err != nil {
		return TaskRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE task_groups
SET status = 'ready', updated_at = ?
WHERE project_id = ? AND id = ? AND status = 'proposed'`,
		now, packet.ProjectID, taskGroupID); err != nil {
		return TaskRecord{}, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE tasks
SET status = 'ready', updated_at = ?
WHERE project_id = ? AND id = ? AND task_group_id = ? AND status = 'proposed'`,
		now, packet.ProjectID, taskID, taskGroupID)
	if err != nil {
		return TaskRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return TaskRecord{}, err
	}
	if affected != 1 {
		return TaskRecord{}, fmt.Errorf("proposed task not found for approval packet: %s", taskID)
	}
	queueID, err := enqueueTaskImplementationWorkItem(ctx, tx, packet.ProjectID, taskID, now)
	if err != nil {
		return TaskRecord{}, err
	}
	if packet.SourceID != "" {
		if _, err := tx.ExecContext(ctx, `
UPDATE feature_requests
SET status = 'planned', task_group_id = ?, updated_at = ?
WHERE project_id = ? AND id = ?`,
			taskGroupID, now, packet.ProjectID, packet.SourceID,
		); err != nil {
			return TaskRecord{}, err
		}
	}
	if err := insertWorkflowEvent(ctx, tx, packet.ProjectID, "approval_packet_task_promoted", map[string]any{
		"approval_packet_id": packet.ID,
		"task_group_id":      taskGroupID,
		"task_id":            taskID,
		"work_queue_item_id": queueID,
	}, now); err != nil {
		return TaskRecord{}, err
	}
	return TaskRecord{ID: taskID, Status: "ready", Title: "Implement " + packet.IntentTitle}, nil
}

func (db *DB) GenerateInitialArtifactsForApprovalPacket(ctx context.Context, projectID string, packetID string) ([]ArtifactVersionRecord, error) {
	packet, err := db.GetApprovalPacket(ctx, projectID, packetID)
	if err != nil {
		return nil, err
	}
	if packet.SourceType != "initial_concept" {
		return nil, nil
	}
	if packet.Status != "approved" && packet.Status != "approved_with_notes" {
		return nil, fmt.Errorf("approval packet %s is not approved: %s", packetID, packet.Status)
	}
	var root string
	if err := db.sql.QueryRowContext(ctx, "SELECT root_path FROM projects WHERE id = ?", projectID).Scan(&root); err != nil {
		return nil, err
	}
	generated := artifactgen.BuildInitialArtifacts(root, packet.IntentRawText, true)
	records := make([]ArtifactVersionRecord, 0, len(generated))
	for _, artifact := range generated {
		if err := writeProjectArtifactFile(root, artifact.Path, artifact.Content); err != nil {
			return nil, err
		}
		record, err := db.SaveArtifactVersion(ctx, ArtifactVersionInput{
			ProjectID:    projectID,
			ArtifactType: ArtifactType(artifact.Type),
			Path:         filepath.ToSlash(artifact.Path),
			Content:      artifact.Content,
			Status:       "proposed",
		})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func approvalPacketByID(ctx context.Context, tx *sql.Tx, projectID string, packetID string) (ApprovalPacketRecord, error) {
	row := tx.QueryRowContext(ctx, `
SELECT ap.id, ap.project_id, ap.source_type, ap.source_id, ap.understanding_snapshot_id,
       ap.proposal_batch_id, ap.title, ap.status, ap.summary_json, ap.options_json,
       ap.recommended_option, ap.risk_level, ap.created_at, ap.updated_at, ap.resolved_at,
       ii.raw_text, ii.normalized_title
FROM approval_packets ap
JOIN understanding_snapshots us ON us.project_id = ap.project_id AND us.id = ap.understanding_snapshot_id
JOIN intent_items ii ON ii.project_id = us.project_id AND ii.id = us.intent_item_id
WHERE ap.project_id = ? AND ap.id = ?`, projectID, packetID)
	return scanApprovalPacket(row)
}

func scanUnderstandingSnapshot(scanner interface{ Scan(dest ...any) error }) (UnderstandingSnapshotRecord, error) {
	var record UnderstandingSnapshotRecord
	var artifactSnapshot, interpretedGoal, userValue, nonGoals, assumptions, openQuestions, affectedContext, risk string
	if err := scanner.Scan(
		&record.ID,
		&record.ProjectID,
		&record.IntentItemID,
		&artifactSnapshot,
		&interpretedGoal,
		&userValue,
		&nonGoals,
		&assumptions,
		&openQuestions,
		&affectedContext,
		&risk,
		&record.Confidence,
		&record.RecommendedGoMode,
		&record.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(artifactSnapshot), &record.ArtifactSnapshot); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(interpretedGoal), &record.InterpretedGoal); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(userValue), &record.UserValue); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(nonGoals), &record.NonGoals); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(assumptions), &record.Assumptions); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(openQuestions), &record.OpenQuestions); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(affectedContext), &record.AffectedContext); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	if err := json.Unmarshal([]byte(risk), &record.Risk); err != nil {
		return UnderstandingSnapshotRecord{}, err
	}
	return record, nil
}

func scanApprovalPacket(scanner interface{ Scan(dest ...any) error }) (ApprovalPacketRecord, error) {
	var record ApprovalPacketRecord
	var sourceID, resolvedAt sql.NullString
	var summaryJSON, optionsJSON string
	if err := scanner.Scan(
		&record.ID,
		&record.ProjectID,
		&record.SourceType,
		&sourceID,
		&record.UnderstandingSnapshotID,
		&record.ProposalBatchID,
		&record.Title,
		&record.Status,
		&summaryJSON,
		&optionsJSON,
		&record.RecommendedOption,
		&record.RiskLevel,
		&record.CreatedAt,
		&record.UpdatedAt,
		&resolvedAt,
		&record.IntentRawText,
		&record.IntentTitle,
	); err != nil {
		return ApprovalPacketRecord{}, err
	}
	if sourceID.Valid {
		record.SourceID = sourceID.String
	}
	if resolvedAt.Valid {
		record.ResolvedAt = resolvedAt.String
	}
	if err := json.Unmarshal([]byte(summaryJSON), &record.Summary); err != nil {
		return ApprovalPacketRecord{}, err
	}
	if err := json.Unmarshal([]byte(optionsJSON), &record.Options); err != nil {
		return ApprovalPacketRecord{}, err
	}
	return record, nil
}

func approvalPacketStatusForOption(option string, notes string) (string, error) {
	switch option {
	case "approve_recommended":
		return "approved", nil
	case "approve_with_notes":
		if strings.TrimSpace(notes) == "" {
			return "", fmt.Errorf("approve_with_notes requires notes")
		}
		return "approved_with_notes", nil
	case "request_changes":
		return "rejected", nil
	case "cancel":
		return "cancelled", nil
	default:
		return "", fmt.Errorf("unsupported approval packet option: %s", option)
	}
}

func approvalPacketSummary(title string, generated planning.UnderstandingSnapshot, taskGroupID string, taskID string) ApprovalPacketSummary {
	included := []string{"Understanding Snapshot", "Scope boundary", "Risk-based go recommendation"}
	if taskID != "" {
		included = append(included, "Feature chunk task proposal")
	}
	excluded := []string{"Unrequested dependencies", "Unrequested external integrations", "Micro-task splitting by implementation file"}
	return ApprovalPacketSummary{
		OneLiner:          "DevOS understands the request as: " + title,
		UserValue:         generated.UserValue,
		ExistingAlignment: generated.AffectedContext,
		ProposedScope: ScopeSummary{
			Included: included,
			Excluded: excluded,
		},
		Assumptions:   generated.Assumptions,
		OpenQuestions: generated.OpenQuestions,
		Recommendation: ApprovalRecommendation{
			Option: "approve_recommended",
			Reason: recommendationReason(generated.Risk.Level),
		},
		Risk:        generated.Risk,
		TaskGroupID: taskGroupID,
		TaskID:      taskID,
		NextAction:  generated.RecommendedGoMode,
	}
}

func proposalDeltasFor(projectID string, batchID string, riskLevel string, summary ApprovalPacketSummary, now string) []ProposalDeltaRecord {
	targets := []string{"prd", "architecture", "roadmap", "task_group"}
	deltas := make([]ProposalDeltaRecord, 0, len(targets))
	for _, target := range targets {
		deltas = append(deltas, ProposalDeltaRecord{
			ID:              "PDELTA-" + stableShortHash(projectID+"|"+batchID+"|"+target),
			ProjectID:       projectID,
			ProposalBatchID: batchID,
			TargetType:      target,
			Delta: map[string]any{
				"one_liner":   summary.OneLiner,
				"next_action": summary.NextAction,
			},
			RenderedMarkdown: "- " + summary.OneLiner,
			RiskLevel:        riskLevel,
			CreatedAt:        now,
		})
	}
	return deltas
}

func approvalPacketOptions() []DecisionOption {
	return []DecisionOption{
		{ID: "approve_recommended", Label: "Approve recommended", Description: "Accept the recommended scope and continue."},
		{ID: "approve_with_notes", Label: "Approve with notes", Description: "Continue while preserving human notes as approval context."},
		{ID: "request_changes", Label: "Request changes", Description: "Do not proceed; ask DevOS to revise the packet."},
		{ID: "cancel", Label: "Cancel", Description: "Cancel this intent or proposal."},
	}
}

func approvalPacketTitle(sourceType string, title string) string {
	if sourceType == "initial_concept" {
		return "Initial Product Understanding"
	}
	return "Implementation Understanding: " + title
}

func recommendationReason(level string) string {
	switch level {
	case "L0":
		return "This is a no-gate change and can proceed after recording the understanding."
	case "L1":
		return "No high-risk boundary was detected, so DevOS can implement with explicit assumptions."
	case "L2":
		return "The work may affect user-facing scope, so approval is recommended before implementation."
	case "L3":
		return "Canonical product or architecture artifacts may change, so approval is required first."
	case "L4":
		return "A high-risk boundary was detected; do not create ready implementation work from this packet."
	default:
		return "Review the packet before implementation."
	}
}

func approvalPacketPriority(level string) int {
	switch level {
	case "L4":
		return 95
	case "L3":
		return 85
	case "L2":
		return 75
	default:
		return 40
	}
}

func approvalPacketInboxBody(packet ApprovalPacketRecord) string {
	return strings.Join([]string{
		packet.Summary.OneLiner,
		"Risk: " + packet.RiskLevel,
		"Recommendation: " + packet.Summary.Recommendation.Reason,
	}, "\n\n")
}

func normalizedIntentTitle(raw string) string {
	line := strings.TrimSpace(raw)
	if i := strings.Index(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = strings.Trim(strings.TrimSpace(line), "# ")
	if line == "" {
		return "Untitled intent"
	}
	words := strings.Fields(line)
	if len(words) > 12 {
		words = words[:12]
	}
	return strings.Join(words, " ")
}

func jsonString(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
