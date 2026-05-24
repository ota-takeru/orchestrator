# CLI Design

## Goal

初期完成スコープでは、Web UIより先にCLIでコア体験を固めます。CLIだけで、コンセプト入力から実装委任、検証、レビュー、承認、反映まで完結する状態を先に作ります。

CLIはGoで実装します。初期版では標準ライブラリの `flag` から始め、サブコマンドや補完が複雑になった段階でCLIフレームワーク導入を検討します。

## Commands

```text
devos init "AIタスク管理アプリを作りたい"
devos spec
devos plan
devos artifacts approve ART-PRD --version 1 --status approved
devos artifacts approve ART-ARCH --version 1 --status approved_with_notes --notes "初期UIはHuman Inbox優先"
devos tasks materialize
devos bootstrap --adapter fake
devos tasks
devos run TASK-001
devos review TASK-001
devos review approve TASK-001
devos review reject TASK-001 --reason "acceptance criteria不足"
devos inbox
devos inbox approve INBOX-001
devos decisions
devos approve DEC-001 --option A
devos artifacts
devos request "Today Viewを追加して"
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
devos change request "タスク画面を今日の実行リスト中心に変える"
devos change analyze CR-001
devos change approve CR-001 --option A
devos memory --type policy
devos dependency risk add --name zod --manager npm --type production --reason "フォーム検証" --risk medium
devos dependency risk list
devos ui snapshot
devos serve
devos env status
devos env set OPENAI_API_KEY --scope project --value-stdin
devos merge approve TASK-001
devos merge TASK-001
devos merge queue
devos patch export TASK-001
devos patch status TASK-001
devos patch mark-applied TASK-001 --commit abc123
devos patch verify-applied TASK-001
devos cleanup --dry-run
devos cleanup --execute
devos cleanup --quarantine --execute
devos cleanup --delete --execute
devos cleanup --merged
devos cleanup --applied
devos cleanup --older-than 14d
devos publish --dry-run
devos publish --execute --remote origin --branch main
devos platform detect
devos platform profile list
devos platform profile set windows-primary
devos platform doctor
devos platform doctor --env windows-main --include-codex --include-ui --save
devos platform codex-readiness --save
devos platform codex-readiness --from-file windows-readiness.json --save
devos platform setup instructions INBOX-001
devos platform setup mark-installed INBOX-001
devos platform setup waive INBOX-001 --reason "sidecar only" --scope task --expiry 2026-06-30T00:00:00Z --allowed-effect report_only
devos platform map add windows-main wsl-sidecar --from-root C:\dev\app --to-root /mnt/c/dev/app --mode same_filesystem --write-owner windows-main
devos platform map list
devos verify TASK-001 --env windows-main
devos run TASK-001 --real-codex --verify --verify-env windows-main
```

## CLI Contract

CLIは人間向け表示を標準出力へ出します。自動化用途では全commandに `--json` を付けられるようにします。`--json` 時はstdoutをmachine-readable JSONだけにし、progressや診断はstderrへ出します。

共通flags:

| Flag | Meaning |
| --- | --- |
| `--project-root PATH` | 対象project root。省略時は現在ディレクトリから探索 |
| `--data-root PATH` | `orchestrator-data` root。省略時はproject policyまたはdefault |
| `--json` | stdoutをJSONに固定 |
| `--dry-run` | 変更予定だけを表示。cleanupやmerge系はdefault dry-runを持つ |
| `--yes` | 非危険な確認を省略。Decision Gate対象は省略不可 |
| `--timeout DURATION` | command全体のtimeout |

exit codes:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | validation error / invalid arguments |
| 2 | storage error / migration error |
| 3 | git error |
| 4 | policy block / hard block |
| 5 | verification failed |
| 6 | human input required |
| 7 | human decision required |
| 8 | external process timeout |
| 9 | internal error |

DB変更を伴うcommandは、1つのuser actionにつき1 transactionを基本にします。ファイル生成とDB更新を両方行う場合は、先に一時ファイルへ書き、検証後にrenameし、DB transaction内でpath/hashを保存します。途中失敗時はDBをrollbackし、作成済み一時ファイルをcleanup対象として記録します。

