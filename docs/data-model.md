# Data Model

## Storage Strategy

SQLiteは検索、一覧、状態管理、UI表示の正規データに使います。Markdown/YAMLはAIと人間が読みやすい成果物として残します。

DBだけにしない理由:

- AIに読ませやすい
- 人間が確認しやすい
- Git管理しやすい
- 破損時に復旧しやすい
- 実行履歴が監査可能になる

## SQLite Tables

Canonical table set:

- `projects`
- `execution_environments`
- `project_run_profiles`
- `path_mappings`
- `target_platforms`
- `toolchain_requirements`
- `artifacts`
- `artifact_versions`
- `change_requests`
- `feature_requests`
- `task_groups`
- `tasks`
- `planning_runs`
- `planning_artifacts`
- `decision_report_drafts`
- `task_dependencies`
- `work_queue_items`
- `worker_runs`
- `runs`
- `run_artifacts`
- `command_events`
- `verification_results`
- `gate_results`
- `inbox_items`
- `decisions`
- `human_approvals`
- `environment_requirements`
- `environment_bindings`
- `environment_audit_events`
- `trace_links`
- `workflow_events`
- `merge_queue_entries`
- `patch_applications`
- `dependency_risk_ledger`
- `semantic_behavior_diffs`
- `policies`
- `memories`

この一覧はcanonical table setです。実際のmigration作成順は [storage-schema.md](storage-schema.md) を正とし、Migration 001では `change_requests`、`feature_requests`、`task_groups` を `tasks` より前に作成します。

JSON文字列で保存する列は、Go側で構造体へdecodeして検証します。Codexの構造化出力にはJSON Schemaを使います。

この文書は概念モデルと代表schemaを説明します。正規enumの優先ソースは、状態遷移が [state-machine.md](state-machine.md)、DDL / CHECK / FK / UNIQUE / indexが [storage-schema.md](storage-schema.md) です。この文書内のstatus/typeコメントは説明用であり、実装時はstate-machineとstorage-schemaを優先します。

SQLite実装時の制約、migration、index、path safety、JSON column contractは [storage-schema.md](storage-schema.md) を正規仕様とします。状態をまたぐ不変条件は [state-invariants.md](state-invariants.md)、許可される状態遷移は [state-machine.md](state-machine.md) を参照します。

初期実装からDBを単なる記録メモとして扱わず、少なくとも次を強制します。

- `PRAGMA foreign_keys = ON`
- `schema_migrations` tableによるmigration管理
- status/type列の `CHECK`
- project/task/run/artifact間のFK
- artifact version、run attempt、open inbox dedupe、open merge queueのUNIQUE制約
- UI/worker query用index
- JSON TEXT列のGo validationとJSON Schema validation
- timestampはUTC RFC3339
- IDはprefix付き文字列で、path separatorとcontrol characterを禁止
- artifact path / run pathは許可root配下に正規化し、path traversalとsymlink escapeを禁止

## Canonical SQLite Schema

実装時はこの節のschemaを概念上の正規table setとします。実際のmigration DDLでは [storage-schema.md](storage-schema.md) のFK、CHECK、UNIQUE、index、delete policyを必ず加えます。背景資料や古い原典にあるschemaと衝突する場合、この節とstorage schemaを優先します。

```sql
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  checksum TEXT NOT NULL,
  applied_at TEXT NOT NULL
);
```

