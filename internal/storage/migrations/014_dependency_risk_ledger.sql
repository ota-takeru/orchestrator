CREATE TABLE dependency_risk_ledger (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  package_manager TEXT NOT NULL CHECK (package_manager IN ('go', 'npm', 'pnpm', 'yarn', 'cargo', 'other')),
  dependency_type TEXT NOT NULL CHECK (dependency_type IN ('production', 'development', 'tool')),
  introduced_by_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
  introduced_by_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  decision_id TEXT REFERENCES decisions(id) ON DELETE SET NULL,
  reason TEXT NOT NULL,
  approved_by TEXT,
  risk TEXT NOT NULL CHECK (risk IN ('low', 'medium', 'high', 'critical')),
  lockfile_changed INTEGER NOT NULL CHECK (lockfile_changed IN (0, 1)),
  lifecycle_scripts TEXT NOT NULL CHECK (lifecycle_scripts IN ('none_detected', 'detected', 'unknown')),
  current_version TEXT,
  approved_scope TEXT NOT NULL CHECK (approved_scope IN ('project', 'task', 'one_time', 'dependency_family')),
  expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_dependency_risk_ledger_project
  ON dependency_risk_ledger(project_id, package_manager, name);

CREATE INDEX idx_dependency_risk_ledger_task
  ON dependency_risk_ledger(project_id, introduced_by_task_id);

CREATE INDEX idx_dependency_risk_ledger_risk
  ON dependency_risk_ledger(project_id, risk, approved_scope, expires_at);