## Command Details

| Command | Arguments / Flags | Creates / Updates | Idempotency |
| --- | --- | --- | --- |
| `devos init` | `CONCEPT`, `--project-root`, `--data-root` | `.devagent/concept.md`、初期project record、policy skeleton | 同じprojectでは既存projectを検出し、上書きしない |
| `devos spec` | `--from CONCEPT_PATH`, `--json` | PRD artifact draft/proposed version | 同じinput hashなら新versionを作らない |
| `devos plan` | `--json` | architecture、roadmap、task draft | 承認済みartifactを勝手に上書きしない |
| `devos tasks materialize` | `--json` | approved artifactsからTask YAMLとcanonical taskをproposed/ready化 | artifact version hashが同じならno-op |
| `devos bootstrap` | `--adapter fake`, `--json` | init/spec/plan/approve/materialize/run/review/approval/merge dry-runの縦断検証 | 開発用。既存projectを壊さない |
| `devos tasks` | `--status`, `--json` | なし | read-only |
| `devos run` | `TASK_ID`, `--adapter fake|real-codex`, `--real-codex`, `--verify`, `--verify-adapter local|fake`, `--verify-env ENV_ID`, `--json` | run、workflow_events、run artifacts、任意でverification/gate evidence | real-codexはprimary environmentのCodex adapter policyに従う。Linux/WSLはLinux runtime、Windows nativeはWindows runtimeでのみ実行し、runtime不一致はblocked runとDecision/Inboxへ正規化する。network off、non-interactive。`--verify` 指定時は実装成功後にOrchestrator権威の検証へ接続する |
| `devos verify` | `TASK_ID`, `--adapter local|fake`, `--env ENV_ID`, `--json` | verification run、command_events、verification_results、gate_results、TaskStatus更新 | `verifying` のtaskだけ対象。v1 localはLinux/current envで `go.mod` 検出時に `go test ./...` をnetwork offで実行し、検証command不明ならGateで止める |
| `devos review` | `TASK_ID`, `--json` | review run、semantic diff、gate results | 対象head commitが同じなら再利用可 |
| `devos review approve` | `TASK_ID`, `--json` | `human_approvals(final_review)`、workflow_events、inbox resolved | 同じhead commitへの同じ承認はno-op |
| `devos review reject` | `TASK_ID`, `--reason`, `--json` | `human_approvals(final_review)` rejected、taskをrepair/needs_decisionへ戻す | reason必須 |
| `devos inbox` | `--status`, `--json` | projection repairのみ可能 | source of truthを変更しない |
| `devos inbox approve` | `INBOX_ID`, `--json` | inboxのsourceを解決し、正規sourceを更新 | source種別ごとの承認条件に従う |
| `devos decisions` | `--status`, `--json` | なし | read-only |
| `devos approve` | `DECISION_ID`, `--option`, `--notes` | decision resolved、workflow_events、inbox resolved | resolved decisionへの再approveはvalidation error |
| `devos artifacts` | `--type`, `--json` | なし | read-only |
| `devos artifacts trusted` | `--json` | なし | `approved_version_id` からhash検証済みartifact snapshot bundleを返す。`approved_with_notes` の `approval_notes` も含める |
| `devos artifacts check` | `--json` | なし | artifact lifecycleのcross-table invariantを検査し、違反を返す |
| `devos artifacts approve` | `ARTIFACT_ID`, `--version`, `--status`, `--notes` | artifact status、artifact version approval、workflow_events | 同じ承認内容はno-op |
| `devos check` | `--json` | なし | artifact / task / run / verification / run artifact fileのproject invariantを横断検査し、違反を返す |
| `devos env status` | `--json` | なし | read-only |
| `devos env set` | `KEY`, `--scope`, `--scope-id`, `--env`, `--value-stdin` | secret store or `.env.local`、redacted binding、audit event | 同じfingerprintならno-op |
| `devos request` | `TEXT`, `--json` | feature_request、work_queue_item | 同一本文でも新requestを作る |
| `devos requests` | `--status`, `--json` | なし | read-only |
| `devos queue` | `--status`, `--json` | なし | read-only |
| `devos plan start` | `--concurrency N`, `--json` | planning_runs、planning_artifacts、decision_report_drafts | 同じinput hashのrunning planning runがあれば重複開始しない |
| `devos plan status` | `--json` | なし | read-only |
| `devos plan consolidate` | `--json` | consolidation result、必要なinbox_items、canonical commit候補 | 同じsnapshotなら再利用可 |
| `devos plan checkpoint` | `--task TASK_ID`, `--json` | rolling_checkpoint planning_run、rolling_checkpoint_report | 同じtask状態snapshotなら再利用可 |
| `devos work start` | `--mode sequential`, `--planning-concurrency N`, `--implementation-concurrency 1`, `--until inbox`, `--budget DURATION`, `--json` | worker_run、work_queue_items、runs | 同じlaneのrunning worker制約に従う |
| `devos work status` | `--json` | なし | read-only |
| `devos work pause` | `WORKER_RUN_ID`, `--json` | worker_run status | paused workerへの再pauseはno-op |
| `devos work resume` | `WORKER_RUN_ID`, `--json` | worker_run status | running workerへのresumeはno-op |
| `devos change request` | `TEXT`, `--json` | change_request proposed | 同一本文でも新requestを作る |
| `devos change analyze` | `CR_ID`, `--json` | impact report、trace links | 同じartifact versionsなら再利用可 |
| `devos change approve` | `CR_ID`, `--option` | artifact update proposal、tasks再分類 | resolved requestへの再approveはvalidation error |
| `devos ui snapshot` | `--limit`, `--json` | なし | Human Inbox UIのdashboard snapshotをread-onlyで返す |
| `devos serve` | `--addr`, `--json` | なし | 同じDBをHTTP APIから公開する。UI/APIはsource of truthを直接編集せず正規repository APIへ委譲する |
| `devos merge approve` | `TASK_ID`, `--json` | `human_approvals(merge)`、`approved_for_merge` | final review approvalとevidence一致が必須 |
| `devos merge` | `TASK_ID`, `--dry-run`, `--json` | merge queue entry、workflow_events | open queue entryがあれば重複投入しない |
| `devos merge queue` | `--json` | なし | read-only |
| `devos merge queue --dry-run-real-git` | `--entry`, `--json` | real Gitの非破壊検査run、command_events、summary artifact | worktree dirty / missing commit / git errorを分類 |
| `devos merge queue --process-real-git` | `--execute`, `--ff-only`, `--no-push`, `--target main`, `--entry`, `--json` | local-only fast-forward、merge前reverify evidence、summary artifact、task/queueをmergedへ同期 | v1はpush禁止、conflict自動解決なし。candidateを一時worktreeでreverifyしてGate通過後だけtarget refを更新する |
| `devos patch export` | `TASK_ID`, `--json` | patch artifact、`patch_applications(exported)`、`patch_exported` | approved_for_merge必須 |
| `devos patch status` | `TASK_ID`, `--json` | なし | read-only |
| `devos patch mark-applied` | `TASK_ID`, `--commit SHA`, `--json` | `patch_applications(manually_applied)`、`manually_applied` | commit存在確認必須。approvalではなくhuman attestation |
| `devos patch verify-applied` | `TASK_ID`, `--json` | `reverify` run、verification_results、gate_results、`applied`またはneeds_decision | 同じcommitなら再利用可 |
| `devos cleanup` | `--dry-run`, `--merged`, `--applied`, `--older-than` | worktree cleanup plan | default dry-run |
| `devos cleanup --execute` | `--merged`, `--applied`, `--older-than` | cleanup execute guard、worktree safety evidence | 恒久削除や隔離移動を伴わないguardだけを証跡化する |
| `devos cleanup --quarantine --execute` | `--merged`, `--applied`, `--older-than`, `--quarantine-root` | cleanup quarantine evidence、worktree safety evidence、eligible worktreeの隔離移動 | `git worktree move` でDevOS管理下のquarantineへ移し、恒久削除はしない |
| `devos cleanup --delete --execute` | `--merged`, `--applied`, `--older-than` | cleanup delete evidence、worktree safety evidence、eligible worktreeの恒久削除 | `git worktree remove --force` を使う。未保存diff、untracked、diff artifact欠落、stale safety evidenceがある対象は削除しない |
| `devos cleanup quarantine list` | `--json` | なし | `cleanup-quarantine-summary.json` evidenceから隔離済みworktreeを一覧表示する |
| `devos cleanup quarantine restore` | `TASK_ID`, `--run RUN_ID`, `--json` | cleanup restore evidence、quarantine worktreeの復元移動 | `git worktree move` で元pathへ戻す。復元先が存在する場合はblocked |
| `devos publish` | `--remote origin`, `--branch main`, `--dry-run`, `--execute`, `--json` | publish readiness / execute run、summary artifact、workflow_events | defaultはreadiness証跡だけ。`--execute` はremoteがlocal behindでないことを確認し、`local_ahead` のときだけ明示refspecでpushする。`up_to_date` はno-op |
| `devos platform detect` | `--apply`, `--json` | Windows / WSL / Linux local environment候補 | `--apply`なしではDB更新しない |
| `devos platform profile list` | `--json` | なし | read-only |
| `devos platform profile set` | `windows-primary|wsl-primary|hybrid` | canonical_operationsを含むproject_run_profileをactive/default化 | profile snapshotが同じならno-op |
| `devos platform doctor` | `--env ENV_ID`, `--include-codex`, `--include-ui`, `--save`, `--json` | toolchain_requirements検査、setup card projection | 同じ検査結果ならno-op |
| `devos platform codex-readiness` | `--from-file PATH`, `--save`, `--json` | Codex runtime readiness report、runner capability issue projection、toolchain_required環境のCodex doctor report | `--save`なしではread-only。`--from-file` は別runtimeで生成したreadiness JSONを取り込む |
| `devos platform map add` | `FROM_ENV`, `TO_ENV`, `--from-root`, `--to-root`, `--mode` | path_mappings作成 | validation failureなら保存しない |
| `devos platform setup instructions` | `INBOX_ID`, `--json` | Toolchain Setup Cardの手動セットアップ手順表示 | 自動インストールはしない |
| `devos platform setup mark-installed` | `INBOX_ID`, `--json` | doctor再実行、toolchain_requirements / setup card同期 | doctorが検出した場合だけresolved |
| `devos platform setup waive` | `INBOX_ID`, `--reason`, `--scope`, `--expiry`, `--allowed-effect`, `--json` | approved decision、toolchain requirement waived、setup card resolved | reason/scope/expiry/allowed-effect必須。`allow_merge_without_toolchain` を明示したwaiverだけmerge向けwaiveとして扱う |
| `devos platform map list` | `--json` | なし | read-only |

