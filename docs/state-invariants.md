# State Invariants

この文書は、SQLite制約だけでは表現しきれない状態、証拠、artifact、Human Inbox、run path の不変条件を定義します。実装ではDB制約、Go validation、JSON Schema validationの三層で強制します。

## Enforcement Layers

| Layer | Responsibility |
| --- | --- |
| SQLite constraints | FK、UNIQUE、CHECK、NOT NULL、index、transaction境界 |
| Go validation | polymorphic reference、path安全性、状態遷移、JSON decode、cross-table invariant |
| JSON Schema | Codex final output、verification result、gate result、semantic diff、decision report |

SQLiteで完全に表現できないものも、必ずGo側のrepository/service境界で検証します。

## Identity

- IDはtable prefixを持つ文字列に統一する。例: `PROJECT-001`、`TASK-003`、`RUN-20260521-001`、`DEC-001`。
- IDは空白、path separator、control characterを含めない。
- DB保存前に正規表現で検証する。
- 外部入力からIDを受けるAPIは、存在確認とproject scope確認を同じtransactionで行う。

## Timestamp

- SQLiteに保存するtimestampはUTCのRFC3339形式に統一する。
- `created_at` は作成後に変更しない。
- `updated_at` は同じtransaction内で状態変更と一緒に更新する。
- `runs.completed_at`、`command_events.completed_at`、その他entityの `finished_at` / `resolved_at` は対応するterminal/resolved状態に入った時だけ設定する。

## Path Safety

- DBに保存するpathは、project rootまたはorchestrator data rootを基準に正規化する。
- 絶対pathを保存する場合も、保存前に許可root配下であることを検証する。
- `..`、symlink escape、空path、NUL byte、home directory展開に依存したpathは禁止する。
- run artifact pathはOrchestrator管理root配下に限定する。
- Codex writable rootとrun artifact rootは重ねない。
- `orchestrator-data/` はGit管理対象にしない。
- `.devagent-worktrees/` はGit管理対象にしない。
- `orchestrator-data/` をrepo内に置く場合はgitignore必須。可能ならrepo外に置く。
- `.devagent/schemas/`、approved artifact、policy、AGENTS.mdはCodex writable rootに含めない。
- path validationは `execution_environment_id` ごとに行う。
- Windows path、WSL path、Linux pathを同じvalidatorで扱わない。
- cross-environment path変換はPathMappingServiceだけが行う。
- same_filesystem mappingで複数environmentが同じworktreeを同時にwriteしてはいけない。
- canonical Git providerはproject_run_profileで1つに固定する。
- canonical merge providerはproject_run_profileで1つに固定する。
- case-sensitive filename collisionをpreflightで検出する。
- line ending policyは `.gitattributes` で固定する。

## Project and Artifact Invariants

- `projects.root_path` は存在するGit repository root、または初期化中のproject rootである。
- `artifacts.approved_version_id` と `artifacts.latest_version_id` は同じ `artifact_id` の `artifact_versions.id` を指す。
- `artifact_versions.version` は同じartifact内で単調増加し、UNIQUE(`artifact_id`, `version`) を満たす。
- approved artifactだけがTask生成のtrusted contextになる。
- PRD、Architecture、Roadmapが `approved` または `approved_with_notes` でない場合、taskは `ready` になれない。
- `artifact_versions.status` がartifact approvalのsource of truth。trusted contextは `artifacts.approved_version_id` だけを見る。`artifacts.latest_version_id` はdraft/proposedを含む作業中の最新versionを指す。
- `approved_with_notes` のartifact versionは `approval_notes` を必須にし、trusted contextへ含める。
- `rejected` のartifact versionからTaskをmaterializeしてはいけない。
- artifact versionを修正する場合は同じversionを書き換えず、新versionを作る。
- `docs/archive/*`、obsolete/non-canonical docsはcontext builderで実装仕様として渡さない。渡す場合はuntrusted background referenceとして明示する。

## Planning Invariants