```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  primary_environment_id TEXT,
  default_run_profile_id TEXT,
  platform_mode TEXT NOT NULL DEFAULT 'single_environment', -- single_environment | windows_primary | wsl_primary | hybrid
  lifecycle_status TEXT NOT NULL, -- concept | spec_ready | roadmap_ready | implementing | blocked | complete
  archive_status TEXT NOT NULL, -- active | archived
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE execution_environments (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  os_family TEXT NOT NULL,
  role TEXT NOT NULL,
  shell TEXT NOT NULL,
  project_root TEXT NOT NULL,
  data_root TEXT,
  worktree_root TEXT,
  git_provider TEXT NOT NULL,
  codex_adapter TEXT NOT NULL,
  sandbox_profile TEXT NOT NULL,
  status TEXT NOT NULL,
  capabilities_json TEXT NOT NULL,
  last_preflight_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE project_run_profiles (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  mode TEXT NOT NULL, -- single_environment | windows_primary | wsl_primary | hybrid
  implementation_environment_id TEXT NOT NULL,
  canonical_git_environment_id TEXT NOT NULL,
  canonical_merge_environment_id TEXT NOT NULL,
  required_verification_environment_ids_json TEXT NOT NULL,
  optional_verification_environment_ids_json TEXT NOT NULL,
  default_for_project INTEGER NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE path_mappings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  from_environment_id TEXT NOT NULL,
  to_environment_id TEXT NOT NULL,
  from_root TEXT NOT NULL,
  to_root TEXT NOT NULL,
  mapping_mode TEXT NOT NULL,
  write_owner_environment_id TEXT,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE target_platforms (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  os_family TEXT NOT NULL,
  app_type TEXT NOT NULL,
  framework TEXT,
  packaging TEXT,
  required_environment_id TEXT,
  canonical_verification_environment_id TEXT,
  required_toolchains_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE toolchain_requirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  name TEXT NOT NULL,
  required_for TEXT NOT NULL, -- implementation | verification | runtime | runtime_smoke | deployment
  status TEXT NOT NULL,
  detector_command_json TEXT,
  detected_version TEXT,
  install_hint TEXT,
  requires_admin INTEGER NOT NULL,
  human_action_required INTEGER NOT NULL,
  last_checked_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE feature_requests (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  source TEXT NOT NULL, -- human | change_request | system
  status TEXT NOT NULL, -- queued | analyzing | planned | running | waiting_for_human | completed | cancelled | superseded
  priority TEXT NOT NULL,
  tier TEXT, -- minor_change | workflow_change | architecture_change
  change_request_id TEXT,
  task_group_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE planning_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  feature_request_id TEXT,
  run_type TEXT NOT NULL, -- feature_detail | impact_analysis | decision_draft | task_group_proposal | risk_report | rolling_checkpoint
  status TEXT NOT NULL, -- queued | running | succeeded | failed | cancelled | stale
  artifact_snapshot_json TEXT NOT NULL,
  input_hash TEXT NOT NULL,
  output_summary TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE planning_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  planning_run_id TEXT NOT NULL,
  feature_request_id TEXT,
  artifact_type TEXT NOT NULL, -- feature_detail_report | impact_analysis_report | task_group_proposal | risk_report | rolling_checkpoint_report
  status TEXT NOT NULL, -- draft | proposed | accepted | rejected | superseded | stale
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  artifact_snapshot_json TEXT NOT NULL,
  superseded_by_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE decision_report_drafts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  planning_run_id TEXT,
  feature_request_id TEXT,
  decision_type TEXT NOT NULL, -- dependency | architecture | db_schema | auth | external_api | ux | policy | scope | privacy
  status TEXT NOT NULL, -- draft | batched | promoted | rejected | superseded | stale
  title TEXT NOT NULL,
  batch_key TEXT,
  recommended_option TEXT,
  content_json TEXT NOT NULL,
  artifact_snapshot_json TEXT NOT NULL,
  promoted_decision_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_groups (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  feature_request_id TEXT,
  change_request_id TEXT,
  title TEXT NOT NULL,
  status TEXT NOT NULL, -- proposed | ready | running | waiting_for_human | completed | cancelled
  planning_unit TEXT NOT NULL, -- feature_chunk | technical_subtasks | migration | repair
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_group_id TEXT,
  parent_task_id TEXT,
  decomposition_origin_task_id TEXT,
  planning_unit TEXT, -- feature_chunk | technical_subtask | migration | repair
  title TEXT NOT NULL,
  status TEXT NOT NULL, -- proposed | ready | implementing | verifying | diagnosing | repairing | reviewing | needs_input | needs_decision | blocked_on_environment | blocked_on_policy | ready_for_human_review | approved_for_merge | queued_for_merge | rebasing | reverifying | merge_conflict | patch_exported | manually_applied | merged | applied | failed | cancelled
  priority TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  artifact_path TEXT NOT NULL,
  base_commit TEXT,
  head_commit TEXT,
  current_run_id TEXT,
  acceptance_criteria_json TEXT NOT NULL,
  verification_commands_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_dependencies (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  depends_on_task_id TEXT NOT NULL,
  dependency_type TEXT NOT NULL, -- TaskDependencyType: blocks_execution | blocks_merge | ordering_only
  created_at TEXT NOT NULL
);

CREATE TABLE work_queue_items (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  lane TEXT NOT NULL, -- planning | consolidation | execution | merge
  item_type TEXT NOT NULL, -- planning_run | planning_consolidation | canonical_commit | feature_request_analysis | change_request_analysis | task_implementation | task_repair | task_review | merge_queue_processing | environment_rerun
  item_id TEXT NOT NULL,
  preferred_environment_id TEXT,
  required_environment_id TEXT,
  run_profile_id TEXT,
  status TEXT NOT NULL, -- queued | leased | running | heartbeat_lost | waiting_for_human | blocked | completed | failed | cancelled
  priority TEXT NOT NULL,
  blocked_reason TEXT,
  run_after TEXT,
  lease_owner TEXT,
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  attempt_no INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  idempotency_key TEXT NOT NULL,
  error_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE worker_runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  lane TEXT NOT NULL, -- planning | consolidation | execution | merge
  mode TEXT NOT NULL, -- bounded_parallel | sequential
  max_concurrency INTEGER NOT NULL,
  status TEXT NOT NULL, -- running | paused | stopped | failed | heartbeat_lost
  started_at TEXT NOT NULL,
  finished_at TEXT,
  stop_reason TEXT,
  lease_owner TEXT,
  last_heartbeat_at TEXT,
  processed_items_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT,
  run_id TEXT,
  decision_type TEXT NOT NULL, -- dependency | architecture | db_schema | auth | external_api | ux | policy | scope
  status TEXT NOT NULL, -- open | approved | rejected | revised | superseded
  recommended_option TEXT,
  selected_option TEXT,
  report_path TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE TABLE human_approvals (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  run_id TEXT,
  approval_type TEXT NOT NULL, -- final_review | merge | manual_apply optional post-verify acknowledgement
  status TEXT NOT NULL, -- open | approved | rejected | revised | cancelled | revoked
  evidence_json TEXT NOT NULL,
  approved_by TEXT,
  approved_at TEXT,
  rejected_reason TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  memory_type TEXT NOT NULL, -- policy | preference | implementation_note | baseline_issue
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  scope TEXT NOT NULL, -- project | task | dependency_family | one_time | user_default
  scope_id TEXT,
  expires_at TEXT,
  invalidated_at TEXT,
  invalidated_by_change_request_id TEXT,
  source_type TEXT NOT NULL, -- human_decision | merge | change_request | system
  source_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE workflow_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT,
  run_id TEXT,
  event_type TEXT NOT NULL,
  from_status TEXT,
  to_status TEXT,
  actor TEXT NOT NULL,
  reason TEXT,
  evidence_json TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE merge_queue_entries (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  status TEXT NOT NULL, -- queued | rebasing | reverifying | merge_conflict | merged | cancelled
  base_commit TEXT NOT NULL,
  target_branch TEXT NOT NULL,
  queued_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  rebase_run_id TEXT,
  reverify_run_id TEXT,
  merge_run_id TEXT,
  conflict_summary TEXT,
  updated_at TEXT NOT NULL
);
```

