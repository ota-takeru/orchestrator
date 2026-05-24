ALTER TABLE feature_requests ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE feature_requests ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE feature_requests ADD COLUMN source TEXT NOT NULL DEFAULT 'human';
ALTER TABLE feature_requests ADD COLUMN priority TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE feature_requests ADD COLUMN tier TEXT;
ALTER TABLE feature_requests ADD COLUMN task_group_id TEXT;
ALTER TABLE feature_requests ADD COLUMN resolved_at TEXT;

UPDATE feature_requests
SET title = body,
    description = body
WHERE title = '' AND description = '';

CREATE TABLE work_queue_items (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  lane TEXT NOT NULL CHECK (lane IN ('planning', 'consolidation', 'execution', 'merge')),
  item_type TEXT NOT NULL CHECK (item_type IN (
    'planning_run',
    'planning_consolidation',
    'canonical_commit',
    'feature_request_analysis',
    'change_request_analysis',
    'task_implementation',
    'task_repair',
    'task_review',
    'merge_queue_processing',
    'environment_rerun'
  )),
  item_id TEXT NOT NULL,
  preferred_environment_id TEXT,
  required_environment_id TEXT,
  run_profile_id TEXT,
  status TEXT NOT NULL CHECK (status IN (
    'queued',
    'leased',
    'running',
    'heartbeat_lost',
    'waiting_for_human',
    'blocked',
    'completed',
    'failed',
    'cancelled'
  )),
  priority TEXT NOT NULL CHECK (priority IN ('critical', 'high', 'medium', 'low')),
  blocked_reason TEXT,
  run_after TEXT,
  lease_owner TEXT,
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  attempt_no INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  idempotency_key TEXT NOT NULL,
  error_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_open_work_queue_idempotency
  ON work_queue_items(project_id, lane, idempotency_key)
  WHERE status IN ('queued', 'leased', 'running', 'heartbeat_lost', 'waiting_for_human', 'blocked');

CREATE INDEX idx_work_queue_lane_status_priority
  ON work_queue_items(project_id, lane, status, priority, run_after, created_at);

CREATE INDEX idx_work_queue_lease_expiry
  ON work_queue_items(project_id, lane, lease_expires_at)
  WHERE status IN ('leased', 'running');
