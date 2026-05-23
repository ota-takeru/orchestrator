# Task Planning and Work Queue

## Goal

ユーザーが複数の機能要望を自然文で出したとき、OrchestratorはそれらをRequest Queueに積み、既存artifactとtaskを照査し、必要なChange Request、Task Group、Taskへ展開し、順次実行できるようにします。

この設計の目的は、Codexを細かい作業指示で縛ることではありません。大きめの機能単位を安全に実装へ渡し、必要なときだけ分解、停止、人間判断へ回します。

## Principles

- default task granularityは `feature_chunk` とする。
- 最初からmicro taskへ分割しない。
- Codexには1つのtask内で内部計画を立てて実装させる。
- 分割は安全境界、検証境界、rollback境界、人間判断境界を越えるときに行う。
- 実装手順、ファイル、コンポーネントだけを理由に分割しない。
- 複数要望はRequest Queueで管理する。
- 要件詳細化、影響分析、意思決定レポートdraft、task proposal作成は `bounded_parallel` planning laneで実行してよい。
- canonical artifact、task、roadmapへの反映はsingle writerでserial commitする。
- implementationとmerge queueはsequentialとする。
- Human Inboxで止まったtaskがあっても、依存しない別taskは継続できる。
- Task Group完了後は、次の実装へ進む前にRolling Planning Checkpointを実行する。
- PRD、Architecture、Roadmap、Task Breakdownは初期生成で固定せず、実装結果、検証結果、Decision Gate、Change Request、Feature Requestに応じて承認付きの新しいartifact versionとして更新する。
- concurrent task executionはLater扱いにする。
- implementation workはproject_run_profile.implementation_environment_idで実行する。
- canonical commit / merge queueはcanonical environmentでだけ処理する。

## Request Intake

ユーザーの自然文要望は、最初にFeature Requestとして保存します。

```text
Human Request
  -> Feature Request
  -> Parallel Planning Lane
  -> Planning Consolidation
  -> Change Request or Task Group
  -> Task
  -> Run
```

Feature Requestは、必ずしも即座にChange Requestになるとは限りません。

| Case | Handling |
| --- | --- |
| 小さなUI文言変更 | `minor_change` として直接task化してよい |
| 既存仕様内の機能追加 | Task Groupへ展開してよい |
| PRD / Architectureに影響する | Change Requestを作る |
| DB / auth / external API / dependency / personal dataに影響する | Change Request + Decision Gate必須 |
| 複数機能が混ざる | 複数Feature RequestまたはTask Groupへ分ける |

## Lane Model

Orchestratorの非同期性は、実装を並列化することではなく、planningを並列化して実装前の整理を進めることで得ます。

```text
User Requests
  -> Feature Request Queue
  -> Parallel Planning Lane
      -> Requirement Detail Report
      -> Impact Analysis Report
      -> Decision Report Draft
      -> Task Group Proposal
      -> Risk Report
  -> Planning Consolidation
  -> Human Inbox if needed
  -> Serial Canonical Commit
  -> Sequential Execution Lane
      -> implementation
      -> verification
      -> repair
      -> review
      -> merge queue
```

正規方針:

- parallel planning
- serial canonical commit
- sequential implementation
- sequential merge

## Parallel Planning Lane

Planning LaneはFeature Requestごと、またはFeature Request群ごとにread-only分析を行い、planning artifactだけを生成します。

並列実行してよいもの:

- Feature Requestの詳細化
- 既存PRD / Architecture / Roadmapへの影響分析
- 既存taskとの重複確認
- acceptance criteria draft作成
- Decision Report draft作成
- dependency / DB / auth / external API / privacy risk検出
- Task Group proposal作成
- 実装順序候補作成
- 人間に聞くべき質問の抽出

並列実行してはいけないもの:

- PRDを直接書き換える
- Architectureを直接書き換える
- Roadmapを直接書き換える
- canonical task DBを複数workerが同時に確定更新する
- merge queueを複数workerが同時に進める
- implementation runを複数同時に開始する

Planning workerはread-only snapshotを入力にし、`planning_runs`、`planning_artifacts`、`decision_report_drafts` だけを書きます。

## Environment Context