`primary_environment_id` と `default_run_profile_id` は循環参照になる場合があるため、SQLite FKではなくGo validationで整合性を強制してよいです。`platform_mode` と `project_run_profiles.mode` の正規値は `single_environment | windows_primary | wsl_primary | hybrid` です。`single_environment` はprimary environmentだけを持つrun profileです。

Path columns must carry or be resolvable to an `execution_environment_id`. Windows paths and WSL/Linux paths are both valid, but must be validated by the environment's path rules. Cross-environment path conversion is allowed only through `path_mappings` and `PathMappingService`.

`projects.root_path` はbootstrap/default用に残せますが、canonical rootは `execution_environments.project_root` を正とします。

`TaskStatus` は実装全体でこの一覧を正とします。

| Status | Meaning |
| --- | --- |
| `proposed` | 生成済みだが、承認済みartifact不足や計画中でまだ実装不可 |
| `ready` | 実装runを開始できる |
| `implementing` | Codex implementation run中 |
| `verifying` | verification command実行中 |
| `diagnosing` | 失敗原因分類中 |
| `repairing` | repair run中 |
| `reviewing` | review run / semantic diff生成中 |
| `needs_input` | 環境変数など、方針判断ではない入力待ち |
| `needs_decision` | Decision Reportに対する人間判断待ち |
| `blocked_on_environment` | 環境要因で停止中 |
| `blocked_on_policy` | policyまたはHARD_BLOCKで停止中 |
| `ready_for_human_review` | merge前の最終人間レビュー待ち |
| `approved_for_merge` | 人間がmergeを承認済み |
| `queued_for_merge` | merge queue投入済み |
| `rebasing` | 最新mainへのrebaseまたはmerge中 |
| `reverifying` | merge前再検証中 |
| `merge_conflict` | conflict解決待ち |
| `patch_exported` | 手動適用用patchがexport済み |
| `manually_applied` | 人間が適用commitを登録済み、Orchestrator再検証前 |
| `merged` | mainへ反映済み |
| `applied` | 手動適用commitが再検証済み |
| `failed` | 自動継続できない失敗として終了 |
| `cancelled` | 人間またはpolicyにより中止 |

`running`、`draft`、`blocked`、`done` はTaskStatusとして使いません。実行中の詳細は `runs.status` と `workflow_events` で表現します。

TaskStatusの遷移は [state-machine.md](state-machine.md) のallowed transition tableだけを許可します。状態一覧に含まれていても、tableにない遷移は実装バグとして拒否します。

Policy memoryは人間の判断回数を減らすために使いますが、永続的な真実として扱いません。scopeと期限を必ず持てるようにし、Change Requestで上書きまたは無効化できるようにします。

例:

```yaml
approved_dependencies:
  - name: zod
    scope: project
    approved_by: human
    reason: form validation and schema sharing
    expires_at: null
    invalidates_when:
      - package_major_version_changes
      - project_policy_changes
```

期限のないmemoryも、Policy / Preference Editorで定期レビュー対象にできます。

## Planning Data

Planning dataはcanonical artifactではありません。bounded parallel planning laneが生成できるのは、`planning_runs`、`planning_artifacts`、`decision_report_drafts` だけです。

正規データへの昇格:

