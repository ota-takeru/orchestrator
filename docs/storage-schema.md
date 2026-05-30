# Storage Schema

この文書はSQLite実装時のDDL、CHECK、FK、UNIQUE、index、path/JSON contractの正です。概念モデルは [data-model.md](data-model.md)、状態遷移は [state-machine.md](state-machine.md)、状態不変条件は [state-invariants.md](state-invariants.md) を参照します。

## Database Settings

SQLite connectionを開いた直後に必ず設定します。

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
```

`PRAGMA foreign_keys` はconnectionごとの設定なので、connection poolを使う場合は各connectionで有効化します。repository testsでは、FK違反が実際に失敗することを確認します。

## Migration Table

初期実装からmigration管理を入れます。schemaを直接作り直すテスト専用pathは持ってよいですが、本番pathはmigration経由だけにします。

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  checksum TEXT NOT NULL,
  applied_at TEXT NOT NULL
);
```

ルール:

- migrationはversion単調増加。
- 適用済みversionのchecksumが変わったら起動失敗。
- migration applyはidempotentではなく、適用済みversionをskipすることで再実行可能にする。
- migrationは1 file 1 transactionを原則にする。

## Migration Slices

Initial Complete Scope全体のtableを最初のmigrationで一括作成してはいけません。ただしPlatform supportはGit/worktree/verificationより前に必要なfoundationなので、初期core sliceに含めます。実装は次のmigration sliceで進めます。

Migration 001 platform + core:

- `projects`
- `execution_environments`
- `project_run_profiles`
- `path_mappings`
- `target_platforms`
- `toolchain_requirements`
- `change_requests`
- `feature_requests`
- `task_groups`
- `artifacts`
- `artifact_versions`
- `tasks`
- `task_dependencies`
- `runs`
- `run_artifacts`
- `command_events`
- `workflow_events`
- `verification_results`
- `gate_results`
- `decisions`
- `human_approvals`
- `inbox_items`
- `trace_links`

Migration 002 merge:

- `merge_queue_entries`
- `patch_applications`
- `semantic_behavior_diffs`

Migration 003 environment:

- `environment_requirements`
- `environment_bindings`
- `environment_audit_events`

Migration 004 planning:

- `planning_runs`
- `planning_artifacts`
- `decision_report_drafts`
- `work_queue_items`
- `worker_runs`

Migration 005 policy / memory / dependency:

- `policies`
- `memories`
- `dependency_risk_ledger`

Migration 015 understanding-first intake:

- `intent_items`
- `understanding_snapshots`
- `proposal_batches`
- `proposal_deltas`
- `approval_packets`

このsliceはDB schema変更を伴う正規スコープです。新しい本番依存パッケージは追加しません。

Migration 001内では `change_requests`、`feature_requests`、`task_groups` を `tasks` より前に作成します。`feature_requests.change_request_id -> change_requests(id)` と `tasks.task_group_id -> task_groups(id)` の親tableが存在しない状態でINSERTする危険を避けるため、planning workerの実装は後続migrationでも、FK親になるtableはcore migrationへ前倒しします。`task_dependencies` は `tasks` 作成後に作成します。

## Core CHECK Values

statusやtypeはTEXTで保存しますが、SQLite CHECKで正規値を制限します。状態遷移上の正規一覧は [state-machine.md](state-machine.md) を正とし、DDLのCHECKとGo側のenum validationは同じ値を使います。

ProjectLifecycleStatus:

```sql
lifecycle_status TEXT NOT NULL CHECK (
  lifecycle_status IN ('concept', 'spec_ready', 'roadmap_ready', 'implementing', 'blocked', 'complete')
)
```

ProjectArchiveStatus:

```sql
archive_status TEXT NOT NULL CHECK (
  archive_status IN ('active', 'archived')
)
```

PlatformMode:

```sql
platform_mode TEXT NOT NULL CHECK (
  platform_mode IN ('single_environment', 'windows_primary', 'wsl_primary', 'hybrid')
)
```

OSFamily:

```sql
os_family TEXT NOT NULL CHECK (
  os_family IN ('windows', 'wsl', 'linux', 'macos', 'remote_windows', 'remote_linux')
)
```

ExecutionEnvironmentRole:

```sql
role TEXT NOT NULL CHECK (
  role IN ('primary', 'sidecar', 'remote', 'disabled')
)
```

Shell:

```sql
shell TEXT NOT NULL CHECK (
  shell IN ('powershell', 'cmd', 'bash', 'sh', 'none')
)
```

GitProvider:

```sql
git_provider TEXT NOT NULL CHECK (
  git_provider IN ('git-for-windows', 'linux-git', 'none')
)
```

CodexAdapter:

```sql
codex_adapter TEXT NOT NULL CHECK (
  codex_adapter IN ('codex-windows', 'codex-wsl', 'codex-linux', 'none')
)
```

SandboxProfile:

```sql
sandbox_profile TEXT NOT NULL CHECK (
  sandbox_profile IN ('windows-native', 'linux-bubblewrap', 'external-isolated', 'none')
)
```

ExecutionEnvironmentStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('detected', 'configured', 'checking', 'ready', 'missing', 'invalid', 'disabled')
)
```

RunProfileMode:

```sql
mode TEXT NOT NULL CHECK (
  mode IN ('single_environment', 'windows_primary', 'wsl_primary', 'hybrid')
)
```

RunProfileStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('draft', 'active', 'invalid', 'disabled')
)
```

MappingMode:

```sql
mapping_mode TEXT NOT NULL CHECK (
  mapping_mode IN ('same_filesystem', 'isolated_worktree', 'mirrored_clone', 'unsupported')
)
```

PathMappingStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('active', 'invalid', 'disabled')
)
```

TargetPlatformStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('draft', 'active', 'unsupported', 'disabled')
)
```

ToolchainRequirementStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('detected', 'missing', 'invalid', 'setup_required', 'waived', 'unsupported', 'revoked')
)
```

CommandEventStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('pending', 'running', 'succeeded', 'failed', 'timed_out', 'blocked', 'cancelled')
)
```

CommandKind:

```sql
command_kind TEXT NOT NULL CHECK (
  command_kind IN ('codex', 'verification', 'reverify', 'git', 'merge', 'patch', 'doctor', 'toolchain', 'cleanup', 'other')
)
```

CommandRunner:

```sql
runner TEXT NOT NULL CHECK (
  runner IN ('powershell', 'cmd', 'bash', 'sh', 'direct', 'fake', 'wsl-bridge')
)
```

NetworkPolicy:

```sql
network_policy TEXT NOT NULL CHECK (
  network_policy IN ('off', 'allowlisted', 'package_registry_only', 'unrestricted')
)
```

ArtifactStatus / ArtifactVersionStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('draft', 'proposed', 'approved', 'approved_with_notes', 'rejected', 'superseded')
)
```

ArtifactType:

```sql
artifact_type TEXT NOT NULL CHECK (
  artifact_type IN ('prd', 'architecture', 'roadmap', 'task_yaml', 'policy', 'memory', 'schema', 'other')
)
```

TaskStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN (
    'proposed',
    'ready',
    'implementing',
    'verifying',
    'diagnosing',
    'repairing',
    'reviewing',
    'needs_input',
    'needs_decision',
    'blocked_on_environment',
    'blocked_on_policy',
    'ready_for_human_review',
    'approved_for_merge',
    'queued_for_merge',
    'rebasing',
    'reverifying',
    'merge_conflict',
    'patch_exported',
    'manually_applied',
    'merged',
    'applied',
    'failed',
    'cancelled'
  )
)
```

RunStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out', 'blocked')
)
```

Run / CommandEvent timestamp rule:

`runs.started_at`、`runs.completed_at`、`command_events.started_at`、`command_events.completed_at` はnullableです。pending record作成時点ではprocessが開始していないため、started_atを持ってはいけません。一方でrecord作成時刻と更新時刻は常に必要なので、`runs.created_at` / `runs.updated_at` と `command_events.created_at` / `command_events.updated_at` はNOT NULLにします。SQLite CHECKで複雑に表現せず、state transition serviceで次を検証します。

初期schemaでは、query/evidence対象の作成時刻として `verification_results.created_at` もNOT NULLにします。mutableな長寿命recordは `environment_requirements.updated_at`、`change_requests.updated_at`、`worker_runs.updated_at`、`merge_queue_entries.updated_at` を持ちます。

```text
status = pending:
  started_at must be NULL

status = running:
  started_at must be NOT NULL
  completed_at must be NULL

terminal status:
  started_at must be NOT NULL
  completed_at must be NOT NULL
```

RunType:

```sql
run_type TEXT NOT NULL CHECK (
  run_type IN ('implementation', 'repair', 'verification', 'review', 'replan', 'rebase', 'reverify', 'merge', 'patch_export', 'cleanup', 'worktree_safety')
)
```

Manual applyの検証runも `reverify` を使います。merge queue由来は `run_type = 'reverify'` かつ `reverify_context_type = 'merge_queue_entry'`、manual apply由来は `run_type = 'reverify'` かつ `reverify_context_type = 'patch_application'` として保存します。manual apply専用の別run typeは定義しません。

cleanup dry-runとworktree削除前安全確認は証拠保存対象です。`cleanup` は `devos cleanup --dry-run` のplan、対象task、blocker、削除しない理由を保存します。`worktree_safety` は実削除またはworktree操作前の未保存diff、untracked files、path ownership、artifact保存済み確認を保存するためのrun typeです。

RunArtifactType:

```sql
artifact_type TEXT NOT NULL CHECK (
  artifact_type IN (
    'prompt',
    'events_jsonl',
    'final_message',
    'diff',
    'verification_summary',
    'gate_result',
    'review',
    'summary',
    'secret_scan',
    'command_stdout',
    'command_stderr',
    'command_result'
  )
)
```

RunArtifactRedactionStatus:

```sql
redaction_status TEXT NOT NULL CHECK (
  redaction_status IN ('not_needed', 'redacted', 'failed')
)
```

VerificationResultStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('passed', 'failed', 'skipped', 'error')
)
```

FailureClass:

```sql
failure_class TEXT CHECK (
  failure_class IS NULL OR failure_class IN ('current_diff', 'environment', 'baseline', 'spec_gap', 'unknown')
)
```

GateResult:

```sql
status TEXT NOT NULL CHECK (
  status IN ('PASS', 'AUTO_REPAIR', 'AUTO_REPLAN', 'REPORT_ONLY', 'HUMAN_INPUT', 'HUMAN_DECISION', 'HARD_BLOCK')
)
```

GateResultSeverity:

```sql
severity TEXT NOT NULL CHECK (
  severity IN ('low', 'medium', 'high', 'critical')
)
```

GateHumanActionType:

```sql
human_action_type TEXT CHECK (
  human_action_type IS NULL OR human_action_type IN ('input', 'decision', 'review', 'policy_approval')
)
```

ToolchainRequirementRequiredFor:

```sql
required_for TEXT NOT NULL CHECK (
  required_for IN ('implementation', 'verification', 'runtime', 'runtime_smoke', 'deployment')
)
```

Work queue lane:

```sql
lane TEXT NOT NULL CHECK (
  lane IN ('planning', 'consolidation', 'execution', 'merge')
)
```

WorkQueueItemStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('queued', 'leased', 'running', 'heartbeat_lost', 'waiting_for_human', 'blocked', 'completed', 'failed', 'cancelled')
)
```

WorkQueueItemType:

```sql
item_type TEXT NOT NULL CHECK (
  item_type IN (
    'planning_run',
    'planning_consolidation',
    'canonical_commit',
    'feature_request_analysis',
    'change_request_analysis',
    'task_implementation',
    'task_repair',
    'task_review',
    'merge_queue_processing',
    'environment_rerun'
  )
)
```

FeatureRequestStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('queued', 'analyzing', 'planned', 'running', 'waiting_for_human', 'completed', 'cancelled', 'superseded')
)
```

PlanningRunStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'stale')
)
```

PlanningArtifactStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('draft', 'proposed', 'accepted', 'rejected', 'superseded', 'stale')
)
```

DecisionReportDraftStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('draft', 'batched', 'promoted', 'rejected', 'superseded', 'stale')
)
```

TaskGroupStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('proposed', 'ready', 'running', 'waiting_for_human', 'completed', 'cancelled')
)
```

WorkerRunStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('running', 'paused', 'stopped', 'failed', 'heartbeat_lost')
)
```

DecisionStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('open', 'approved', 'rejected', 'revised', 'superseded')
)
```

DecisionType:

```sql
decision_type TEXT NOT NULL CHECK (
  decision_type IN ('dependency', 'architecture', 'db_schema', 'auth', 'external_api', 'ux', 'policy', 'scope', 'privacy')
)
```

HumanApprovalStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('open', 'approved', 'rejected', 'revised', 'cancelled', 'revoked')
)
```

HumanApprovalType:

```sql
approval_type TEXT NOT NULL CHECK (
  approval_type IN ('final_review', 'merge', 'manual_apply')
)
```

PatchApplicationStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('exported', 'manually_applied', 'verifying', 'verified', 'needs_decision', 'failed', 'cancelled')
)
```

ChangeRequestStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('proposed', 'impact_analyzed', 'approved', 'applying', 'needs_decision', 'rejected', 'applied', 'failed', 'cancelled')
)
```

MergeQueueStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict', 'merged', 'cancelled')
)
```

InboxItemStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('open', 'snoozed', 'resolved', 'dismissed')
)
```

InboxItemType:

```sql
item_type TEXT NOT NULL CHECK (
  item_type IN ('human_decision', 'human_input', 'approval', 'report', 'hard_block', 'change_request', 'merge_conflict', 'platform_setup', 'toolchain_setup')
)
```

InboxSourceType:

```sql
source_type TEXT NOT NULL CHECK (
  source_type IN ('decision', 'human_approval', 'approval_packet', 'environment_requirement', 'environment_binding', 'gate_result', 'verification_result', 'change_request', 'merge_conflict', 'dependency', 'patch_application', 'execution_environment', 'path_mapping', 'toolchain_requirement', 'run_profile')
)
```

Understanding / Approval Packet values:

```sql
risk_level TEXT NOT NULL CHECK (risk_level IN ('L0', 'L1', 'L2', 'L3', 'L4'));
recommended_go_mode TEXT NOT NULL CHECK (
  recommended_go_mode IN ('no_gate', 'implement_with_assumptions', 'approval_before_implementation', 'approval_before_canonical_artifact_update', 'hard_gate')
);
approval_packets.status TEXT NOT NULL CHECK (
  status IN ('open', 'approved', 'approved_with_notes', 'rejected', 'superseded', 'cancelled')
);
```

`approval_packets.source_type/source_id` は `initial_concept`、`feature_request`、`change_request`、`planning_consolidation` を指すpolymorphic referenceです。`source_type='approval_packet'` の `inbox_items` は表示projectionであり、source of truthは `approval_packets` です。

EnvironmentRequirementStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('missing', 'requested', 'configured', 'invalid', 'waived', 'cancelled', 'revoked')
)
```

EnvironmentBindingStatus:

```sql
status TEXT NOT NULL CHECK (
  status IN ('configured', 'missing', 'invalid', 'revoked')
)
```

Priority:

```sql
priority TEXT NOT NULL CHECK (
  priority IN ('critical', 'high', 'medium', 'low')
)
```

RiskLevel:

```sql
risk_level TEXT NOT NULL CHECK (
  risk_level IN ('critical', 'high', 'medium', 'low')
)
```

MemoryType:

```sql
memory_type TEXT NOT NULL CHECK (
  memory_type IN ('policy', 'preference', 'implementation_note', 'baseline_issue')
)
```

Policy / Memory scope:

```sql
scope TEXT NOT NULL CHECK (
  scope IN ('project', 'task', 'dependency_family', 'one_time', 'user_default')
)
```

EnvironmentBindingScope:

```sql
scope TEXT NOT NULL CHECK (
  scope IN ('project', 'task', 'run', 'user_default')
)
```

Policy / Memory scopeとEnvironmentBinding scopeは別enumです。同じCHECKを共有してはいけません。

TaskDependencyType:

```sql
dependency_type TEXT NOT NULL CHECK (
  dependency_type IN ('blocks_execution', 'blocks_merge', 'ordering_only')
)
```

LedgerDependencyType:

```sql
dependency_type TEXT NOT NULL CHECK (
  dependency_type IN ('production', 'development', 'tool')
)
```

## CHECK Matrix

主要enumは次のmatrixで同期します。SQLite CHECKが難しいpolymorphic columnやJSON内IDはGo validationで強制します。