- Planning workerは `planning_runs`、`planning_artifacts`、`decision_report_drafts` だけを書ける。
- Planning workerは `artifacts`、`artifact_versions`、`tasks`、`task_groups`、`merge_queue_entries` を直接更新してはいけない。
- Planning artifactはcanonical artifactではない。実装仕様として扱うにはPlanning Consolidatorの採用とSerial Canonical Commitが必要。
- planning runは `artifact_snapshot_json` を必ず持つ。
- Consolidatorはsnapshotが古いplanning artifactを採用する前にrevalidateする。revalidate不能なら `stale` にする。
- `decision_report_drafts` はHuman Inboxに直接表示しない。batch化またはpromote後に `decisions` と `inbox_items` へ反映する。
- canonical artifact / task / roadmap更新はsingle writerで行う。同時canonical commitは拒否する。
- planning laneのbounded parallelはimplementation concurrencyを増やさない。
- `work_queue_items` は `lease_owner`、`lease_expires_at`、`last_heartbeat_at`、`attempt_no`、`max_attempts`、`idempotency_key` を持つ。
- `leased` / `running` itemはlease期限切れ時にrecoveryされ、attempt budget内なら `queued` へ戻す。
- `idempotency_key` が同じopen work itemを重複作成してはいけない。

## Task and Run Invariants

- `tasks.project_id` は必ず存在するprojectを指す。
- `tasks.current_run_id` が非NULLの場合、同じtask/projectの `runs.id` を指す。
- 同じtaskで `runs.status = 'running'` のrunは原則1つまでとする。並列runを許す場合はrun_typeごとに明示的なlockを持つ。
- TaskStatus変更は [state-machine.md](state-machine.md) のallowed transitionだけを許す。
- `runs.attempt_no` は同じtask_id/run_type内でUNIQUEにする。
- repair runの `repair_of_run_id` は同じtaskのrunを指す。
- `runs.base_commit` は空にしない。実装run開始時点のtarget branch commitを保存する。
- `runs.head_commit` はdiff収集後に設定する。diffが空の場合も空diff artifactを保存する。
- run artifactは `run_artifacts` にpathとcontent hashを保存する。
- human approval evidenceはrun artifact pathだけでなく `run_artifacts.id` と `content_hash` を参照する。
- approval後に対応run artifactのhashが変わった場合、そのapprovalは無効として扱い再承認を要求する。
- implementation / repair runは `implementation_environment_id` または `run_profile_id` から実行環境を解決できなければならない。verification / reverify runは1つのrunに複数environment resultを持てるため、各 `command_events.environment_id` と `verification_results.environment_id` から実行環境を解決する。
- verification resultは `environment_id` を必ず持つ。
- `required_for_merge=false` のoptional verification failureは、自動的にmerge blockにしてはいけない。Gate policyでREPORT_ONLYまたはHUMAN_DECISIONへ分類する。
- command_eventは `environment_id`、runner、cwd、argv、network_policy、exit_code、stdout/stderr artifactを持つ。

## Verification Invariants

- `verification_results.run_id` はverificationまたはreverify runを指す。
- 1つのverification / reverify runは複数environmentのverification_resultsを持てる。environment単位の区別は `verification_results.environment_id` と `command_events.environment_id` で表現する。
- verification commandはCodexではなくOrchestrator process runnerが実行する。
- verification resultはstdout/stderrのraw値をDBへ直接保存しない。redacted artifact pathだけを保存する。
- `failure_class` は `current_diff`、`environment`、`baseline`、`spec_gap`、`unknown` のいずれかに正規化する。
- `current_diff` と分類するには、head側失敗とbaseline側成功、またはdiffに紐づく明確なfailure signatureが必要。
- baseline verificationが実行不能な場合は `environment` または `unknown` として扱い、current diff起因と断定しない。
- baseline failureがあるtaskをmerge可能にするには、base/headのfailure signatureが同等で、current diffが悪化させておらず、Baseline Issue ReportまたはREPORT_ONLY gate resultが保存済みで、project policyが許可している必要がある。
- unclassified verification failure、current diff failure、またはbaseline failure regressionがあるtaskはmergeしてはいけない。

## Gate and Decision Invariants