`devos init "concept"` の初期生成物:

platform command details:

- `devos platform detect` はWindows / WSL / Linux local environmentを検出し、execution_environments候補を作る。DB更新は `--apply` なしではしない。
- `devos platform profile set` はcanonical_operationsを含むproject_run_profileをactive/defaultにする。
- `devos platform doctor` はtoolchain_requirementsを検査し、missing/setup_requiredをHuman Inboxへ投影する。
- `devos platform doctor --include-ui` はUI検証用のNode.js / Corepackを検査する。pnpmは `ui/package.json` の `packageManager` をCorepack経由で実行するため、正規検証コマンドは `corepack pnpm --dir ui test` / `lint` / `build` とする。
- `devos platform codex-readiness --save` はruntime不一致をRunner Capability Issueとして投影し、runtimeは利用可能だがCodex CLI / auth / sandbox系toolchainが不足する環境では `devos platform doctor --include-codex` 相当のToolchainRequirementも保存する。
- `devos platform codex-readiness --from-file windows-readiness.json --save` は、Windows上のDevOS runtimeで生成したreadiness JSONを別環境のDBへ取り込む。import時はschema validationとenvironment id照合を行い、Windows nativeでreadyになったenvironmentのopen runtime issueを解消できる。
- `devos platform map add` はpath_mappingsを作成する。mapping後のpath validationを通らない場合は保存しない。

