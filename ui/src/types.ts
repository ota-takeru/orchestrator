export type SnapshotCounts = {
  open_inbox_items: number;
  running_tasks: number;
  waiting_for_human_tasks: number;
  blocked_tasks: number;
  queued_requests: number;
  open_decisions: number;
  running_workers: number;
  open_merge_queue: number;
  baseline_issues: number;
};

export type InboxItem = {
  id: string;
  task_id?: string;
  item_type: string;
  status: string;
  source_type: string;
  source_id?: string;
  priority: number;
  title: string;
  body: string;
  created_at: string;
  updated_at: string;
};

export type HumanInboxSnapshot = {
  project_id: string;
  generated_at: string;
  counts: SnapshotCounts;
  last_successful_merge_at?: string;
  open_inbox_items: InboxItem[];
  recommended_next_commands?: string[];
};

export type Decision = {
  id: string;
  task_id?: string;
  status: string;
  title: string;
  options?: DecisionOption[];
  created_at: string;
  updated_at: string;
};

export type DecisionOption = {
  id: string;
  label: string;
  description?: string;
};

export type MemoryRecord = {
  id: string;
  memory_type: string;
  key: string;
  value: string;
  scope: string;
  scope_id?: string;
  source_type: string;
  source_id?: string;
  created_at: string;
  updated_at: string;
};

export type TrustedArtifact = {
  artifact_id: string;
  artifact_type: string;
  version_id: string;
  version: number;
  status: string;
  path: string;
  content_hash: string;
  approval_notes?: string;
  reviewed_at?: string;
  content: string;
};

export type PathMapping = {
  id: string;
  from_environment_id: string;
  to_environment_id: string;
  from_root: string;
  to_root: string;
  mapping_mode: string;
  write_owner_environment_id?: string;
  status: string;
};

export type ToolchainSetupCard = {
  inbox_id: string;
  requirement_id: string;
  environment_id: string;
  os_family: string;
  toolchain_key: string;
  required_for: string;
  required_for_merge: boolean;
  status: string;
  message: string;
  instructions: string[];
  rerun_command: string;
};

export type MergeQueueEntry = {
  id: string;
  task_id: string;
  status: string;
  base_commit: string;
  head_commit: string;
};

export type MergeGateStatus = {
  queue: MergeQueueEntry[];
  blockers?: string[];
  blocking_inbox_items?: InboxItem[];
  ready: boolean;
};

export type InvariantViolation = {
  scope: string;
  id: string;
  code: string;
  message: string;
};

export type DashboardData = {
  snapshot: HumanInboxSnapshot;
  decisions: Decision[];
  baselineIssues: MemoryRecord[];
  trustedArtifacts: TrustedArtifact[];
  pathMappings: PathMapping[];
  toolchainSetupCards: ToolchainSetupCard[];
  mergeStatus: MergeGateStatus;
  projectViolations: InvariantViolation[];
};