Work Queue itemはenvironment contextを持てます。

```text
work_queue_items:
  preferred_environment_id
  required_environment_id
  run_profile_id
```

Scheduler rules:

- implementation workは `project_run_profile.implementation_environment_id` で実行する。
- verification workは `verification_command.environment` をRunProfileで解決する。
- missing toolchainのenvironmentにはwork itemをleaseしない。
- optional verification laneのfailureはexecution laneを止めず、Gate policyで扱う。
- canonical commit / merge queueはcanonical environmentでだけ処理する。

## Planning Runs

Planning runは実装runとは別です。コード変更、canonical artifact更新、task確定を行わず、分析結果を保存します。

```yaml
planning_run:
  id: PLANRUN-021
  feature_request_id: FR-021
  run_type: impact_analysis
  status: succeeded
  artifact_snapshot:
    prd_version: PRD-v12
    architecture_version: ARCH-v8
    roadmap_version: ROADMAP-v6
    task_set_hash: sha256:...
  outputs:
    - PLANART-021-impact
    - DEC-DRAFT-018
```

Consolidatorは、planning runのsnapshotが古い場合にその結果をそのまま採用してはいけません。

| Snapshot check | Handling |
| --- | --- |
| current artifact versionsと一致 | 採用可能 |
| versionは古いが影響範囲が無関係 | revalidateして採用可能 |
| versionが古く、該当領域が変わっている | staleにして再分析 |
| task set hashが変わり重複判定に影響する | task proposalを再生成 |

## Planning Artifacts

Planning Artifactは正規PRDや正規taskではなく、分析結果です。並列生成してよいですが、実装仕様としてCodexへ直接渡す場合はConsolidatorが採用したものだけを使います。

| Planning Artifact | Purpose |
| --- | --- |
| `feature_detail_report` | 要望を実装可能な成果単位へ詳細化する |
| `impact_analysis_report` | PRD / Architecture / Roadmap / Taskへの影響を示す |
| `decision_report_draft` | 人間判断が必要な論点と選択肢を下書きする |
| `task_group_proposal` | Task Groupとfeature chunk task候補を提案する |
| `risk_report` | dependency、DB、auth、external API、privacy riskを検出する |

例:

```yaml
feature_detail_report:
  feature_request_id: FR-021
  title: PDFからタスク候補を生成したい
  clarified_goal:
    - PDFをアップロードできる
    - PDF本文を抽出できる
    - 抽出内容からtask candidateを生成できる
    - task candidateはHuman Inboxで承認できる
  open_questions:
    - PDFを永続保存するか、一時保存だけにするか
    - OCRが必要か
    - 日本語PDFを対象にするか
    - 添付ファイルのサイズ上限をどうするか
  recommended_default:
    - 初期版では一時保存
    - OCRなし
    - text-extractable PDFのみ対象
```

```yaml
task_group_proposal:
  id: TG-PROPOSAL-021
  feature_request_id: FR-021
  planning_unit: feature_chunk
  proposed_tasks:
    - id: TASK-PROPOSED-031
      title: Add PDF upload and text extraction
      planning_unit: feature_chunk
    - id: TASK-PROPOSED-032
      title: Generate task candidates from extracted PDF text
      planning_unit: feature_chunk
    - id: TASK-PROPOSED-033
      title: Review generated task candidates in Human Inbox
      planning_unit: feature_chunk
  requires_decisions:
    - DEC-DRAFT-018
```

`TASK-PROPOSED-*` はcanonical taskではありません。Planning Consolidatorが重複、依存、risk、snapshot freshnessを確認してから、serial canonical commitで正式なTaskへ昇格します。

## Planning Consolidator

Planning Consolidatorはsingle writerです。複数planning workerの出力を統合し、重複、依存、衝突、古いsnapshotを整理します。

責務:

- 似たTask Group proposalをmergeする。
- 同じupload、notification、parserなどの重複taskを統合する。
- Feature Request間の依存関係を決める。
- stale planning artifactをrevalidateまたは再分析へ戻す。
- Decision Report draftをbatch化する。
- canonical commit候補を作る。
- Human Inboxに出すべき判断だけを選別する。