| Draft / Proposal | Promoted to |
| --- | --- |
| `feature_detail_report` | Feature Request更新、またはTask Group説明 |
| `impact_analysis_report` | Change Impact Report、Task Impact Classification |
| `decision_report_draft` | `decisions` + `inbox_items` |
| `task_group_proposal` | `task_groups` + `tasks` |
| `risk_report` | `gate_results`、Decision draft、Dependency Risk Ledger候補 |
| `rolling_checkpoint_report` | 次のTask Group候補、artifact更新案、Human Inbox候補 |

昇格はPlanning ConsolidatorとSerial Canonical Commitだけが行います。Planning workerは `tasks`、`artifacts`、`artifact_versions`、`merge_queue_entries` を直接更新してはいけません。

`artifact_snapshot_json` の最小形:

```json
{
  "prd_version_id": "ARTVER-PRD-012",
  "architecture_version_id": "ARTVER-ARCH-008",
  "roadmap_version_id": "ARTVER-ROADMAP-006",
  "task_set_hash": "sha256:...",
  "policy_version_id": "POLICY-003"
}
```

Consolidatorは、snapshotが現在のartifact versionと異なるplanning artifactを `accepted` にする前にrevalidateします。revalidateできない場合は `stale` にします。

## Artifact Versioning

成果物は常にversion、hash、承認状態、親version、change requestとの関係を持ちます。人間の変更要求をあとから取り込むには、artifactの履歴と承認単位を追跡できる必要があります。

```sql
CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  artifact_type TEXT NOT NULL,
  path TEXT NOT NULL,
  approved_version_id TEXT,
  latest_version_id TEXT,
  status TEXT NOT NULL, -- draft | proposed | approved | approved_with_notes | rejected | superseded
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE artifact_versions (
  id TEXT PRIMARY KEY,
  artifact_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  artifact_type TEXT NOT NULL,
  path TEXT NOT NULL,
  version INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  parent_version_id TEXT,
  change_request_id TEXT,
  status TEXT NOT NULL, -- draft | proposed | approved | approved_with_notes | rejected | superseded
  approval_notes TEXT,
  rejected_reason TEXT,
  reviewed_by TEXT,
  reviewed_at TEXT,
  approved_at TEXT,
  approved_by TEXT,
  created_at TEXT NOT NULL
);
```

`artifact_versions.status` が承認状態のsource of truthです。`artifacts.approved_version_id` と `artifacts.latest_version_id` は用途を分けます。

- `approved_version_id`: trusted contextとして使う最新approved version。
- `latest_version_id`: draft / proposedを含む最新version。

Change Request中にv1 approved、v2 proposedが存在する場合、trusted contextは常に `approved_version_id = v1` を見ます。作業中の提案やimpact analysisは `latest_version_id = v2` を見ます。新versionが承認されたら、同じtransactionで `approved_version_id = latest_version_id` にし、旧approved versionを `superseded` にします。

`artifacts.status` はlatest側の集約/projectionとして扱います。trusted context判定に `artifacts.status` や `latest_version_id` を使ってはいけません。

Coding Agentが読む `.devagent/prd.md` と `.devagent/architecture.md` は、必ず `approved` または `approved_with_notes` のartifact versionから生成します。`approved_with_notes` の `approval_notes` はtrusted contextへ含めます。PRD、Architecture、Roadmapがdraft/proposed/rejectedのままならTask YAMLを `ready` にしません。

## Runs

`runs` は同じtaskに複数作られます。初回実装、修復、検証、レビュー、再計画、rebase、merge前再検証をattemptとして記録します。

```sql
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  run_type TEXT NOT NULL, -- implementation | repair | verification | review | replan | rebase | reverify | merge | patch_export
  status TEXT NOT NULL,
  attempt_no INTEGER NOT NULL,
  repair_of_run_id TEXT,
  implementation_environment_id TEXT,
  primary_verification_environment_id TEXT,
  run_profile_id TEXT,
  path_mapping_id TEXT,
  sandbox_profile TEXT,
  reverify_context_type TEXT,
  reverify_context_id TEXT,
  stop_reason TEXT,
  next_action TEXT,
  worktree_path TEXT,
  prompt_path TEXT,
  events_path TEXT,
  stderr_path TEXT,
  final_message_path TEXT,
  diff_path TEXT,
  base_commit TEXT NOT NULL,
  head_commit TEXT,
  changed_files_json TEXT,
  touched_test_files_json TEXT,
  baseline_verification_run_id TEXT,
  implementation_verification_run_id TEXT,
  started_at TEXT,
  completed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE command_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  command_kind TEXT NOT NULL,
  runner TEXT NOT NULL,
  cwd TEXT NOT NULL,
  argv_json TEXT NOT NULL,
  shell_invocation INTEGER NOT NULL,
  network_policy TEXT NOT NULL,
  exit_code INTEGER,
  status TEXT NOT NULL,
  stdout_artifact_id TEXT,
  stderr_artifact_id TEXT,
  detected_risks_json TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

Run artifactのpathだけでは後から証拠ファイルが変わったか検出できないため、初期実装からhashを保存します。`runs.*_path` は高速アクセス用の代表pathとして残せますが、approval evidenceは `run_artifacts.content_hash` を参照します。

Hybrid verificationでは、1つの `verification` / `reverify` runが複数environmentの `command_events` と `verification_results` を持てます。run自体に単一の `verification_environment_id` は持たせません。代表表示が必要な場合だけ `primary_verification_environment_id` をnullableで保存できますが、権威あるenvironmentはchild tableの `environment_id` です。

`implementation` / `repair` runでは `implementation_environment_id` を必須にします。`verification` / `reverify` runでは `run_profile_id` または検証command群からenvironmentを解決し、各 `verification_results.environment_id` と `command_events.environment_id` を必須にします。

`project_run_profiles.required_verification_environment_ids_json` はrun全体の要求環境として扱います。`UNIQUE(task_id, run_type, attempt_no)` は維持し、複数environmentの結果はchild tableで表現します。

```text
run
  ├─ command_event: windows-main / windows-build
  ├─ command_event: windows-main / windows-test
  └─ command_event: wsl-sidecar / wsl-lint
