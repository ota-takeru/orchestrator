ALTER TABLE planning_runs ADD COLUMN change_request_id TEXT;
ALTER TABLE planning_artifacts ADD COLUMN change_request_id TEXT;
ALTER TABLE decision_report_drafts ADD COLUMN change_request_id TEXT;

CREATE INDEX idx_planning_artifacts_change_type_status
  ON planning_artifacts(project_id, change_request_id, artifact_type, status);
