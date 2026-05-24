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

export type TaskRecord = {
  id: string;
  status: string;
  title: string;
};

export type FeatureRequest = {
  id: string;
  status: string;
  title: string;
  description: string;
  source: string;
  priority: string;
  created_at: string;
  updated_at: string;
};

export type WorkQueueItem = {
  id: string;
  lane: string;
  item_type: string;
  item_id: string;
  status: string;
  priority: string;
  attempt_no: number;
  max_attempts: number;
  blocked_reason?: string;
  updated_at: string;
};

export type PlanningRun = {
  id: string;
  run_type: string;
  status: string;
  output_summary?: string;
  updated_at: string;
};

export type PlanningArtifact = {
  id: string;
  artifact_type: string;
  status: string;
  path: string;
  updated_at: string;
};

export type WorkerRun = {
  id: string;
  lane: string;
  mode: string;
  status: string;
  stop_reason?: string;
  started_at: string;
  finished_at?: string;
};

export type WorkStatus = {
  worker_runs: WorkerRun[];
  planning: {
    runs: PlanningRun[];
    artifacts: PlanningArtifact[];
    queue: WorkQueueItem[];
  };
};

export type PlanningStatus = {
  runs: PlanningRun[];
  artifacts: PlanningArtifact[];
  queue: WorkQueueItem[];
};

export type ChangeRequest = {
  id: string;
  status: string;
  body: string;
  created_at: string;
  updated_at: string;
};

export type DependencyRisk = {
  id: string;
  name: string;
  package_manager: string;
  dependency_type: string;
  reason: string;
  risk: string;
  approved_scope: string;
  lockfile_changed: boolean;
  created_at: string;
};

export type DashboardData = {
  snapshot: HumanInboxSnapshot;
  tasks: TaskRecord[];
  featureRequests: FeatureRequest[];
  queueItems: WorkQueueItem[];
  workStatus: WorkStatus;
  planningStatus: PlanningStatus;
  changeRequests: ChangeRequest[];
  dependencyRisks: DependencyRisk[];
  decisions: Decision[];
  baselineIssues: MemoryRecord[];
  trustedArtifacts: TrustedArtifact[];
  pathMappings: PathMapping[];
  toolchainSetupCards: ToolchainSetupCard[];
  mergeStatus: MergeGateStatus;
  projectViolations: InvariantViolation[];
};

export type RegisteredProject = {
  id: string;
  display_name: string;
  authority_runtime: "windows" | "wsl";
  primary_environment_id: string;
  project_root: string;
  data_root?: string;
  windows_display_root?: string;
  wsl_distro?: string;
  wsl_project_root?: string;
  status: "active" | "missing" | "invalid" | "disabled";
  last_seen_at?: string;
  created_at: string;
  updated_at: string;
};