```

pending/running/terminal timestamp validationはGo state transition serviceで強制します。

- `status = pending`: `started_at` must be NULL
- `status = running`: `started_at` must be NOT NULL and `completed_at` must be NULL
- terminal status: `started_at` and `completed_at` must be NOT NULL

`created_at` はrecord作成時刻であり、pending run / pending command eventでも必須です。`updated_at` は状態、artifact参照、結果分類、停止理由などが変わるたびに更新します。

`reverify_context_type` は `merge_queue_entry | patch_application` のいずれかです。merge queue由来のreverifyは `merged` にだけ進め、manual patch apply由来のreverifyは `applied` にだけ進めます。manual applyの検証も `run_type = 'reverify'` とし、patch専用の別run typeは持ちません。

```sql
CREATE TABLE run_artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  command_event_id TEXT,
  artifact_type TEXT NOT NULL, -- prompt | events_jsonl | final_message | diff | verification_summary | gate_result | review | summary | secret_scan | command_stdout | command_stderr | command_result
  artifact_key TEXT NOT NULL,
  path TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  redaction_status TEXT NOT NULL, -- not_needed | redacted | failed
  created_at TEXT NOT NULL
);
```

1つのrunに複数commandのstdout/stderrを保存できるよう、artifactは `artifact_key` または `command_event_id` で識別します。`command_events.stdout_artifact_id` と `command_events.stderr_artifact_id` は `run_artifacts.id` を参照します。

## Inbox Items

Human Inboxに表示する項目は、Decisionそのものではなくprojection / queueとして保存します。source of truthは `decisions`、`human_approvals`、`environment_bindings`、`gate_results`、`verification_results`、`change_requests`、`dependency_risk_ledger`、`patch_applications` などです。`inbox_items` は人間へ見せる順序、重複排除、batch操作、snoozeを扱う表示用キューです。

```sql
CREATE TABLE inbox_items (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT,
  run_id TEXT,
  item_type TEXT NOT NULL, -- human_decision | human_input | approval | report | hard_block | change_request | merge_conflict | platform_setup | toolchain_setup
  status TEXT NOT NULL, -- open | snoozed | resolved | dismissed
  priority TEXT NOT NULL, -- critical | high | medium | low
  title TEXT NOT NULL,
  summary TEXT NOT NULL,
  recommended_action TEXT,
  source_type TEXT NOT NULL, -- decision | human_approval | environment_requirement | environment_binding | gate_result | verification_result | change_request | merge_conflict | dependency | patch_application | execution_environment | path_mapping | toolchain_requirement | run_profile
  source_id TEXT NOT NULL,
  dedupe_key TEXT,
  batch_key TEXT,
  created_at TEXT NOT NULL,
  snoozed_until TEXT,
  resolved_at TEXT
);
```

状態の二重管理を避けるため、sourceがresolvedになったら対応するinbox itemを同じtransactionでresolvedへ同期します。同期できない場合はsource側を正とし、inbox itemを再生成します。`dedupe_key`、`batch_key`、snooze、hard block dismiss可否、report-only件数の扱いは [state-machine.md](state-machine.md) のInbox Projection Syncを正とします。

Final ReviewとMerge Approvalは `human_approvals` がsource of truthです。Manual Applyは標準では `patch_applications` がsource of truthです。`inbox_items` は承認待ちやattestation待ちを表示するprojectionであり、`approved_for_merge`、`queued_for_merge`、`patch_exported`、`manually_applied` への状態遷移は対応sourceの更新と同じtransactionで行います。

## Verification Results

テスト、lint、buildは独立した結果として保存します。runの成否だけでは、失敗原因分類や自動修復の証拠として粗すぎます。

```sql
CREATE TABLE verification_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  command TEXT NOT NULL,
  command_event_id TEXT,
  verification_command_id TEXT NOT NULL,
  required_for_merge INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL, -- passed | failed | skipped | error
  exit_code INTEGER,
  stdout_path TEXT,
  stderr_path TEXT,
  summary TEXT,
  failure_class TEXT, -- current_diff | environment | baseline | spec_gap | unknown
  failure_signature TEXT,
  base_commit TEXT,
  head_commit TEXT,
  baseline_verification_run_id TEXT,
  implementation_verification_run_id TEXT,
  started_at TEXT,
  completed_at TEXT,
  created_at TEXT NOT NULL
);
```

## Gate Results

Decision Gateの結果は、Decisionとは別に保存します。GateResultは `PASS`、`AUTO_REPAIR`、`AUTO_REPLAN`、`REPORT_ONLY`、`HUMAN_INPUT`、`HUMAN_DECISION`、`HARD_BLOCK` のいずれかです。

```sql
CREATE TABLE gate_results (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  gate_id TEXT NOT NULL,
  status TEXT NOT NULL, -- PASS | AUTO_REPAIR | AUTO_REPLAN | REPORT_ONLY | HUMAN_INPUT | HUMAN_DECISION | HARD_BLOCK
  severity TEXT NOT NULL,
  reason TEXT NOT NULL,
  human_action_type TEXT, -- input | decision | review | policy_approval
  evidence_json TEXT NOT NULL,
  recommended_next_action_json TEXT,
  human_required INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
```

`gate_results.status` はGateResultの正規actionです。`action` という別列は持たせず、次に行う処理は `recommended_next_action_json` に保存します。

## Semantic Behavior Diff

Human Reviewでは生diffだけでなく、実際の挙動変化を要約したsemantic behavior diffを保存します。LLMがdiffにないことを言わないよう、各項目にはdiff由来の証拠を付けます。証拠がないitemは保存してもHuman Reviewの上位表示には出しません。行番号まで取れる場合は `evidence_json` に含め、取れない場合はconfidenceを下げます。

```sql
CREATE TABLE semantic_behavior_diffs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  category TEXT NOT NULL, -- user_visible | non_user_visible | risk | test_change
  summary TEXT NOT NULL,
  evidence_json TEXT NOT NULL,
  confidence TEXT NOT NULL, -- high | medium | low
  created_at TEXT NOT NULL
);
```

`evidence_json` の最小形:

```json
[
  {
    "file": "ui/src/TaskForm.tsx",
    "change_type": "modified",
    "line_start": 42,
    "line_end": 58,
    "source": "git_diff",
    "generated": false
  }
]
```

ルール:

- `file` は対象runの `diff.patch` に存在するpathだけを許可する。
- `change_type` は `added`、`modified`、`deleted`、`renamed` のいずれかにする。
- deleted fileでline rangeが取れない場合は `confidence` を `medium` 以下にする。
- generated fileは `generated: true` を明示し、primary evidenceとしては扱わない。
- LLM review出力は `.devagent/schemas/semantic-behavior-diff.schema.json` で検証する。

例:

```yaml
user_visible:
  - summary: タスク作成フォームにタイトル必須バリデーションが追加された
    evidence:
      - file: ui/src/TaskForm.tsx
        change_type: modified
        line_start: 42
        line_end: 58
        source: git_diff
        generated: false
      - file: ui/src/TaskForm.test.tsx
        change_type: modified
        line_start: 12
        line_end: 34
        source: git_diff
        generated: false
    confidence: high
