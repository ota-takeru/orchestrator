CREATE TABLE planning_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  feature_request_id TEXT REFERENCES feature_requests(id) ON DELETE SET NULL,
  run_type TEXT NOT NULL CHECK (run_type IN (
    'feature_detail',
    'impact_analysis',
    'decision_draft',
    'task_group_proposal',
    'risk_report',
    'rolling_checkpoint'
  )),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'stale')),
  artifact_snapshot_json TEXT NOT NULL,
  input_hash TEXT NOT NULL,
  output_summary TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE planning_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  planning_run_id TEXT NOT NULL REFERENCES planning_runs(id) ON DELETE RESTRICT,
  feature_request_id TEXT REFERENCES feature_requests(id) ON DELETE SET NULL,
  artifact_type TEXT NOT NULL CHECK (artifact_type IN (
    'feature_detail_report',
    'impact_analysis_report',
    'task_group_proposal',
    'risk_report',
    'rolling_checkpoint_report'
  )),
  status TEXT NOT NULL CHECK (status IN ('draft', 'proposed', 'accepted', 'rejected', 'superseded', 'stale')),
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  artifact_snapshot_json TEXT NOT NULL,
  superseded_by_id TEXT REFERENCES planning_artifacts(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE decision_report_drafts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  planning_run_id TEXT REFERENCES planning_runs(id) ON DELETE SET NULL,
  feature_request_id TEXT REFERENCES feature_requests(id) ON DELETE SET NULL,
  decision_type TEXT NOT NULL CHECK (decision_type IN (
    'dependency',
    'architecture',
    'db_schema',
    'auth',
    'external_api',
    'ux',
    'policy',
    'scope',
    'privacy'
  )),
  status TEXT NOT NULL CHECK (status IN ('draft', 'batched', 'promoted', 'rejected', 'superseded', 'stale')),
  title TEXT NOT NULL,
  batch_key TEXT,
  recommended_option TEXT,
  content_json TEXT NOT NULL,
  artifact_snapshot_json TEXT NOT NULL,
  promoted_decision_id TEXT REFERENCES decisions(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_planning_runs_input
  ON planning_runs(project_id, feature_request_id, run_type, input_hash)
  WHERE status IN ('queued', 'running', 'succeeded');

CREATE INDEX idx_planning_runs_project_status
  ON planning_runs(project_id, status, updated_at);

CREATE INDEX idx_planning_artifacts_feature_type_status
  ON planning_artifacts(project_id, feature_request_id, artifact_type, status);