| Table.Column | Allowed values | Source document | Enforced by |
| --- | --- | --- | --- |
| `projects.platform_mode` | `single_environment`, `windows_primary`, `wsl_primary`, `hybrid` | `platform-model.md` | SQLite CHECK |
| `execution_environments.os_family` | `windows`, `wsl`, `linux`, `macos`, `remote_windows`, `remote_linux` | `platform-model.md` | SQLite CHECK |
| `execution_environments.role` | `primary`, `sidecar`, `remote`, `disabled` | `platform-model.md` | SQLite CHECK |
| `execution_environments.shell` | `powershell`, `cmd`, `bash`, `sh`, `none` | `platform-model.md` | SQLite CHECK |
| `execution_environments.git_provider` | `git-for-windows`, `linux-git`, `none` | `platform-model.md` | SQLite CHECK |
| `execution_environments.codex_adapter` | `codex-windows`, `codex-wsl`, `codex-linux`, `none` | `platform-model.md` | SQLite CHECK |
| `execution_environments.sandbox_profile` | `windows-native`, `linux-bubblewrap`, `external-isolated`, `none` | `platform-model.md` | SQLite CHECK |
| `execution_environments.status` | `detected`, `configured`, `checking`, `ready`, `missing`, `invalid`, `disabled` | `state-machine.md` | SQLite CHECK + state service |
| `project_run_profiles.mode` | `single_environment`, `windows_primary`, `wsl_primary`, `hybrid` | `platform-model.md` | SQLite CHECK |
| `project_run_profiles.status` | `draft`, `active`, `invalid`, `disabled` | `state-machine.md` | SQLite CHECK + state service |
| `path_mappings.mapping_mode` | `same_filesystem`, `isolated_worktree`, `mirrored_clone`, `unsupported` | `path-mapping.md` | SQLite CHECK |
| `path_mappings.status` | `active`, `invalid`, `disabled` | `state-machine.md` | SQLite CHECK + state service |
| `target_platforms.os_family` | `windows`, `wsl`, `linux`, `macos`, `remote_windows`, `remote_linux` | `platform-model.md` | SQLite CHECK |
| `target_platforms.status` | `draft`, `active`, `unsupported`, `disabled` | `state-machine.md` | SQLite CHECK + state service |
| `toolchain_requirements.required_for` | `implementation`, `verification`, `runtime`, `runtime_smoke`, `deployment` | `toolchain-requirements.md` | SQLite CHECK |
| `toolchain_requirements.status` | `detected`, `missing`, `invalid`, `setup_required`, `waived`, `unsupported`, `revoked` | `toolchain-requirements.md` | SQLite CHECK + state service |
| `tasks.planning_unit` | `feature_chunk`, `technical_subtask`, `migration`, `repair` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `tasks.status` | TaskStatus table values | `state-machine.md` | SQLite CHECK + state service |
| `tasks.verification_commands_json` | `VerificationCommand[]`; environment ids must resolve | `runner-protocol.md` | Go validation + JSON Schema |
| `task_dependencies.dependency_type` | `blocks_execution`, `blocks_merge`, `ordering_only` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `runs.run_type` | `implementation`, `repair`, `verification`, `review`, `replan`, `rebase`, `reverify`, `merge`, `patch_export`, `cleanup`, `worktree_safety` | `storage-schema.md` | SQLite CHECK |
| `runs.status` | `pending`, `running`, `succeeded`, `failed`, `cancelled`, `timed_out`, `blocked` | `state-machine.md` | SQLite CHECK + state service |
| `run_artifacts.artifact_type` | `prompt`, `events_jsonl`, `final_message`, `diff`, `verification_summary`, `gate_result`, `review`, `summary`, `secret_scan`, `command_stdout`, `command_stderr`, `command_result` | `runner-protocol.md` | SQLite CHECK |
| `command_events.command_kind` | `codex`, `verification`, `reverify`, `git`, `merge`, `patch`, `doctor`, `toolchain`, `cleanup`, `other` | `runner-protocol.md` | SQLite CHECK |
| `command_events.runner` | `powershell`, `cmd`, `bash`, `sh`, `direct`, `fake`, `wsl-bridge` | `runner-protocol.md` | SQLite CHECK |
| `command_events.network_policy` | `off`, `allowlisted`, `package_registry_only`, `unrestricted` | `runner-protocol.md` | SQLite CHECK |
| `command_events.status` | `pending`, `running`, `succeeded`, `failed`, `timed_out`, `blocked`, `cancelled` | `state-machine.md` | SQLite CHECK + state service |
| `verification_results.status` | `passed`, `failed`, `skipped`, `error` | `state-machine.md` | SQLite CHECK |
| `verification_results.failure_class` | `current_diff`, `environment`, `baseline`, `spec_gap`, `unknown` | `state-invariants.md` | SQLite CHECK |
| `gate_results.status` | `PASS`, `AUTO_REPAIR`, `AUTO_REPLAN`, `REPORT_ONLY`, `HUMAN_INPUT`, `HUMAN_DECISION`, `HARD_BLOCK` | `decision-gate.md` | SQLite CHECK |
| `gate_results.severity` | `low`, `medium`, `high`, `critical` | `decision-gate.md` | SQLite CHECK |
| `gate_results.human_action_type` | `input`, `decision`, `review`, `policy_approval` | `decision-gate.md` | SQLite CHECK |
| `feature_requests.source` | `human`, `change_request`, `system` | `data-model.md` | SQLite CHECK |
| `feature_requests.tier` | `minor_change`, `workflow_change`, `architecture_change` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `planning_runs.run_type` | `feature_detail`, `impact_analysis`, `decision_draft`, `task_group_proposal`, `risk_report`, `rolling_checkpoint` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `planning_artifacts.artifact_type` | `feature_detail_report`, `impact_analysis_report`, `task_group_proposal`, `risk_report`, `rolling_checkpoint_report` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `task_groups.planning_unit` | `feature_chunk`, `technical_subtasks`, `migration`, `repair` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `worker_runs.mode` | `bounded_parallel`, `sequential` | `task-planning-and-work-queue.md` | SQLite CHECK |
| `change_requests.type` | `product`, `architecture`, `ui`, `policy`, `priority`, `technical` | `change-requests.md` | SQLite CHECK |
| `environment_requirements.required_for` | `implementation`, `verification`, `runtime`, `runtime_smoke`, `deployment` | `environment-variables.md` | SQLite CHECK |
| `environment_requirements.source_hint` | `user_input`, `generated_example`, `external_secret` | `environment-variables.md` | SQLite CHECK |
| `environment_bindings.scope` | `project`, `task`, `run`, `user_default` | `environment-variables.md` | SQLite CHECK |
| `environment_bindings.storage` | `env_file`, `os_keychain`, `external_secret` | `environment-variables.md` | SQLite CHECK |
| `environment_bindings.created_by` | `human`, `policy` | `environment-variables.md` | SQLite CHECK |
| `environment_audit_events.action` | `requested`, `configured`, `updated`, `revoked`, `used`, `validation_failed` | `environment-variables.md` | SQLite CHECK |
| `environment_audit_events.actor` | `human`, `orchestrator`, `policy`, `system` | `environment-variables.md` | SQLite CHECK |
| `environment_audit_events.scope` | `project`, `task`, `run`, `user_default` | `environment-variables.md` | SQLite CHECK |
| `semantic_behavior_diffs.category` | `user_visible`, `non_user_visible`, `risk`, `test_change` | `data-model.md` | SQLite CHECK |
| `semantic_behavior_diffs.confidence` | `high`, `medium`, `low` | `data-model.md` | SQLite CHECK |
| `dependency_risk_ledger.package_manager` | `go`, `npm`, `pnpm`, `yarn`, `cargo`, `other` | `data-model.md` | SQLite CHECK |
| `dependency_risk_ledger.dependency_type` | `production`, `development`, `tool` | `data-model.md` | SQLite CHECK |
| `dependency_risk_ledger.risk` | `low`, `medium`, `high`, `critical` | `data-model.md` | SQLite CHECK |
| `dependency_risk_ledger.lifecycle_scripts` | `none_detected`, `detected`, `unknown` | `data-model.md` | SQLite CHECK |
| `dependency_risk_ledger.approved_scope` | `project`, `task`, `one_time`, `dependency_family` | `data-model.md` | SQLite CHECK |
| `memories.scope` | `project`, `task`, `dependency_family`, `one_time`, `user_default` | `data-model.md` | SQLite CHECK |

## Foreign Keys and Delete Policy

初期実装では、証拠を失わないため物理削除を最小化します。