```text
.devagent/
  concept.md
  policies/project-policy.yaml
  schemas/

orchestrator-data/
  projects/{project_id}/
```

PRD、Architecture、Roadmap、Task YAMLは `devos spec` / `devos plan` / artifact approval flowで生成・承認します。`init` だけで実装可能状態にしません。

責務分担:

- `devos init "concept"` はproject record、`.devagent/concept.md`、policy skeleton、`.devagent/schemas/` schema registry copy、preflight reportだけを作る。
- `devos init` はproject rootからhost environmentを検出し、execution_environment候補を作る。
- `devos init` はprimary_environmentを確定できない場合、Human InboxまたはCLI promptで選択させる。
- `devos init` は `--profile windows-primary|wsl-primary|hybrid` を受け付ける。
- `devos spec` はPRD draft/proposed artifactを作る。
- `devos plan` はArchitecture draft、Roadmap draft、Task YAML draftを作る。
- `devos artifacts approve ...` はartifact versionの承認状態を保存する。
- `devos tasks materialize` は承認済みartifactからcanonical Taskを `proposed` または `ready` にする。
- `devos bootstrap --adapter fake` は開発用の縦断commandであり、`init` に生成、承認、ready化、mergeまで背負わせない。
- `devos bootstrap --adapter fake` はCodex CLIやCodex authが未設定でも通る。`--adapter codex` のときだけCodex preflightをhard failureにする。