Consolidatorは、planning artifactを採用する前に次を確認します。

- artifact snapshotが最新またはrevalidate済みである。
- acceptance criteriaが削られていない。
- PRD / Architectureの方針を勝手に変更していない。
- `feature_chunk` 粒度を保っている。
- external API、auth、DB、dependency、personal dataのriskがDecision draftへ反映されている。

## Decision Batching

並列planningはDecision Report draftを複数生成できますが、Human Inboxに細かい質問を大量に出してはいけません。

Decision batchingでは、同じFeature Requestまたは同じrisk familyの判断をまとめます。

```yaml
decision_batch:
  id: DEC-BATCH-004
  title: Document Intake Feature Decisions
  feature_request_ids:
    - FR-021
  decisions:
    - file_retention_policy
    - ocr_support
    - file_size_limit
  recommended_next:
    - Implement Today View first
    - Ask decisions for PDF and Slack
    - Continue with AI priority suggestion while waiting
```

batch化してよいもの:

- 同一feature内のstorage / privacy / UX判断
- 同一外部連携内のscope / trigger / frequency判断
- 同じrisk familyの低リスクpolicy approval

batch化しないもの:

- unrelated featureの高リスク判断
- DB schema変更と外部API追加のように承認者の観点が異なる判断
- `HARD_BLOCK`
- secret exposureやprotected file access

## Serial Canonical Commit

Canonical artifactとcanonical taskは、Planning Laneから直接書き換えません。

canonical artifact:

- PRD
- Architecture
- Roadmap
- approved Task / Task Group
- policy / memory

planning artifact:

- Feature Detail Report
- Impact Analysis Report
- Decision Report Draft
- Task Group Proposal
- Risk Report

Serial Canonical Commitは、Consolidatorが採用したproposalだけを1 transactionまたは1 commit unitとして反映します。

```text
planning artifacts
  -> consolidation result
  -> optional Human Inbox approval
  -> canonical commit
      -> Change Request
      -> Task Group
      -> Task
      -> Roadmap update
```

Canonical Commitの条件:

- required artifact snapshotがrevalidate済みである。
- 必要なHuman Decisionがapprovedまたは不要と判定済みである。
- proposed taskが重複していない。
- task dependency graphが保存されている。
- canonical artifact更新はserialで行われる。

## Rolling Planning Checkpoint

Orchestratorは、初期設計を固定計画として扱いません。TaskまたはTask Groupの完了後、次の実装へ進む前にRolling Planning Checkpointを実行します。

このcheckpointは、Feature Request、Change Request、Decision Gateの `AUTO_REPLAN`、`spec_gap`、task size超過のような例外時だけでなく、通常の実装サイクルとして実行します。目的は、完了済み実装から得た証拠を使って、次のTask Groupを再設計することです。

```text
Task Group completed
  -> Verification / Gate / Human Approval
  -> Implementation Summary
  -> Roadmap delta check
  -> Unresolved Decision check
  -> Next Task Group candidates
  -> optional PRD / Architecture / Roadmap update proposal
  -> Human Inbox if approval is required
  -> Serial Canonical Commit
  -> next implementation
```

入力:

- 現在のPRD / Architecture / Roadmap / Task Breakdown
- 完了したTaskまたはTask Groupのdiff
- `verification_results`
- `gate_results`
- `semantic_behavior_diff`
- 未解決のDecision
- Feature Request / Change Request queue
- dependency risk ledger
- policy memory
- 既存のtrace linkと実装summary

出力:

- 次のTaskをそのまま実行可能
- Task Groupを再構成する
- Roadmap更新案を作る
- Architecture更新案を作る
- PRD更新案を作る
- Human Decision Reportを作る
- 実装を一時停止する

Rolling Planning Checkpointの結果、canonical artifactへ反映する必要がある場合は、Planning Consolidatorだけが更新候補を採用し、Serial Canonical Commitで反映します。人間承認が必要なPRD、Architecture、Roadmap、Task Group、risk policyの変更はHuman Inboxに送ります。

