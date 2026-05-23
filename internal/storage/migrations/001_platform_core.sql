CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  checksum TEXT NOT NULL,
  applied_at TEXT NOT NULL
);

CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  lifecycle_status TEXT NOT NULL CHECK (lifecycle_status IN ('concept', 'spec_ready', 'roadmap_ready', 'implementing', 'blocked', 'complete')),
  archive_status TEXT NOT NULL CHECK (archive_status IN ('active', 'archived')),
  primary_environment_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE execution_environments (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  os_family TEXT NOT NULL CHECK (os_family IN ('windows', 'wsl', 'linux', 'macos', 'remote_windows', 'remote_linux')),
  role TEXT NOT NULL CHECK (role IN ('primary', 'sidecar', 'remote', 'disabled')),
  shell TEXT NOT NULL CHECK (shell IN ('powershell', 'cmd', 'bash', 'sh', 'none')),
  project_root TEXT NOT NULL,
  worktree_root TEXT,
  git_provider TEXT NOT NULL CHECK (git_provider IN ('git-for-windows', 'linux-git', 'none')),
  codex_adapter TEXT NOT NULL CHECK (codex_adapter IN ('codex-windows', 'codex-wsl', 'codex-linux', 'none')),
  sandbox_profile TEXT NOT NULL CHECK (sandbox_profile IN ('windows-native', 'linux-bubblewrap', 'external-isolated', 'none')),
  status TEXT NOT NULL CHECK (status IN ('detected', 'configured', 'checking', 'ready', 'missing', 'invalid', 'disabled')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_execution_environments_one_primary
ON execution_environments(project_id)
WHERE role = 'primary';

CREATE TABLE project_run_profiles (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('single_environment', 'windows_primary', 'wsl_primary', 'hybrid')),
  status TEXT NOT NULL CHECK (status IN ('draft', 'active', 'invalid', 'disabled')),
  primary_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  implementation_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  git_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  merge_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  required_verification_environment_ids_json TEXT NOT NULL,
  optional_verification_environment_ids_json TEXT NOT NULL,
  canonical_operations_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, name)
);

CREATE TABLE path_mappings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  from_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  to_environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  from_root TEXT NOT NULL,
  to_root TEXT NOT NULL,
  mapping_mode TEXT NOT NULL CHECK (mapping_mode IN ('same_filesystem', 'isolated_worktree', 'mirrored_clone', 'unsupported')),
  write_owner_environment_id TEXT REFERENCES execution_environments(id),
  status TEXT NOT NULL CHECK (status IN ('active', 'invalid', 'disabled')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(project_id, from_environment_id, to_environment_id, from_root, to_root)
);