error behavior:

- validation errorはDBを書き換えない。
- policy blockはDecision Gate / inbox sourceを保存してからexit code 4で終了する。
- `HUMAN_INPUT` はexit code 6、`HUMAN_DECISION` はexit code 7を返す。
- `--json` ではerrorも `{ "error": { "code": "...", "message": "...", "details": ... } }` の形にする。

secret input:

- `devos env set KEY --scope project` はTTY password promptで値を入力する。
- automationでは `devos env set KEY --scope project --value-stdin` を使い、stdinから値を読む。
- `--value` flagは初期実装では禁止する。shell history、process list、logsへsecretを残さないため。
- `--json` 時もsecret値をstdout/stderrへ出してはいけない。responseはredacted metadataだけにする。

`devos env` と `devos platform` の責務:

- `devos env` は環境変数/secretを扱う。
- `devos platform` は実行環境、toolchain、path mappingを扱う。
- この2つを混ぜてはいけない。Visual Studio、MSBuild、Windows SDK、bubblewrap、Gitなどは `devos env` ではなく `devos platform doctor` とToolchain Setup Cardで扱う。

## Responsibilities

- `.devagent/` 初期化
- SQLiteへの登録
- Markdown/YAML成果物の生成
- タスク状態遷移
- Codex CLI実行
- JSONLログ保存
- diff保存
- failure diagnosis
- auto repair / auto replan
- Decision Gate判定
- レビュー結果表示
- Human Inbox表示
- Change Request影響分析
- Feature Request受付
- Request Queue / Work Queue管理
- bounded parallel planning
- planning consolidation
- serial canonical commit
- sequential worker実行

## Repository Commands

このリポジトリ自身の検証コマンドは、実装開始後に次を基本にします。

```text
go test ./...
pnpm --dir ui test
pnpm --dir ui lint
pnpm --dir ui build
```

## Generated Artifacts

```text
.devagent/
  concept.md
  prd.md
  architecture.md
  roadmap.yaml
  tasks/
    TASK-001.yaml
  decisions/
  memory/

orchestrator-data/
  projects/{project_id}/runs/{run_id}/
```

## Task Execution Flow

