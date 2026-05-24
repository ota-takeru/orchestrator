CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  memory_type TEXT NOT NULL CHECK (memory_type IN ('policy', 'preference', 'implementation_note', 'baseline_issue')),
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('project', 'task', 'dependency_family', 'one_time', 'user_default')),
  scope_id TEXT NOT NULL DEFAULT '',
  expires_at TEXT,
  invalidated_at TEXT,
  invalidated_by_change_request_id TEXT REFERENCES change_requests(id) ON DELETE SET NULL,
  source_type TEXT NOT NULL CHECK (source_type IN ('human_decision', 'merge', 'change_request', 'system')),
  source_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, memory_type, key, scope, scope_id)
);

CREATE INDEX idx_memories_project_scope
  ON memories(project_id, memory_type, scope, scope_id, invalidated_at, expires_at);
