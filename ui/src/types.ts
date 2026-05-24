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

export type DashboardData = {
  snapshot: HumanInboxSnapshot;
  decisions: Decision[];
  baselineIssues: MemoryRecord[];
};