```text
1. 次のREADYタスクを選ぶ
2. Git worktreeを作る
3. タスク用プロンプトを生成する
4. Codexに実装させる
5. JSONLイベントを保存する
6. Orchestrator process runnerがverification command、lint、buildを実行する
7. 失敗したら原因分類する
8. 現diff起因で自動修復可能ならrepair runを実行する
9. タスクが大きすぎる場合は自動分割または再計画する
10. 差分を取得する
11. Decision Gateを実行し、PASS / AUTO_REPAIR / AUTO_REPLAN / REPORT_ONLY / HUMAN_INPUT / HUMAN_DECISION / HARD_BLOCKへ振り分ける
12. レビュー用Codex実行を別途走らせる
13. Decision Reportが必要なら証拠付きで作成する
14. 問題なければHuman Inboxにfinal review approval待ちとして提示し、`human_approvals` をsource of truthにする
15. final reviewとmergeが承認されたらmerge queueへ入れる、または手動適用用patchをexportする
16. 対象worktreeを最新mainへrebaseまたはmergeする
17. conflictがあれば自動修復またはHuman Inboxへ出す
18. verificationを再実行する
19. Decision Gateを再評価する
20. 問題なければmainにmergeする。手動適用経路の場合は適用commitを検証済みとして `applied` にする
21. roadmap、policy、memoryを更新する
```

## Request and Worker Commands

`devos request` は、自然文の機能要望をFeature Requestとして保存します。

`devos plan start` は、Feature Requestの詳細化、影響分析、Decision Report draft、Task Group proposal、Risk Report作成をbounded parallelで開始します。planning workerはcanonical artifactやtaskを直接変更せず、planning artifactだけを作ります。

`devos plan` 単体は初期PRD / Architecture / Roadmap / Task draft生成を扱います。`devos plan start|status|consolidate|checkpoint` はFeature Request Queue向けの非同期planning laneを扱います。

`devos plan consolidate` は、planning artifactをsingle writerで統合し、重複、依存、snapshotの古さ、decision batchingを整理します。必要ならHuman Inbox itemを作り、承認不要なものだけcanonical commit候補にします。

`devos plan checkpoint` は、指定taskの現在状態、Task Group、work queue、planning artifact集計を `rolling_checkpoint_report` として保存します。checkpointはcanonical artifactやtask statusを直接更新しません。

`devos work start` は、planning lane、consolidation lane、execution lane、merge laneを内部的に処理できます。ただしimplementation concurrencyとmerge concurrencyは初期完成スコープでは必ず1です。

`devos run TASK-001` は単一taskのmanual/debug executionとして残します。通常の非同期運用では `devos request` と `devos work start` を使います。

worker flow:

1. queued Feature Requestを読む。
2. planning laneでfeature detail / impact analysis / decision draft / task proposalを並列生成する。
3. consolidation laneでplanning結果を統合する。
4. 必要ならHuman Inboxへbatch decisionを出す。
5. canonical commitをserialに行い、Change Request、Task Group、Taskへ反映する。
6. READY taskを選ぶ。
7. implementation / verification / repair / review / gateをsequentialに進める。
8. 人間判断が必要ならHuman Inboxへ出す。
9. 依存しないwork itemがあれば継続する。
10. merge承認済みtaskをmerge queueでsequentialに処理する。

## State Model

```ts
type ProjectLifecycleStatus =
  | "concept"
  | "spec_ready"
  | "roadmap_ready"
  | "implementing"
  | "blocked"
  | "complete";

type ProjectArchiveStatus = "active" | "archived";

type TaskStatus =
  | "proposed"
  | "ready"
  | "implementing"
  | "verifying"
  | "diagnosing"
  | "repairing"
  | "reviewing"
  | "needs_input"
  | "needs_decision"
  | "blocked_on_environment"
  | "blocked_on_policy"
  | "ready_for_human_review"
  | "approved_for_merge"
  | "queued_for_merge"
  | "rebasing"
  | "reverifying"
  | "merge_conflict"
  | "patch_exported"
  | "manually_applied"
  | "merged"
  | "applied"
  | "failed"
  | "cancelled";
```