CREATE TABLE target_platforms (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  os_family TEXT NOT NULL CHECK (os_family IN ('windows', 'wsl', 'linux', 'macos', 'remote_windows', 'remote_linux')),
  app_type TEXT NOT NULL,
  framework TEXT NOT NULL,
  packaging TEXT,
  required_environment_id TEXT REFERENCES execution_environments(id),
  canonical_verification_environment_id TEXT REFERENCES execution_environments(id),
  status TEXT NOT NULL CHECK (status IN ('draft', 'active', 'unsupported', 'disabled')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE toolchain_requirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  environment_id TEXT REFERENCES execution_environments(id),
  target_platform_id TEXT REFERENCES target_platforms(id),
  toolchain_key TEXT NOT NULL,
  required_for TEXT NOT NULL CHECK (required_for IN ('implementation', 'verification', 'runtime', 'runtime_smoke', 'deployment')),
  required_for_merge INTEGER NOT NULL CHECK (required_for_merge IN (0, 1)),
  status TEXT NOT NULL CHECK (status IN ('detected', 'missing', 'invalid', 'setup_required', 'waived', 'unsupported', 'revoked')),
  detected_version TEXT,
  required_version TEXT,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE change_requests (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  status TEXT NOT NULL CHECK (status IN ('proposed', 'impact_analyzed', 'approved', 'applying', 'applied', 'needs_decision', 'rejected', 'cancelled', 'failed')),
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE feature_requests (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  change_request_id TEXT REFERENCES change_requests(id),
  status TEXT NOT NULL CHECK (status IN ('queued', 'analyzing', 'planned', 'running', 'waiting_for_human', 'completed', 'cancelled', 'superseded')),
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_groups (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  feature_request_id TEXT REFERENCES feature_requests(id),
  status TEXT NOT NULL CHECK (status IN ('proposed', 'ready', 'running', 'waiting_for_human', 'completed', 'cancelled')),
  title TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  artifact_type TEXT NOT NULL CHECK (artifact_type IN ('prd', 'architecture', 'roadmap', 'task_yaml', 'policy', 'memory', 'schema', 'other')),
  status TEXT NOT NULL CHECK (status IN ('draft', 'proposed', 'approved', 'approved_with_notes', 'rejected', 'superseded')),
  approved_version_id TEXT,
  latest_version_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE artifact_versions (
  id TEXT PRIMARY KEY,
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE RESTRICT,
  version INTEGER NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('draft', 'proposed', 'approved', 'approved_with_notes', 'rejected', 'superseded')),
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  reviewed_by TEXT,
  reviewed_at TEXT,
  approval_notes TEXT,
  rejected_reason TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(artifact_id, version)
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_group_id TEXT REFERENCES task_groups(id),
  artifact_version_id TEXT REFERENCES artifact_versions(id),
  status TEXT NOT NULL CHECK (status IN ('proposed', 'ready', 'implementing', 'verifying', 'diagnosing', 'repairing', 'reviewing', 'needs_input', 'needs_decision', 'blocked_on_environment', 'blocked_on_policy', 'ready_for_human_review', 'approved_for_merge', 'queued_for_merge', 'rebasing', 'reverifying', 'merge_conflict', 'patch_exported', 'manually_applied', 'merged', 'applied', 'failed', 'cancelled')),
  title TEXT NOT NULL,
  current_run_id TEXT,
  base_branch TEXT NOT NULL,
  head_branch TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_dependencies (
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
  depends_on_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
  dependency_type TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(task_id, depends_on_task_id)
);

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
  run_type TEXT NOT NULL CHECK (run_type IN ('implementation', 'repair', 'verification', 'review', 'replan', 'rebase', 'reverify', 'merge', 'patch_export')),
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

CREATE TABLE run_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
  command_event_id TEXT,
  artifact_type TEXT NOT NULL CHECK (artifact_type IN ('prompt', 'events_jsonl', 'final_message', 'diff', 'verification_summary', 'gate_result', 'review', 'summary', 'secret_scan', 'command_stdout', 'command_stderr', 'command_result')),
  artifact_key TEXT NOT NULL,
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  redaction_status TEXT NOT NULL CHECK (redaction_status IN ('not_needed', 'redacted', 'failed')),
  created_at TEXT NOT NULL,
  UNIQUE(run_id, artifact_type, artifact_key)
);

CREATE TABLE command_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
  environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  command_kind TEXT NOT NULL CHECK (command_kind IN ('codex', 'verification', 'reverify', 'git', 'merge', 'patch', 'doctor', 'toolchain', 'cleanup', 'other')),
  runner TEXT NOT NULL CHECK (runner IN ('powershell', 'cmd', 'bash', 'sh', 'direct', 'fake', 'wsl-bridge')),
  cwd TEXT NOT NULL,
  argv_json TEXT NOT NULL,
  shell_invocation INTEGER NOT NULL CHECK (shell_invocation IN (0, 1)),
  network_policy TEXT NOT NULL CHECK (network_policy IN ('off', 'allowlisted', 'package_registry_only', 'unrestricted')),
  exit_code INTEGER,
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'timed_out', 'blocked', 'cancelled')),
  stdout_artifact_id TEXT,
  stderr_artifact_id TEXT,
  detected_risks_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT
);

CREATE TABLE workflow_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id),
  run_id TEXT REFERENCES runs(id),
  event_type TEXT NOT NULL,
  from_status TEXT,
  to_status TEXT,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE verification_results (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
  environment_id TEXT NOT NULL REFERENCES execution_environments(id),
  command_event_id TEXT REFERENCES command_events(id),
  command_id TEXT NOT NULL,
  required_for_merge INTEGER NOT NULL CHECK (required_for_merge IN (0, 1)),
  status TEXT NOT NULL CHECK (status IN ('passed', 'failed', 'skipped', 'error')),
  failure_class TEXT CHECK (failure_class IS NULL OR failure_class IN ('current_diff', 'environment', 'baseline', 'spec_gap', 'unknown')),
  summary_artifact_id TEXT REFERENCES run_artifacts(id),
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE gate_results (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id),
  run_id TEXT REFERENCES runs(id),
  status TEXT NOT NULL CHECK (status IN ('PASS', 'AUTO_REPAIR', 'AUTO_REPLAN', 'REPORT_ONLY', 'HUMAN_INPUT', 'HUMAN_DECISION', 'HARD_BLOCK')),
  severity TEXT NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
  detector TEXT NOT NULL,
  human_action_type TEXT CHECK (human_action_type IS NULL OR human_action_type IN ('input', 'decision', 'review', 'policy_approval')),
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id),
  status TEXT NOT NULL CHECK (status IN ('open', 'approved', 'rejected', 'revised', 'superseded')),
  title TEXT NOT NULL,
  options_json TEXT NOT NULL,
  selected_option TEXT,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE human_approvals (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id),
  approval_type TEXT NOT NULL CHECK (approval_type IN ('final_review', 'merge', 'manual_apply', 'artifact')),
  status TEXT NOT NULL CHECK (status IN ('open', 'approved', 'rejected', 'revised', 'cancelled', 'revoked')),
  evidence_json TEXT NOT NULL,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE inbox_items (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_id TEXT REFERENCES tasks(id),
  item_type TEXT NOT NULL CHECK (item_type IN ('human_input', 'human_decision', 'approval', 'toolchain_setup', 'platform_setup', 'path_mapping_issue', 'runner_capability_issue', 'hard_block', 'report')),
  status TEXT NOT NULL CHECK (status IN ('open', 'snoozed', 'resolved', 'dismissed')),
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  batch_key TEXT,
  priority INTEGER NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT,
  UNIQUE(project_id, dedupe_key, status)
);

CREATE TABLE trace_links (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  from_type TEXT NOT NULL,
  from_id TEXT NOT NULL,
  to_type TEXT NOT NULL,
  to_id TEXT NOT NULL,
  relation TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_tasks_project_status ON tasks(project_id, status);
CREATE INDEX idx_runs_task_status ON runs(task_id, status);
CREATE INDEX idx_command_events_run_environment ON command_events(run_id, environment_id);
CREATE INDEX idx_verification_results_run_environment ON verification_results(run_id, environment_id);
CREATE INDEX idx_gate_results_task_status ON gate_results(task_id, status);
CREATE INDEX idx_inbox_items_project_status ON inbox_items(project_id, status);
