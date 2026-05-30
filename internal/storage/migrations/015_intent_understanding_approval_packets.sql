CREATE TABLE intent_items (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  source_type TEXT NOT NULL CHECK (source_type IN (
    'initial_concept',
    'feature_request',
    'change_request',
    'inbox_reply',
    'artifact_note',
    'environment_input',
    'dependency_approval'
  )),
  source_id TEXT,
  raw_text TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN (
    'received',
    'interpreting',
    'interpreted',
    'needs_clarification',
    'planning',
    'proposal_ready',
    'approved_for_execution',
    'superseded',
    'cancelled'
  )),
  risk_level TEXT NOT NULL CHECK (risk_level IN ('L0', 'L1', 'L2', 'L3', 'L4')),
  confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE understanding_snapshots (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  intent_item_id TEXT NOT NULL REFERENCES intent_items(id) ON DELETE RESTRICT,
  artifact_snapshot_json TEXT NOT NULL,
  interpreted_goal_json TEXT NOT NULL,
  user_value_json TEXT NOT NULL,
  non_goals_json TEXT NOT NULL,
  assumptions_json TEXT NOT NULL,
  open_questions_json TEXT NOT NULL,
  affected_context_json TEXT NOT NULL,
  risk_json TEXT NOT NULL,
  confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
  recommended_go_mode TEXT NOT NULL CHECK (recommended_go_mode IN (
    'no_gate',
    'implement_with_assumptions',
    'approval_before_implementation',
    'approval_before_canonical_artifact_update',
    'hard_gate'
  )),
  status TEXT NOT NULL CHECK (status IN ('draft', 'proposed', 'approved', 'superseded')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE proposal_batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  intent_item_ids_json TEXT NOT NULL,
  understanding_snapshot_id TEXT NOT NULL REFERENCES understanding_snapshots(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN ('proposed', 'approved', 'approved_with_notes', 'rejected', 'superseded')),
  recommended_option TEXT NOT NULL,
  summary_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE proposal_deltas (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  proposal_batch_id TEXT NOT NULL REFERENCES proposal_batches(id) ON DELETE RESTRICT,
  target_type TEXT NOT NULL CHECK (target_type IN ('prd', 'architecture', 'roadmap', 'task_group', 'task', 'policy', 'memory')),
  target_id TEXT,
  delta_json TEXT NOT NULL,
  rendered_markdown TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK (risk_level IN ('L0', 'L1', 'L2', 'L3', 'L4')),
  created_at TEXT NOT NULL
);

CREATE TABLE approval_packets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  source_type TEXT NOT NULL CHECK (source_type IN ('initial_concept', 'feature_request', 'change_request', 'planning_consolidation')),
  source_id TEXT,
  understanding_snapshot_id TEXT NOT NULL REFERENCES understanding_snapshots(id) ON DELETE RESTRICT,
  proposal_batch_id TEXT NOT NULL REFERENCES proposal_batches(id) ON DELETE RESTRICT,
  title TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('open', 'approved', 'approved_with_notes', 'rejected', 'superseded', 'cancelled')),
  summary_json TEXT NOT NULL,
  options_json TEXT NOT NULL,
  recommended_option TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK (risk_level IN ('L0', 'L1', 'L2', 'L3', 'L4')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE INDEX idx_intent_items_project_status
  ON intent_items(project_id, status, created_at);

CREATE INDEX idx_intent_items_source
  ON intent_items(project_id, source_type, source_id);

CREATE INDEX idx_understanding_snapshots_project_status
  ON understanding_snapshots(project_id, status, created_at);

CREATE INDEX idx_understanding_snapshots_intent
  ON understanding_snapshots(project_id, intent_item_id);

CREATE INDEX idx_proposal_batches_project_status
  ON proposal_batches(project_id, status, created_at);

CREATE INDEX idx_proposal_deltas_batch
  ON proposal_deltas(project_id, proposal_batch_id, target_type);

CREATE INDEX idx_approval_packets_project_status
  ON approval_packets(project_id, status, created_at);

CREATE INDEX idx_approval_packets_source
  ON approval_packets(project_id, source_type, source_id);