- `gate_results.status` はGateResult actionであり、TaskStatusではない。
- `HUMAN_DECISION` のgate resultには未解決Decisionが1つ以上存在する。
- `HUMAN_INPUT` は通常Decisionを作らず、environment requirement/bindingまたは入力sourceへprojectionする。
- `HARD_BLOCK` は承認だけでは解除しない。解消条件、再計画、中止のいずれかをsource側に記録する。
- Decision ReportはOrchestrator-owned artifactであり、Coding Agentは書かない。
- Decision resolved時は、関連するopen inbox itemを同じtransactionでresolvedにする。
- Final ReviewとMerge ApprovalはDecisionではなく `human_approvals` をsource of truthにする。Manual Applyの標準source of truthは `patch_applications` にする。
- `human_approvals.evidence_json` には対象run、head commit、diff hash、verification result ids、gate result idsを含める。
- `approved_for_merge`、`queued_for_merge`、`patch_exported` へ進めるには、対象head commitに対するapproved final review approvalとapproved merge approvalが必要。
- `manually_applied` へ進めるには、`devos patch mark-applied` のhuman attestation、適用commit、exported patch hashを `patch_applications.evidence_json` に保存する必要がある。これはapprovalではない。
- approval対象のhead commitやpatch hashが変わった場合、既存approvalを再利用してはいけない。
- platform mismatch、path mapping failure、toolchain missingはEnvironment Input Cardへ分類しない。Gate actionが `HUMAN_INPUT` 相当の停止を返す場合でも、Inbox projectionは `toolchain_setup`、`platform_setup`、またはEnvironment Issue Reportにする。
- Windows admin elevation、registry変更、installer実行、certificate import、firewall/Defender変更は通常承認では進めずHARD_BLOCKまたはexplicit isolated/manual actionにする。

## Inbox Invariants

- `inbox_items` はprojectionであり、source of truthではない。
- `source_type/source_id` はGo validationで実在確認する。
- openな `human_decision` itemには未解決Decisionが必要。
- openな `approval` itemにはopenな `human_approvals`、またはattestation待ちの `patch_applications` が必要。
- openな `human_input` itemには未解決environment requirement/binding、または入力待ちsourceが必要。
- openな `hard_block` itemはdismiss不可。
- `REPORT_ONLY` itemはHuman waiting countに含めない。
- `snoozed` itemのsourceが重大化した場合はsnoozeを解除し、priorityを再計算する。

## Semantic Diff Invariants

- semantic diff itemはdiff evidenceを持たない場合、Human Reviewの上位表示に出さない。
- evidence fileは対象runの `diff.patch` に存在するfile pathでなければならない。
- deleted fileは `change_type: "deleted"` として扱い、line rangeが取れない場合はconfidenceを下げる。
- generated fileをevidenceに使う場合は `generated: true` を明示し、primary evidenceにしない。
- line rangeが取れない場合はconfidenceを `medium` または `low` にする。
- LLM review outputはJSON Schemaで検証し、schema外フィールドは保存しない。

## Command Event Invariants

- command eventはquery可能な `command_events` tableに構造化して保存する。少なくとも `environment_id`、runner、argv、shell経由かdirect execか、cwd、network_policy、exit code、stdout/stderr artifact、sandbox/approval event、network access疑い、path access疑いを持つ。
- 危険コマンド検出は単純文字列一致だけにしない。argv正規化、shell command分類、protected path access、network lane違反、worktree外writeを組み合わせる。
- destructive commandは通常Human Inbox承認ではなく `HARD_BLOCK`、またはisolated runner requiredとして扱う。
- Codex diffに `.devagent/prd.md`、`.devagent/architecture.md`、`.devagent/roadmap.yaml`、`.devagent/schemas/*`、`.devagent/policies/*`、`AGENTS.md` の変更が含まれる場合はDecision Gateへ送る。schema変更は `HARD_BLOCK`、policy / AGENTS.md / canonical artifact変更は少なくとも `HUMAN_DECISION` とする。

## Deletion and Cleanup Invariants

- worktree cleanupはdefault dry-run。
- 未merge diff、未保存diff artifact、untracked filesがあるworktreeは削除しない。
- `orchestrator-data` のrun artifactはtask削除と連動して物理削除しない。明示的なretention cleanupだけが削除できる。
- DBの削除は初期実装では原則soft deleteまたはstatus変更で表現し、証拠artifactとの参照を壊さない。

## Prompt and Retention Invariants

- `prompt.md` はsensitive artifactとして扱う。
- prompt、stdout JSONL、stderr、final message、summary、diff、review、decision reportは保存前にsecret scanとredactionを通す。
- UIはraw prompt/raw logsをデフォルト表示しない。
- retention periodをproject policyで管理する。初期値は未削除だが、削除機能を入れる場合はDecision Report対象にする。
- promptにrepository contentsを大量に含める場合、API key、cookie、token、個人情報らしき文字列は保存前にredactionする。