non_user_visible:
  - summary: TaskRepositoryにCreateメソッドを追加した
    evidence:
      - file: internal/tasks/repository.go
        change_type: modified
        source: git_diff
    confidence: medium
risk:
  - summary: 既存のTaskForm snapshot testを更新している
    evidence:
      - file: ui/src/TaskForm.test.tsx
        change_type: modified
        source: git_diff
    confidence: medium
```

## Change Requests

人間があとから仕様、UI、優先順位、技術方針を変えたい場合は、直接PRDやRoadmapを書き換えず、Change Requestとして取り込みます。

```sql
CREATE TABLE change_requests (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  type TEXT NOT NULL, -- product | architecture | ui | policy | priority | technical
  status TEXT NOT NULL, -- proposed | impact_analyzed | approved | applying | needs_decision | rejected | applied | failed | cancelled
  impact_summary TEXT,
  selected_option TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);
```

## Manual Patch Applications

手動適用フローでは、Orchestratorが生成したpatchと、人間が実際に適用したcommitを別々に記録します。人間がcommit SHAを登録しても、その時点では完了ではありません。Orchestratorがpatch一致確認、verification、Gate再評価を通した後だけ `verified` とし、TaskStatusを `applied` にします。

`patch_applications.verify_run_id` は `runs(run_type = 'reverify', reverify_context_type = 'patch_application')` を指します。manual apply専用の別run typeは使いません。

```sql
CREATE TABLE patch_applications (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  export_run_id TEXT,
  verify_run_id TEXT,
  status TEXT NOT NULL, -- exported | manually_applied | verifying | verified | needs_decision | failed | cancelled
  patch_path TEXT NOT NULL,
  patch_hash TEXT NOT NULL,
  exported_head_commit TEXT NOT NULL,
  applied_commit TEXT,
  applied_by TEXT,
  evidence_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  applied_at TEXT,
  verified_at TEXT
);
```

## Dependency Risk Ledger

本番依存追加はDecision Reportだけで終わらせず、依存ごとの台帳へ記録します。後から「なぜ入れたか」「誰が承認したか」「どのタスクで入ったか」を追跡できるようにします。

```sql
CREATE TABLE dependency_risk_ledger (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  package_manager TEXT NOT NULL, -- go | npm | pnpm | yarn | cargo | other
  dependency_type TEXT NOT NULL, -- LedgerDependencyType: production | development | tool
  introduced_by_task_id TEXT,
  introduced_by_run_id TEXT,
  decision_id TEXT,
  reason TEXT NOT NULL,
  approved_by TEXT,
  risk TEXT NOT NULL, -- low | medium | high | critical
  lockfile_changed INTEGER NOT NULL,
  lifecycle_scripts TEXT NOT NULL, -- none_detected | detected | unknown
  current_version TEXT,
  approved_scope TEXT NOT NULL, -- project | task | one_time | dependency_family
  expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

`dependency_risk_ledger.dependency_type` はLedgerDependencyTypeです。Task間の依存関係を表す `task_dependencies.dependency_type` とは意味が違うため、Go enum、CHECK、validation名を共有してはいけません。

小さなライブラリでも本番依存は長期影響があるため、初期完成スコープでは台帳化します。Policy memoryで既承認の場合も、依存追加の証跡としてledger entryは残します。

## Traceability

PRD要件、Architecture判断、Task、Run、Diff、Verification、Decision、Human Approvalをつなぎます。これがないと、変更要求時の影響範囲分析が手作業になります。

```sql
CREATE TABLE trace_links (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_id TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  relation TEXT NOT NULL
);
```

`trace_links` はChange Requestより前に必要になるため、Migration 001 coreで作成します。影響分析はこのtableを根拠にし、link不足の場合は推測で埋めずにimpact reportへ `trace_missing` として記録します。

platform系entityをtraceに使う場合、`source_type` / `target_type` は `execution_environment`、`path_mapping`、`toolchain_requirement`、`run_profile` も許可します。

例:

```text
PRD requirement R-004
  -> Architecture decision A-002
  -> TASK-007
  -> RUN-20260521-003
  -> diff.patch
  -> verification result
```

## Environment Variables

環境変数のsecret値はSQLiteに保存しません。DBには必要キー、scope、保存先、redacted preview、fingerprint、監査イベントだけを保存します。詳細は [environment-variables.md](environment-variables.md) を参照します。

`environment_id` はproject共通secretの場合NULLを許可します。ただしverification/runtimeへ注入する時点では、どのenvironmentへ注入するかを必ず解決します。WindowsとWSLで保存先pathやsecret storeが異なる場合は別bindingとして扱います。

```sql
CREATE TABLE environment_requirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  key TEXT NOT NULL,
  required_for TEXT NOT NULL, -- implementation | verification | runtime | runtime_smoke | deployment
  status TEXT NOT NULL, -- missing | requested | configured | invalid | waived | cancelled | revoked
  source_hint TEXT NOT NULL, -- user_input | generated_example | external_secret
  validation_json TEXT NOT NULL,
  description TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE environment_bindings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  key TEXT NOT NULL,
  scope TEXT NOT NULL, -- project | task | run | user_default
  scope_id TEXT,
  storage TEXT NOT NULL, -- env_file | os_keychain | external_secret
  storage_ref TEXT NOT NULL,
  status TEXT NOT NULL, -- configured | missing | invalid | revoked
  redacted_preview TEXT,
  value_fingerprint TEXT,
  created_by TEXT NOT NULL, -- human | policy
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_by_run_id TEXT
);

CREATE TABLE environment_audit_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  binding_id TEXT,
  requirement_id TEXT,
  key TEXT NOT NULL,
  action TEXT NOT NULL, -- requested | configured | updated | revoked | used | validation_failed
  actor TEXT NOT NULL,
  scope TEXT NOT NULL,
  scope_id TEXT,
  run_id TEXT,
  command_event_id TEXT,
  redacted_preview TEXT,
  created_at TEXT NOT NULL
);
```

## Policy

自動修復、自動再計画、人間判断条件はDBとYAMLの両方で扱います。YAMLは人間とAIが読み、DBはUIと状態管理が使います。

```sql
CREATE TABLE policies (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  path TEXT NOT NULL,
  version INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

`.devagent/policies/project-policy.yaml`:

```yaml
autonomy:
  default_mode: aggressive_safe
  auto_repair: true
  auto_replan: true
  max_repair_attempts: 2

lane_concurrency:
  planning:
    max_concurrency: 3
  consolidation:
    max_concurrency: 1
  execution:
    max_concurrency: 1
  merge:
    max_concurrency: 1

human_decision:
  require_for:
    - auth_change
    - db_schema_change
    - production_dependency
    - external_api
    - personal_data
  do_not_require_for:
    - test_repair
    - lint_fix
    - small_ui_copy_change
    - task_split_without_scope_change

ui_preferences:
  decision_reports_first: true
  show_raw_logs_by_default: false
  diff_view: semantic
```

## File Layout

各対象プロジェクトに `.devagent/` を置きます。run証拠は原則としてOrchestrator-owned immutable artifactsとして対象リポジトリ外の `orchestrator-data/` に保存します。

```text
my-app/
  AGENTS.md
  go.mod
  cmd/
  internal/
  ui/
    package.json
    src/
  tests/

  .devagent/
    concept.md
    prd.md
    architecture.md
    roadmap.yaml
    schemas/
      run-result.schema.json
      gate-result.schema.json
      decision-report.schema.json
    policies/
      project-policy.yaml
    planning/
      PLANART-021-impact.yaml
      DEC-DRAFT-018.yaml
    decisions/
      DEC-001-auth.md
    change-requests/
      CR-001-today-focus-view.yaml
    tasks/
      TASK-001-project-setup.yaml
    environment/
      required.yaml
      bindings.yaml
    memory/
      project-rules.md
      user-preferences.md
      implementation-notes.md
    dependencies/
      risk-ledger.yaml

orchestrator-data/
  projects/
    PROJECT-001/
      runs/
        RUN-20260521-001/
          prompt.md
          events.redacted.jsonl
          stderr.redacted.log
          final.json
          diff.patch
          semantic-behavior-diff.json
          verification.json
          gate-results.json
          review.json
          summary.md
```

`.devagent/runs/` を使う必要がある場合でも、Codexのwritable rootsから除外し、Coding Agentがrun artifactを書き換えられないようにします。

## Task YAML v2

```yaml
id: TASK-003
title: Implement task creation API and form
status: ready
priority: high
risk_level: medium
task_group_id: TG-021
parent_task_id: null
decomposition_origin_task_id: null
planning_unit: feature_chunk

depends_on:
  - TASK-201
blocks:
  - TASK-204

goal: >
  ユーザーが新しいタスクを作成できるようにする。

context:
  prd: .devagent/prd.md
  architecture: .devagent/architecture.md
  trust_levels:
    project_agents_md: trusted
    devagent_artifacts: trusted_after_validation
    repository_files: untrusted
    logs: untrusted

acceptance_criteria:
  - id: AC-001
    text: ユーザーはタスク名を入力できる
  - id: AC-002
    text: 入力値バリデーションがある
  - id: AC-003
    text: 関連テストが通る

constraints:
  - 認証方式は変更しない
  - 新しい本番依存パッケージを追加しない
  - 関係ないUIを変更しない

verification_commands:
  - id: go-test
    environment: primary
    runner: auto
    required_for_merge: true
    working_dir: task_worktree
    command:
      argv: ["go", "test", "./..."]
    timeout: 10m
    network: false
    required_toolchains:
      - go

  - id: windows-build
    environment: windows-main
    runner: powershell
    required_for_merge: true
    working_dir: task_worktree
    command:
      argv:
        - powershell.exe
        - -NoProfile
        - -ExecutionPolicy
        - Bypass
        - -File
        - .\.devagent\scripts\verify-windows.ps1
    timeout: 15m
    network: false
    required_toolchains:
      - dotnet
      - msbuild

  - id: wsl-lint
    environment: wsl-sidecar
    runner: bash
    required_for_merge: false
    working_dir: mapped_task_worktree
    command:
      argv: ["bash", "-lc", "./.devagent/scripts/lint-linux.sh"]
    timeout: 5m
    network: false

run_policy:
  max_implementation_attempts: 3
  max_repair_attempts: 2
  max_total_runtime_minutes: 30
  auto_repair_allowed_when:
    - test_failure_caused_by_current_diff
    - lint_failure
    - type_error
    - missing_generated_file

decision_triggers:
  - DBスキーマ変更が必要な場合
  - 新しい外部ライブラリが必要な場合
  - 認証・権限設計に影響する場合
```

`environment: primary` はRunProfileでprimary environmentへ解決します。`runner: auto` はenvironmentのdefault shellへ解決します。既存の文字列配列は後方互換として読み込んでもよいですが、正規schemaは構造体です。

## Memory Types

| Type | Purpose | File |
| --- | --- | --- |
| User Preference Memory | ユーザーの好み | `.devagent/memory/user-preferences.md` |
| Project Memory | プロジェクト固有の判断 | `.devagent/memory/project-rules.md` |
| Implementation Memory | 実装上の注意 | `.devagent/memory/implementation-notes.md` |