Roadmap、Architecture、Task Breakdownは、初期生成後に機械的に消化する固定リストではありません。実装結果、検証結果、Decision Gate、Feature Request、Change Requestにより、新しいartifact versionとして更新されます。Coding Agentへ渡すのは、常に承認済みまたは承認付きnotesのあるartifact versionから生成したcontextだけです。

Rolling Planning Checkpointは、未完了のacceptance criteriaを削って進めるための仕組みではありません。Task Groupの再構成やRoadmap更新を行う場合も、既存の承認済み要求、初期完成スコープ、Decision Gateの判断を保持します。

## Feature Request Lifecycle

```text
queued
  -> analyzing
  -> planned
  -> running
  -> waiting_for_human
  -> completed
```

追加状態:

| Status | Meaning |
| --- | --- |
| `queued` | 受け付け済み、まだ分析していない |
| `analyzing` | 既存artifact / taskへの影響を分析中 |
| `planned` | Task GroupまたはChange Requestへ展開済み |
| `running` | 関連taskの実行中 |
| `waiting_for_human` | 関連Decision / Input / Review待ち |
| `completed` | 関連taskが完了した |
| `cancelled` | 人間またはpolicyで中止 |
| `superseded` | 別Feature RequestまたはChange Requestに置き換えられた |

## Task Granularity Policy

```yaml
task_granularity:
  default: feature_chunk

  prefer_single_task_when:
    - acceptance_criteria_are_cohesive
    - changes_share_one_user_visible_outcome
    - no_new_auth_or_permission_boundary
    - no_new_external_api
    - no_db_schema_change
    - no_production_dependency
    - no_personal_data_handling_change
    - verification_commands_are_same
    - rollback_unit_is_same

  split_before_run_when:
    - architecture_decision_required
    - db_schema_change_required
    - production_dependency_required
    - external_api_or_auth_required
    - personal_data_or_secret_handling_changes
    - independent_features_are_mixed
    - different_verification_domains_are_mixed
    - rollback_units_are_different
    - task_contains_policy_blocking_and_non_blocking_work

  split_after_run_when:
    - task_too_large_gate_result
    - changed_files_exceeds_budget
    - added_lines_exceeds_budget
    - review_finds_unrelated_behavior_changes
    - verification_failure_cannot_be_localized
    - repair_would_exceed_task_scope
    - codex_reports_task_needs_split_with_evidence

  do_not_split_for:
    - individual_files
    - implementation_steps
    - component_names_only
    - trivial_ui_states
    - source_file_vs_test_file
    - helper_function_extraction
```

## Adaptive Split

Auto splitは、最初から細かく設計するための機能ではありません。大きめのfeature chunkとして実行した結果、証拠に基づいて分割が必要になったときに使います。

人間に聞かずに分割してよい条件:

- acceptance criteriaを削らない。
- PRD要件を変えない。
- UX方針を変えない。
- architecture decisionを変えない。
- 分割後のtaskが元taskのgoalを保持している。
- 分割理由がdiff、verification、review、gate resultの証拠に基づく。

人間判断が必要な条件:

- scopeを縮小する。
- requirementを削る。
- architecture decisionを変更する。
- 外部サービス、auth、DB、dependency、personal dataが新たに必要になる。
- 初期完成スコープを変える。

## Task Impact Classification

Change RequestまたはFeature Requestの分析では、既存taskを次のように分類します。

| Impact | Meaning |
| --- | --- |
| `keep` | 変更なしで継続できる |
| `modify` | acceptance criteria、constraints、verificationなどを更新する |
| `split` | 既存taskを複数taskに分ける |
| `merge` | 複数taskを1つのfeature chunkへまとめる |
| `obsolete` | 変更により不要になった |
| `supersede` | 新しいtaskまたはtask groupに置き換える |
| `new` | 新規taskを作る |
| `blocked` | decision、environment、dependencyなどの承認待ち |

## Task Dependency Graph

複数taskを扱うため、taskは依存関係を持ちます。

```yaml
id: TASK-203
title: Add PDF task candidate generation
task_group_id: TG-021
planning_unit: feature_chunk
depends_on:
  - TASK-201
blocks:
  - TASK-204
blocked_by_decisions:
  - DEC-012-storage-policy
```

依存関係の目的:

- 実行順序を決める。
- Human Decision待ちのtaskを飛ばして、別のunblocked taskを進める。
- merge queueでrebase / reverify順序を制御する。
- Change Request時の影響範囲を追跡する。

`task_dependencies.dependency_type` はTaskDependencyTypeとして扱い、正規値は `blocks_execution`、`blocks_merge`、`ordering_only` です。依存関係の強さは実行停止、merge停止、表示/順序付けだけのどれに効くかを明示します。依存packageの種別を表すDependency Risk Ledgerの `dependency_type` とは別enumです。

## Work Queue

workerはtaskだけでなく、request analysis、change application、merge、reverifyも扱います。

```yaml
work_queue_item_types:
  - planning_run
  - planning_consolidation
  - canonical_commit
  - feature_request_analysis
  - change_request_analysis
  - task_implementation
  - task_repair
  - task_review
  - merge_queue_processing
  - environment_rerun
```

各work queue itemはlaneを持ちます。

```yaml
work_queue_item:
  id: WQ-102
  item_type: planning_run
  lane: planning
  item_id: PLANRUN-021
  status: queued
  lease_owner: null
  lease_expires_at: null
  last_heartbeat_at: null
  attempt_no: 0
  max_attempts: 3
  idempotency_key: planning_run:PLANRUN-021
```

Queue item status:

```text
queued
  -> leased
  -> running
  -> heartbeat_lost
  -> waiting_for_human
  -> blocked
  -> completed
  -> failed
  -> cancelled
```

Work queue itemは必ずleaseを持ちます。`running` だけではworker crash時に永久停止するため、次の列を初期実装から持たせます。

| Field | Purpose |
| --- | --- |
| `lease_owner` | itemを保持しているworker id |
| `lease_expires_at` | lease失効時刻 |
| `last_heartbeat_at` | workerが生存更新した時刻 |
| `attempt_no` | 実行試行回数 |
| `max_attempts` | recoveryを含む最大試行回数 |
| `idempotency_key` | 同じwork itemの重複作成防止 |
| `error_json` | 失敗分類、最後のerror、recovery理由 |

lease / heartbeat rule:

- workerは `queued` itemを選ぶとき、同一transactionで `leased` にし、`lease_owner`、`lease_expires_at` を設定し、`attempt_no` を1回だけ増やす。
- 実処理開始時に `leased -> running` へ遷移する。
- running workerはheartbeat intervalごとに `last_heartbeat_at` と `lease_expires_at` を延長する。
- worker起動時と定期recovery時に、`leased` または `running` で `lease_expires_at < now` のitemを `heartbeat_lost` として記録する。
- `attempt_no < max_attempts` なら `heartbeat_lost -> queued` に戻し、`workflow_events` に `work_queue_lease_recovered` を保存する。recovery時に `attempt_no` は増やさない。
- `attempt_no >= max_attempts` なら `failed` にして `error_json` を保存し、必要ならHuman Inboxへreportを出す。
- `idempotency_key` が同じopen itemを二重作成してはいけない。

## Lane Concurrency Policy

```yaml
lane_concurrency:
  planning:
    mode: bounded_parallel
    max_concurrency: 3
    allowed_to_write:
      - planning_runs
      - planning_artifacts
      - decision_report_drafts
    forbidden_to_write:
      - canonical_artifacts
      - tasks
      - roadmap
      - architecture
      - merge_queue

  consolidation:
    mode: sequential
    max_concurrency: 1
    allowed_to_write:
      - change_requests
      - task_groups
      - proposed_tasks
      - inbox_items

  execution:
    mode: sequential
    max_concurrency: 1
    allowed_to_write:
      - runs
      - verification_results
      - gate_results
      - work_queue_items

  merge:
    mode: sequential
    max_concurrency: 1
    allowed_to_write:
      - merge_queue_entries
      - runs
      - verification_results
      - gate_results
```

`planning.max_concurrency` は初期値3です。project policyで下げられますが、implementation concurrencyは初期完成スコープでは1のままです。

## Execution Worker

Execution workerはsequentialに動きます。