`ProjectLifecycleStatus` は `projects.lifecycle_status` に保存します。`ProjectArchiveStatus` は `projects.archive_status` に保存します。workflow進捗とarchive状態を `projects.status` という1列で混在させてはいけません。

TaskStatusは [state-machine.md](state-machine.md) の正規一覧と [storage-schema.md](storage-schema.md) のCHECK制約に合わせます。`draft`、`running`、`blocked`、`done` は曖昧なので使いません。人間承認前は `ready_for_human_review`、承認後は `approved_for_merge`、merge queue投入後は `queued_for_merge`、反映後は `merged` にします。手動適用経路では `patch_exported`、`manually_applied`、`applied` を使います。
許可される遷移は [state-machine.md](state-machine.md) を正とし、CLIはallowed transitionにない状態変更を拒否します。

初期artifact承認:

```text
devos artifacts
devos artifacts trusted --json
devos artifacts check --json
devos artifacts approve ART-PRD --version 1 --status approved
devos artifacts approve ART-ARCH --version 1 --status approved_with_notes --notes "SQLite/local-firstは維持"
devos artifacts approve ART-ROADMAP --version 1 --status approved
```

PRD、Architecture、Roadmapが `approved` または `approved_with_notes` になるまで、Task YAMLは `proposed` のままにし、`ready` へ遷移させません。

merge前の正規遷移:

```text
ready_for_human_review
  -> approved_for_merge
  -> queued_for_merge
  -> rebasing
  -> reverifying
  -> merged
```

`devos review approve TASK` はfinal_review approvalをapprovedにします。この時点ではtaskは `ready_for_human_review` のままでもよいです。`devos merge approve TASK` はmerge approvalをapprovedにします。同一head commit / diff hash / verification evidenceに対してfinal_reviewとmergeの両方がapprovedなら、同じtransactionで `approved_for_merge` へ進めます。head commit、diff hash、verification result、gate resultが変わったら両approvalを再要求します。

`rebasing` でconflictが出た場合は `merge_conflict` にし、軽微なconflictはauto repair、判断が必要なconflictはHuman Inboxの `merge_conflict` itemとして出します。

手動適用の正規遷移:

```text
ready_for_human_review
  -> approved_for_merge
  -> patch_exported
  -> manually_applied
  -> reverifying
  -> applied
```

手動適用はmergeの代替完了経路ですが、人間のcommit登録だけでは完了扱いにしません。`devos patch verify-applied` がpatch一致確認、verification、Gate再評価を通してから `applied` にします。

`devos patch verify-applied` が作るrunは `run_type = "reverify"`、`reverify_context_type = "patch_application"` です。merge queueの再検証も同じ `reverify` run typeを使い、`reverify_context_type = "merge_queue_entry"` で区別します。

`devos patch mark-applied` は「このcommitへpatchを適用した」というhuman attestationを保存する操作で、approvalではありません。初期標準pathではManual Apply Approvalを別に要求せず、`patch_applications` をsource of truthにします。project policyが追加承認を要求する場合だけ、verify-applied後に `human_approvals(manual_apply)` をfinal acknowledgementとして使えます。

## Run Attempt Model

`runs` は同じtaskに複数作られます。実装run、repair run、review runを区別し、停止理由と次アクションを保存します。

```ts
type Run = {
  id: string;
  task_id: string;
  run_type:
    | "implementation"
    | "repair"
    | "verification"
    | "review"
    | "replan"
    | "rebase"
    | "reverify"
    | "merge"
    | "patch_export"
    | "cleanup"
    | "worktree_safety";
  attempt_no: number;
  repair_of_run_id?: string;
  reverify_context_type?: "merge_queue_entry" | "patch_application";
  reverify_context_id?: string;
  status: "pending" | "running" | "succeeded" | "failed" | "cancelled" | "timed_out" | "blocked";
  stop_reason?: "verification_failed" | "budget_exhausted" | "human_required" | "hard_block" | "timeout" | "approval_event" | "schema_validation_failed";
  next_action?: "auto_repair" | "auto_replan" | "human_input" | "human_decision" | "environment_input" | "environment_issue_report" | "baseline_issue_report" | "ready_for_review" | "merge";
};
```