| Table | FK | Delete Policy |
| --- | --- | --- |
| `tasks.project_id` | `projects(id)` | RESTRICT |
| `tasks.task_group_id` | `task_groups(id)` | SET NULL |
| `tasks.parent_task_id` | `tasks(id)` | SET NULL |
| `tasks.decomposition_origin_task_id` | `tasks(id)` | SET NULL |
| `tasks.current_run_id` | `runs(id)` | SET NULL |
| `execution_environments.project_id` | `projects(id)` | RESTRICT |
| `project_run_profiles.project_id` | `projects(id)` | RESTRICT |
| `project_run_profiles.implementation_environment_id` | `execution_environments(id)` | RESTRICT |
| `project_run_profiles.canonical_git_environment_id` | `execution_environments(id)` | RESTRICT |
| `project_run_profiles.canonical_merge_environment_id` | `execution_environments(id)` | RESTRICT |
| `path_mappings.project_id` | `projects(id)` | RESTRICT |
| `path_mappings.from_environment_id` | `execution_environments(id)` | RESTRICT |
| `path_mappings.to_environment_id` | `execution_environments(id)` | RESTRICT |
| `path_mappings.write_owner_environment_id` | `execution_environments(id)` | SET NULL |
| `target_platforms.project_id` | `projects(id)` | RESTRICT |
| `target_platforms.required_environment_id` | `execution_environments(id)` | SET NULL |
| `target_platforms.canonical_verification_environment_id` | `execution_environments(id)` | SET NULL |
| `toolchain_requirements.project_id` | `projects(id)` | RESTRICT |
| `toolchain_requirements.environment_id` | `execution_environments(id)` | RESTRICT |
| `runs.project_id` | `projects(id)` | RESTRICT |
| `runs.task_id` | `tasks(id)` | RESTRICT |
| `runs.repair_of_run_id` | `runs(id)` | SET NULL |
| `runs.implementation_environment_id` | `execution_environments(id)` | SET NULL |
| `runs.primary_verification_environment_id` | `execution_environments(id)` | SET NULL |
| `runs.run_profile_id` | `project_run_profiles(id)` | SET NULL |
| `runs.path_mapping_id` | `path_mappings(id)` | SET NULL |
| `run_artifacts.project_id` | `projects(id)` | RESTRICT |
| `run_artifacts.run_id` | `runs(id)` | RESTRICT |
| `run_artifacts.command_event_id` | `command_events(id)` | SET NULL |
| `command_events.project_id` | `projects(id)` | RESTRICT |
| `command_events.run_id` | `runs(id)` | RESTRICT |
| `command_events.environment_id` | `execution_environments(id)` | RESTRICT |
| `command_events.stdout_artifact_id` | `run_artifacts(id)` | SET NULL |
| `command_events.stderr_artifact_id` | `run_artifacts(id)` | SET NULL |
| `verification_results.run_id` | `runs(id)` | RESTRICT |
| `verification_results.task_id` | `tasks(id)` | RESTRICT |
| `verification_results.project_id` | `projects(id)` | RESTRICT |
| `verification_results.environment_id` | `execution_environments(id)` | RESTRICT |
| `verification_results.command_event_id` | `command_events(id)` | SET NULL |
| `gate_results.run_id` | `runs(id)` | RESTRICT |
| `gate_results.task_id` | `tasks(id)` | RESTRICT |
| `gate_results.project_id` | `projects(id)` | RESTRICT |
| `decisions.project_id` | `projects(id)` | RESTRICT |
| `decisions.task_id` | `tasks(id)` | SET NULL |
| `decisions.run_id` | `runs(id)` | SET NULL |
| `human_approvals.project_id` | `projects(id)` | RESTRICT |
| `human_approvals.task_id` | `tasks(id)` | RESTRICT |
| `human_approvals.run_id` | `runs(id)` | SET NULL |
| `artifacts.project_id` | `projects(id)` | RESTRICT |
| `artifacts.approved_version_id` | `artifact_versions(id)` | SET NULL |
| `artifacts.latest_version_id` | `artifact_versions(id)` | SET NULL |
| `artifact_versions.artifact_id` | `artifacts(id)` | RESTRICT |
| `artifact_versions.project_id` | `projects(id)` | RESTRICT |
| `merge_queue_entries.task_id` | `tasks(id)` | RESTRICT |
| `merge_queue_entries.project_id` | `projects(id)` | RESTRICT |
| `patch_applications.task_id` | `tasks(id)` | RESTRICT |
| `patch_applications.project_id` | `projects(id)` | RESTRICT |
| `patch_applications.export_run_id` | `runs(id)` | SET NULL |
| `patch_applications.verify_run_id` | `runs(id)` | SET NULL |
| `planning_runs.project_id` | `projects(id)` | RESTRICT |
| `planning_runs.feature_request_id` | `feature_requests(id)` | SET NULL |
| `feature_requests.project_id` | `projects(id)` | RESTRICT |
| `feature_requests.change_request_id` | `change_requests(id)` | SET NULL |
| `planning_artifacts.project_id` | `projects(id)` | RESTRICT |
| `planning_artifacts.planning_run_id` | `planning_runs(id)` | RESTRICT |
| `decision_report_drafts.project_id` | `projects(id)` | RESTRICT |
| `decision_report_drafts.planning_run_id` | `planning_runs(id)` | SET NULL |
| `task_groups.project_id` | `projects(id)` | RESTRICT |
| `task_groups.feature_request_id` | `feature_requests(id)` | SET NULL |
| `task_groups.change_request_id` | `change_requests(id)` | SET NULL |
| `task_dependencies.project_id` | `projects(id)` | RESTRICT |
| `task_dependencies.task_id` | `tasks(id)` | RESTRICT |
| `task_dependencies.depends_on_task_id` | `tasks(id)` | RESTRICT |
| `work_queue_items.project_id` | `projects(id)` | RESTRICT |
| `worker_runs.project_id` | `projects(id)` | RESTRICT |
| `change_requests.project_id` | `projects(id)` | RESTRICT |
| `environment_requirements.project_id` | `projects(id)` | RESTRICT |
| `environment_requirements.environment_id` | `execution_environments(id)` | SET NULL |
| `environment_bindings.project_id` | `projects(id)` | RESTRICT |
| `environment_bindings.environment_id` | `execution_environments(id)` | SET NULL |
| `environment_audit_events.project_id` | `projects(id)` | RESTRICT |
| `environment_audit_events.environment_id` | `execution_environments(id)` | SET NULL |
| `environment_audit_events.binding_id` | `environment_bindings(id)` | SET NULL |
| `environment_audit_events.requirement_id` | `environment_requirements(id)` | SET NULL |
| `environment_audit_events.run_id` | `runs(id)` | SET NULL |
| `environment_audit_events.command_event_id` | `command_events(id)` | SET NULL |
| `semantic_behavior_diffs.project_id` | `projects(id)` | RESTRICT |
| `semantic_behavior_diffs.task_id` | `tasks(id)` | RESTRICT |
| `semantic_behavior_diffs.run_id` | `runs(id)` | RESTRICT |
| `dependency_risk_ledger.project_id` | `projects(id)` | RESTRICT |
| `dependency_risk_ledger.introduced_by_task_id` | `tasks(id)` | SET NULL |
| `dependency_risk_ledger.introduced_by_run_id` | `runs(id)` | SET NULL |
| `dependency_risk_ledger.decision_id` | `decisions(id)` | SET NULL |
| `policies.project_id` | `projects(id)` | RESTRICT |
| `memories.project_id` | `projects(id)` | RESTRICT |
| `memories.invalidated_by_change_request_id` | `change_requests(id)` | SET NULL |