```yaml
worker:
  lane: execution
  mode: sequential
  max_concurrency: 1
  auto_process_requests: true
  auto_pick_next_task: true
  continue_unblocked_tasks_when_one_waits_for_human: true

  stop_conditions:
    - no_ready_work
    - budget_exhausted
    - hard_block
    - all_ready_work_requires_same_unresolved_human_input
    - storage_or_git_error

  human_inbox_only_for:
    - product_decision
    - architecture_decision
    - dependency_approval
    - auth_or_permission_change
    - db_schema_change
    - external_api
    - personal_data
    - environment_input
    - final_merge_approval
```

workerの基本ループ:

1. canonical Task Group / TaskからREADY taskを選ぶ。
2. Codex implementation runを作る。
3. verificationを実行する。
4. failure classificationを行う。
5. auto repair / auto replan可能なら実行する。
6. reviewとDecision Gateを実行する。
7. Human Inboxが必要ならitemを作る。
8. merge承認済みならmerge queue itemを作る。
9. TaskまたはTask Groupが完了した場合はRolling Planning Checkpointを実行する。
10. 次のunblocked execution work itemへ進む。

## Scheduler Rules

`PickNextTask` / `PickNextWorkItem` は単純なpriority順ではなく、以下を考慮します。

- unresolved dependencyがない。
- required artifactsが `approved` または `approved_with_notes` である。
- unresolved Human Decisionがない。
- unresolved Environment Inputがない。
- risk level。
- priority。
- feature request priority。
- merge conflict risk。
- 同じfilesを触るtaskが直前にあるか。
- worker budget。
- lane concurrency。
- planning snapshot freshness。
- canonical commit lockの有無。

## Human Waiting Behavior

あるtaskまたはplanning resultが `needs_decision` または `needs_input` になっても、worker全体を止める必要はありません。

継続してよい条件:

- 他にunblocked taskがある。
- そのtaskが待機中decisionに依存していない。
- 同じprotected resourceを変更しない。
- merge queue上で順序依存がない。
- 別Feature Requestのplanningが未完了で、canonical commitを必要としない。

止まる条件:

- すべてのready workが同じdecisionに依存している。
- `HARD_BLOCK` がproject-level policyに関係する。
- environment inputが全taskに必要。
- storage / git / schema migration failureで安全に継続できない。
- canonical commit lockを取得できない。
- planning artifactのsnapshotが古く、revalidateできない。

## CLI Contract

```text
devos request "Today Viewを追加して"
devos request "PDFからタスク候補を作れるようにして"
devos request "Slack通知もほしい"

devos requests
devos queue
devos plan start --concurrency 3
devos plan status
devos plan consolidate
devos work start --planning-concurrency 3 --implementation-concurrency 1
devos work start --mode sequential
devos work start --until inbox
devos work start --budget 30m
devos work status
devos work pause
devos work resume
```

`devos run TASK-001` は残します。これは単一taskを明示実行するmanual/debug commandとして扱います。

## Completion Criteria

この設計が実装されたと見なす条件:

- 複数の自然文要望をFeature Requestとして登録できる。
- Feature Requestのplanningをbounded parallelで実行できる。
- planning workerはplanning artifact / decision draft / task proposalだけを作り、canonical artifactを直接変更しない。
- Planning Consolidatorが並列分析結果を統合できる。
- canonical artifact / task / roadmapへの反映がserial commitで行われる。
- Feature RequestからChange RequestまたはTask Groupへ展開できる。
- TaskまたはTask Group完了後にRolling Planning Checkpointを実行できる。
- Rolling Planning Checkpointが、実装summary、diff、verification results、gate results、未解決Decision、既存Roadmapを入力にして次のTask Group候補またはartifact更新案を生成できる。
- Roadmap、Architecture、Task Breakdownが固定計画ではなく、承認付きartifact versionとして更新されることが保証されている。
- taskを最初からmicro taskへ分割しないpolicyがある。
- splitする条件とsplitしない条件が明文化されている。
- task dependency graphを保存できる。
- execution workerがREADY workを順番に処理できる。
- waiting human itemがある場合でも、unblocked workを継続できる。
- Human Inboxが関連Decisionをbatch表示できる。
- Human Inboxには判断、入力、最終承認が必要なものだけが出る。
- concurrent task executionはLater扱いのままになっている。
