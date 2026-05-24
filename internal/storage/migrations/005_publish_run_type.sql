-- devos:non_transactional
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;

BEGIN TRANSACTION;

ALTER TABLE runs RENAME TO runs_old;

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
  run_type TEXT NOT NULL CHECK (run_type IN ('implementation', 'repair', 'verification', 'review', 'replan', 'rebase', 'reverify', 'merge', 'patch_export', 'cleanup', 'worktree_safety', 'publish')),
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out', 'blocked')),
  run_profile_id TEXT REFERENCES project_run_profiles(id),
  implementation_environment_id TEXT REFERENCES execution_environments(id),
  attempt_no INTEGER NOT NULL,
  repair_of_run_id TEXT REFERENCES runs(id),
  reverify_context_type TEXT,
  reverify_context_id TEXT,
  base_commit TEXT NOT NULL,
  head_commit TEXT,
  diff_hash TEXT,
  sandbox_profile TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  UNIQUE(task_id, run_type, attempt_no)
);

INSERT INTO runs(
  id, project_id, task_id, run_type, status, run_profile_id,
  implementation_environment_id, attempt_no, repair_of_run_id,
  reverify_context_type, reverify_context_id, base_commit, head_commit,
  diff_hash, sandbox_profile, created_at, updated_at, started_at, completed_at
)
SELECT
  id, project_id, task_id, run_type, status, run_profile_id,
  implementation_environment_id, attempt_no, repair_of_run_id,
  reverify_context_type, reverify_context_id, base_commit, head_commit,
  diff_hash, sandbox_profile, created_at, updated_at, started_at, completed_at
FROM runs_old;

DROP TABLE runs_old;

CREATE INDEX idx_runs_task_status ON runs(task_id, status);

COMMIT;

PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
