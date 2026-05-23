CREATE TABLE merge_queue_entries (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict', 'merged', 'cancelled')),
  base_commit TEXT NOT NULL,
  head_commit TEXT NOT NULL,
  final_review_approval_id TEXT NOT NULL REFERENCES human_approvals(id),
  merge_approval_id TEXT NOT NULL REFERENCES human_approvals(id),
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT
);

CREATE UNIQUE INDEX idx_merge_queue_one_open_task
ON merge_queue_entries(task_id)
WHERE status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict');

CREATE TABLE patch_applications (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN ('exported', 'manually_applied', 'verifying', 'verified', 'needs_decision', 'cancelled', 'failed')),
  patch_artifact_id TEXT REFERENCES run_artifacts(id),
  patch_hash TEXT NOT NULL,
  applied_commit TEXT,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE TABLE semantic_behavior_diffs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
  diff_artifact_id TEXT NOT NULL REFERENCES run_artifacts(id),
  status TEXT NOT NULL CHECK (status IN ('draft', 'ready', 'superseded')),
  summary_json TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_patch_applications_task_status ON patch_applications(task_id, status);
CREATE INDEX idx_semantic_behavior_diffs_task ON semantic_behavior_diffs(task_id);
