CREATE TABLE worker_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  lane TEXT NOT NULL CHECK (lane IN ('planning', 'consolidation', 'execution', 'merge')),
  mode TEXT NOT NULL CHECK (mode IN ('bounded_parallel', 'sequential')),
  max_concurrency INTEGER NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running', 'paused', 'stopped', 'failed', 'heartbeat_lost')),
  started_at TEXT NOT NULL,
  finished_at TEXT,
  stop_reason TEXT,
  lease_owner TEXT,
  last_heartbeat_at TEXT
);

CREATE UNIQUE INDEX idx_worker_runs_one_running_lane
  ON worker_runs(project_id, lane)
  WHERE status = 'running';