RunStatusの許可遷移は [state-machine.md](state-machine.md) を正とします。同じrunをterminal stateから再利用せず、新しいattemptを作ります。`blocked` になったrunも初期標準pathでは `running` に戻さず、人間入力や判断後に新しいattemptを作ります。

## Run Policy

```yaml
run_policy:
  max_implementation_attempts: 3
  max_repair_attempts: 2
  max_total_runtime_minutes: 30
  auto_split_if_diff_exceeds:
    changed_files: 12
    added_lines: 800
  auto_repair_allowed_when:
    - test_failure_caused_by_current_diff
    - lint_failure
    - type_error
    - missing_generated_file
  human_decision_required_when:
    - requirement_change
    - new_external_service
    - auth_or_permission_change
    - db_schema_change
    - production_dependency
    - personal_data_storage
```

## Merge Blocking Conditions

タスクは次の状態では `merged` にできません。

- current diff起因のverification failureが未解決
- verification failureが未分類
- baseline failureがcurrent diffで悪化している
- baseline failureにBaseline Issue Report、waiver、またはREPORT_ONLY gate resultが記録されていない
- 未解決のHuman Inputがある
- 未解決のDecisionがある
- diffが空でないのにレビューが未完了
- Decision Gateで `HUMAN_INPUT`、`HUMAN_DECISION`、`HARD_BLOCK` が残っている
- 自動修復予算を使い切った失敗が分類されていない
- acceptance criteria が未確認
- 最新mainへのrebaseまたはmergeが未実行
- merge前reverificationが未実行、または失敗している
- merge conflictが未解決

次の条件をすべて満たす場合、head側verificationにbaseline failureが残っていてもmergeを許可できます。

- head verification failureが `baseline` に分類済み
- base/headの `failure_signature` が同等
- current diffが新しいfailureを追加していない
- Baseline Issue ReportまたはREPORT_ONLY gate resultが保存済み
- project policyが既知baseline issue付きmergeを許可している

## Worktree Cleanup

worktree cleanupは危険操作なので、defaultはdry-runです。

```text
devos cleanup --dry-run
devos cleanup --execute
devos cleanup --quarantine --execute
devos cleanup --delete --execute
devos cleanup --merged
devos cleanup --applied
devos cleanup --older-than 14d
```

削除条件:

- `--dry-run` では削除予定だけを表示する。
- `--execute` 単体では削除せず、guard結果と `actual_delete_enabled=false` を証拠保存する。
- `--quarantine --execute` はeligible worktreeをDevOS管理下のquarantine rootへ移動し、後から `devos cleanup quarantine restore` で復元できる。
- `--delete --execute` はeligible worktreeを恒久削除する。`--delete` と `--quarantine` は同時指定できない。
- 未merge diffがあるworktreeは削除しない。
- 削除前に `diff.patch` がOrchestrator-owned artifactとして保存済みであることを確認する。
- untracked filesがあるworktreeは、人間が明示しない限り削除しない。
- `merged`、`applied`、`cancelled`、`failed` のworktreeだけを対象にする。

## Publish

mainのpublishはmergeとは別commandです。`devos merge queue --process-real-git --execute` はlocal-only / no-pushのままにし、remote更新は `devos publish` が担当します。

```text
devos publish --dry-run --remote origin --branch main
devos publish --execute --remote origin --branch main
```

publish条件:

- defaultは `--dry-run` としてremote URL、local OID、remote OID、local/remote関係、blockerだけを証跡化する。
- `--execute` はremote branchが存在しない、またはremoteがlocalのancestorである場合だけpush可能にする。
- `remote_ahead`、`diverged`、local branch未存在、remote URL未設定、push後OID不一致はblockedとしてevidenceを残す。
- push実行後は `git ls-remote` でremote OIDがlocal OIDへ更新されたことを確認する。
