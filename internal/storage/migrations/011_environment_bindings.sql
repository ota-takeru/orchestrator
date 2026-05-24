CREATE TABLE environment_requirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  environment_id TEXT REFERENCES execution_environments(id) ON DELETE SET NULL,
  key TEXT NOT NULL,
  required_for TEXT NOT NULL CHECK (required_for IN ('implementation', 'verification', 'runtime', 'runtime_smoke', 'deployment')),
  status TEXT NOT NULL CHECK (status IN ('missing', 'requested', 'configured', 'invalid', 'waived', 'cancelled', 'revoked')),
  source_hint TEXT NOT NULL CHECK (source_hint IN ('user_input', 'generated_example', 'external_secret')),
  validation_json TEXT NOT NULL,
  description TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE environment_bindings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  environment_id TEXT REFERENCES execution_environments(id) ON DELETE SET NULL,
  key TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('project', 'task', 'run', 'user_default')),
  scope_id TEXT,
  storage TEXT NOT NULL CHECK (storage IN ('env_file', 'os_keychain', 'external_secret')),
  storage_ref TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('configured', 'missing', 'invalid', 'revoked')),
  redacted_preview TEXT,
  value_fingerprint TEXT,
  created_by TEXT NOT NULL CHECK (created_by IN ('human', 'policy')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_by_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL
);

CREATE TABLE environment_audit_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  environment_id TEXT REFERENCES execution_environments(id) ON DELETE SET NULL,
  binding_id TEXT REFERENCES environment_bindings(id) ON DELETE SET NULL,
  requirement_id TEXT REFERENCES environment_requirements(id) ON DELETE SET NULL,
  key TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('requested', 'configured', 'updated', 'revoked', 'used', 'validation_failed')),
  actor TEXT NOT NULL CHECK (actor IN ('human', 'orchestrator', 'policy', 'system')),
  scope TEXT NOT NULL CHECK (scope IN ('project', 'task', 'run', 'user_default')),
  scope_id TEXT,
  run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  command_event_id TEXT REFERENCES command_events(id) ON DELETE SET NULL,
  redacted_preview TEXT,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_env_binding_env_key_scope
  ON environment_bindings(project_id, environment_id, key, scope, COALESCE(scope_id, ''))
  WHERE environment_id IS NOT NULL;

CREATE UNIQUE INDEX idx_env_binding_global_key_scope
  ON environment_bindings(project_id, key, scope, COALESCE(scope_id, ''))
  WHERE environment_id IS NULL;