`inbox_items.source_type/source_id` と `trace_links.source_type/source_id` はpolymorphic referenceなので、SQLite FKだけでは表現しません。Go validationでsourceの存在、project scope、resolved同期を強制します。

Polymorphic reference mapping:

| Column | Type value | Target table |
| --- | --- | --- |
| `inbox_items.source_type/source_id` | `decision` | `decisions(id)` |
| `inbox_items.source_type/source_id` | `human_approval` | `human_approvals(id)` |
| `inbox_items.source_type/source_id` | `environment_requirement` | `environment_requirements(id)` |
| `inbox_items.source_type/source_id` | `environment_binding` | `environment_bindings(id)` |
| `inbox_items.source_type/source_id` | `gate_result` | `gate_results(id)` |
| `inbox_items.source_type/source_id` | `verification_result` | `verification_results(id)` |
| `inbox_items.source_type/source_id` | `change_request` | `change_requests(id)` |
| `inbox_items.source_type/source_id` | `dependency` | `dependency_risk_ledger(id)` |
| `inbox_items.source_type/source_id` | `patch_application` | `patch_applications(id)` |
| `inbox_items.source_type/source_id` | `execution_environment` | `execution_environments(id)` |
| `inbox_items.source_type/source_id` | `path_mapping` | `path_mappings(id)` |
| `inbox_items.source_type/source_id` | `toolchain_requirement` | `toolchain_requirements(id)` |
| `inbox_items.source_type/source_id` | `run_profile` | `project_run_profiles(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `artifact_version` | `artifact_versions(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `task` | `tasks(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `run` | `runs(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `run_artifact` | `run_artifacts(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `verification_result` | `verification_results(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `gate_result` | `gate_results(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `decision` | `decisions(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `human_approval` | `human_approvals(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `change_request` | `change_requests(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `execution_environment` | `execution_environments(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `path_mapping` | `path_mappings(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `toolchain_requirement` | `toolchain_requirements(id)` |
| `trace_links.source_type/source_id` / `target_type/target_id` | `run_profile` | `project_run_profiles(id)` |
| `work_queue_items.item_type/item_id` | `planning_run` | `planning_runs(id)` |
| `work_queue_items.item_type/item_id` | `planning_consolidation` | `planning_runs(id)` or consolidation artifact id |
| `work_queue_items.item_type/item_id` | `canonical_commit` | consolidation result id |
| `work_queue_items.item_type/item_id` | `feature_request_analysis` | `feature_requests(id)` |
| `work_queue_items.item_type/item_id` | `change_request_analysis` | `change_requests(id)` |
| `work_queue_items.item_type/item_id` | `task_implementation` | `tasks(id)` |
| `work_queue_items.item_type/item_id` | `task_repair` | `tasks(id)` or `runs(id)` |
| `work_queue_items.item_type/item_id` | `task_review` | `tasks(id)` or `runs(id)` |
| `work_queue_items.item_type/item_id` | `merge_queue_processing` | `merge_queue_entries(id)` |
| `work_queue_items.item_type/item_id` | `environment_rerun` | `environment_requirements(id)` or `runs(id)` |

Go validationは、type mapping、target id prefix、project scope、sourceのterminal/open状態を同じtransactionで検証します。

JSON ID配列のvalidation:

- `project_run_profiles.required_verification_environment_ids_json` と `optional_verification_environment_ids_json` の各environment idは存在しなければならない。
- 同じ配列内に重複idを含めてはいけない。
- requiredとoptionalは重複してはいけない。
- projectのprimary_environmentは存在しなければならない。
- `required_for_merge=true` のverification commandが参照するenvironmentはrequired verification environmentsに含まれなければならない。
- `path_mappings.mapping_mode = 'same_filesystem'` の場合、`write_owner_environment_id` は必須。

## Required Unique Constraints

```sql
CREATE UNIQUE INDEX idx_artifact_versions_artifact_version
  ON artifact_versions(artifact_id, version);

CREATE UNIQUE INDEX idx_run_artifacts_run_type_key
  ON run_artifacts(run_id, artifact_type, artifact_key);

CREATE UNIQUE INDEX idx_run_artifacts_command_type
  ON run_artifacts(run_id, command_event_id, artifact_type)
  WHERE command_event_id IS NOT NULL
    AND artifact_type IN ('command_stdout', 'command_stderr', 'command_result');

CREATE UNIQUE INDEX idx_artifacts_project_type_path
  ON artifacts(project_id, artifact_type, path);

CREATE UNIQUE INDEX idx_execution_environments_project_name
  ON execution_environments(project_id, name);

CREATE UNIQUE INDEX idx_execution_environments_one_primary
  ON execution_environments(project_id)
  WHERE role = 'primary' AND status NOT IN ('disabled');

CREATE UNIQUE INDEX idx_project_run_profiles_name
  ON project_run_profiles(project_id, name);

CREATE UNIQUE INDEX idx_project_run_profiles_one_default
  ON project_run_profiles(project_id)
  WHERE default_for_project = 1 AND status = 'active';

CREATE UNIQUE INDEX idx_path_mappings_pair_roots
  ON path_mappings(project_id, from_environment_id, to_environment_id, from_root, to_root);

CREATE UNIQUE INDEX idx_toolchain_requirement_env_name_for
  ON toolchain_requirements(project_id, environment_id, name, required_for);

CREATE UNIQUE INDEX idx_runs_task_type_attempt
  ON runs(task_id, run_type, attempt_no);

CREATE UNIQUE INDEX idx_open_merge_queue_task
  ON merge_queue_entries(task_id)
  WHERE status IN ('queued', 'rebasing', 'reverifying', 'merge_conflict');

CREATE UNIQUE INDEX idx_open_human_approval_task_type
  ON human_approvals(task_id, approval_type)
  WHERE status = 'open';

CREATE UNIQUE INDEX idx_open_patch_application_task
  ON patch_applications(task_id)
  WHERE status IN ('exported', 'manually_applied', 'verifying', 'needs_decision');

CREATE UNIQUE INDEX idx_inbox_open_dedupe
  ON inbox_items(project_id, dedupe_key)
  WHERE status IN ('open', 'snoozed') AND dedupe_key IS NOT NULL;

CREATE UNIQUE INDEX idx_env_req_env_key_for
  ON environment_requirements(project_id, environment_id, key, required_for)
  WHERE environment_id IS NOT NULL;

CREATE UNIQUE INDEX idx_env_req_global_key_for
  ON environment_requirements(project_id, key, required_for)
  WHERE environment_id IS NULL;

CREATE UNIQUE INDEX idx_env_binding_env_key_scope
  ON environment_bindings(project_id, environment_id, key, scope, COALESCE(scope_id, ''))
  WHERE environment_id IS NOT NULL;

CREATE UNIQUE INDEX idx_env_binding_global_key_scope
  ON environment_bindings(project_id, key, scope, COALESCE(scope_id, ''))
  WHERE environment_id IS NULL;

CREATE UNIQUE INDEX idx_open_work_queue_idempotency
  ON work_queue_items(project_id, lane, idempotency_key)
  WHERE status IN ('queued', 'leased', 'running', 'heartbeat_lost', 'waiting_for_human', 'blocked');

CREATE UNIQUE INDEX idx_worker_runs_one_running_lane
  ON worker_runs(project_id, lane)
  WHERE status = 'running';

CREATE UNIQUE INDEX idx_task_dependencies_unique
  ON task_dependencies(task_id, depends_on_task_id, dependency_type);

CREATE UNIQUE INDEX idx_trace_links_unique
  ON trace_links(project_id, source_type, source_id, target_type, target_id, relation);
```

running runの同時実行制御:

```sql
CREATE UNIQUE INDEX idx_runs_one_running_task
  ON runs(task_id)
  WHERE status = 'running';
```

将来、reviewとverificationの並列実行を許す場合は、このindexをrun_type単位のlock tableへ置き換えます。

planning laneはbounded parallelを許しますが、同じplanning inputの重複実行は避けます。

```sql
CREATE UNIQUE INDEX idx_planning_runs_input
  ON planning_runs(project_id, feature_request_id, run_type, input_hash)
  WHERE status IN ('queued', 'running', 'succeeded');
```

## Required Indexes

UIとworkerが頻繁に使うqueryには初期からindexを置きます。

```sql
CREATE INDEX idx_tasks_project_status_priority
  ON tasks(project_id, status, priority, updated_at);

CREATE INDEX idx_execution_environments_project_status
  ON execution_environments(project_id, status, role);

CREATE INDEX idx_toolchain_requirements_env_status
  ON toolchain_requirements(project_id, environment_id, status);

CREATE INDEX idx_command_events_run_env_status
  ON command_events(run_id, environment_id, status);

CREATE INDEX idx_runs_task_started
  ON runs(task_id, started_at);

CREATE INDEX idx_verification_results_run
  ON verification_results(run_id, status);

CREATE INDEX idx_gate_results_run_status
  ON gate_results(run_id, status);

CREATE INDEX idx_inbox_items_project_status_priority
  ON inbox_items(project_id, status, priority, created_at);

CREATE INDEX idx_workflow_events_task_created
  ON workflow_events(task_id, created_at);

CREATE INDEX idx_trace_links_source
  ON trace_links(project_id, source_type, source_id);

CREATE INDEX idx_trace_links_target
  ON trace_links(project_id, target_type, target_id);

CREATE INDEX idx_work_queue_lane_status_priority
  ON work_queue_items(project_id, lane, status, priority, run_after, created_at);

CREATE INDEX idx_work_queue_lease_expiry
  ON work_queue_items(project_id, lane, lease_expires_at)
  WHERE status IN ('leased', 'running');

CREATE INDEX idx_planning_runs_project_status
  ON planning_runs(project_id, status, updated_at);

CREATE INDEX idx_planning_artifacts_feature_type_status
  ON planning_artifacts(project_id, feature_request_id, artifact_type, status);
```

## JSON Column Contract

SQLiteにはJSONをTEXTで保存します。保存前にGo structへdecodeし、JSON Schemaがあるものはschema validationを通します。

| Column | Validation Target |
| --- | --- |
| `tasks.acceptance_criteria_json` | `AcceptanceCriteria[]` |
| `tasks.verification_commands_json` | `VerificationCommand[]` |
| `execution_environments.capabilities_json` | `ExecutionEnvironmentCapabilities` |
| `project_run_profiles.required_verification_environment_ids_json` | `ExecutionEnvironmentID[]` |
| `project_run_profiles.optional_verification_environment_ids_json` | `ExecutionEnvironmentID[]` |
| `target_platforms.required_toolchains_json` | `ToolchainRequirementRef[]` |
| `toolchain_requirements.detector_command_json` | `DetectorCommand` |
| `decisions.evidence_json` | `.devagent/schemas/decision-report.schema.json` のevidence部分 |
| `human_approvals.evidence_json` | `HumanApprovalEvidence` |
| `workflow_events.evidence_json` | `WorkflowEventEvidence` |
| `gate_results.evidence_json` | `devos.gate-result.v1` |
| `gate_results.recommended_next_action_json` | `GateRecommendedNextAction` |
| `runs.changed_files_json` | `ChangedFile[]` |
| `runs.touched_test_files_json` | `TouchedTestFile[]` |
| `command_events.argv_json` | `CommandArgv` |
| `command_events.detected_risks_json` | `DetectedCommandRisk[]` |
| `semantic_behavior_diffs.evidence_json` | `.devagent/schemas/semantic-behavior-diff.schema.json` |
| `environment_requirements.validation_json` | `EnvironmentValidationRule` |
| `planning_runs.artifact_snapshot_json` | `PlanningSnapshot` |
| `planning_artifacts.artifact_snapshot_json` | `PlanningSnapshot` |
| `decision_report_drafts.content_json` | `.devagent/schemas/decision-report-draft.schema.json` |
| `decision_report_drafts.artifact_snapshot_json` | `PlanningSnapshot` |
| `understanding_snapshots.interpreted_goal_json` | `string[]` |
| `understanding_snapshots.assumptions_json` | `UnderstandingAssumption[]` |
| `understanding_snapshots.open_questions_json` | `UnderstandingOpenQuestion[]` |
| `understanding_snapshots.affected_context_json` | `AffectedContext` |
| `understanding_snapshots.risk_json` | `RiskAssessment` |
| `proposal_batches.intent_item_ids_json` | `IntentItemID[]` |
| `proposal_batches.summary_json` | `ProposalBatchSummary` |
| `proposal_deltas.delta_json` | `ProposalDelta` |
| `approval_packets.summary_json` | `ApprovalPacketSummary` |
| `approval_packets.options_json` | `DecisionOption[]` |
| `work_queue_items.error_json` | `WorkQueueError` |
| `patch_applications.evidence_json` | `PatchApplicationEvidence` |

空オブジェクトや空配列を許すかはschemaごとに明示します。GateResult evidenceは空配列不可です。

DB列以外のOrchestrator-owned API/CLI outputも、UIが直接依存するものはschema registryで検証します。

| Output | Validation Target |
| --- | --- |
| `devos ui snapshot` / `GET /api/ui/snapshot` | `devos.human-inbox-snapshot.v1` |

## Path Columns

path列は保存前に正規化します。

Path columns must carry or be resolvable to an `execution_environment_id`. Windows paths and WSL/Linux paths are both valid, but must be validated by the environment's path rules. Cross-environment path conversion is allowed only through `path_mappings` and `PathMappingService`.

`projects.root_path` はbootstrap/default用に残してよいですが、canonical rootは `execution_environments.project_root` を正とします。

| Column | Root |
| --- | --- |
| `projects.root_path` | bootstrap/default path; canonical rootは `execution_environments.project_root` |
| `execution_environments.project_root` | environment-specific absolute path |
| `execution_environments.worktree_root` | environment-specific absolute path; `.devagent-worktrees/` はdefault |
| `path_mappings.from_root` / `to_root` | registered root only |
| `tasks.artifact_path` | project root |
| `runs.worktree_path` | environment worktree root |
| `runs.prompt_path` | orchestrator data root |
| `runs.events_path` | orchestrator data root |
| `runs.stderr_path` | orchestrator data root |
| `runs.final_message_path` | orchestrator data root |
| `runs.diff_path` | orchestrator data root |
| `run_artifacts.path` | orchestrator data root |
| `patch_applications.patch_path` | orchestrator data root |
| `verification_results.stdout_path` | orchestrator data root |
| `verification_results.stderr_path` | orchestrator data root |
| `decisions.report_path` | orchestrator data root or project `.devagent/decisions/` |

path traversal、symlink escape、Codex writable rootとartifact rootの重なりは禁止します。

Windows pathではdrive letter、UNC、reserved name、NUL、control characterを検査します。WSL/Linux pathではabsolute path、NUL、symlink escapeを検査します。case-sensitive filename collisionとline ending policyはpreflightで検査します。

## Required Non-State Columns

Artifact approval metadata:

```sql
approval_notes TEXT,
rejected_reason TEXT,
reviewed_by TEXT,
reviewed_at TEXT,
approved_by TEXT,
approved_at TEXT
```

`approved_with_notes` の場合は `approval_notes` を必須にします。`rejected` の場合は `rejected_reason` を必須にします。この条件はSQLite CHECKまたはGo validationで強制します。

Run artifact integrity:

```sql
CREATE TABLE run_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
  command_event_id TEXT REFERENCES command_events(id) ON DELETE SET NULL,
  artifact_type TEXT NOT NULL CHECK (artifact_type IN (
    'prompt', 'events_jsonl', 'final_message', 'diff',
    'verification_summary', 'gate_result', 'review', 'summary', 'secret_scan',
    'command_stdout', 'command_stderr', 'command_result'
  )),
  artifact_key TEXT NOT NULL,
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  redaction_status TEXT NOT NULL CHECK (redaction_status IN ('not_needed', 'redacted', 'failed')),
  created_at TEXT NOT NULL
);
```

Human approval evidence must reference `run_artifacts.id` and `content_hash`, not only file paths. command stdout/stderr artifacts use `command_event_id` plus `artifact_type` or a stable `artifact_key` such as `windows-build.stdout`.

`command_events.stdout_artifact_id` / `stderr_artifact_id` と `run_artifacts.command_event_id` は循環参照になり得るため、command_event作成時はartifact idをNULLで保存し、artifact保存後に同じtransactionでcommand_eventを更新します。必要なら該当FKはDEFERRABLEまたはGo validation補完にします。

## Minimal DDL Pattern

実際のmigrationでは、[data-model.md](data-model.md) のtable定義にこの文書のFK、CHECK、UNIQUE、indexを加えます。例:

```sql
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
  task_group_id TEXT REFERENCES task_groups(id) ON DELETE SET NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN (
    'proposed', 'ready', 'implementing', 'verifying', 'diagnosing',
    'repairing', 'reviewing', 'needs_input', 'needs_decision',
    'blocked_on_environment', 'blocked_on_policy',
    'ready_for_human_review', 'approved_for_merge', 'queued_for_merge',
    'rebasing', 'reverifying', 'merge_conflict',
    'patch_exported', 'manually_applied', 'merged', 'applied', 'failed', 'cancelled'
  )),
  priority TEXT NOT NULL CHECK (priority IN ('critical', 'high', 'medium', 'low')),
  risk_level TEXT NOT NULL CHECK (risk_level IN ('critical', 'high', 'medium', 'low')),
  artifact_path TEXT NOT NULL,
  base_commit TEXT,
  head_commit TEXT,
  current_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
  acceptance_criteria_json TEXT NOT NULL,
  verification_commands_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

`tasks.current_run_id` と `runs.task_id` は循環参照になるため、migrationではSQLiteの制約挙動を確認し、必要なら `current_run_id` をFKなしにしてGo validationで補完します。その場合もrepository testsで不整合を検出します。
