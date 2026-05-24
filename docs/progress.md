# Implementation Progress

この文書は、このリポジトリ自体の実装進捗を [implementation-plan.md](implementation-plan.md) のsliceに沿って確認するためのトラッカーです。単なる時系列ログではなく、計画に対して何が完了し、何が未着手かを見えるようにします。

これはOrchestratorが将来保存するrun artifact、verification result、gate result、Decision Reportではありません。正規仕様は [index.md](index.md) の "Canonical Implementation Docs" を優先します。

## Summary

最終更新: 2026-05-24

| Area | Progress | Status |
| --- | ---: | --- |
| Documentation baseline | 100% | 完了 |
| Codex implementation operating docs | 100% | 完了 |
| Product implementation code | 99% | 着手 |
| Initial Complete Scope end-to-end workflow | 99% | 進行中 |

## Latest Implementation Update

2026-05-24:

- Windows実機ランタイムで `devos.exe` を実行し、Windows filesystem上の一時cloneに対して `devos init`、`devos platform profile set windows-primary`、`devos platform doctor --include-codex --include-ui --save`、`devos platform codex-readiness --save` を確認しました。
- Windows profile設定時に `windows-main.project_root` が実project rootではなく `C:\fake\project` へ置き換わる不備を修正し、Windows native readiness reportが実際のWindows repo pathを保存することを確認しました。
- Windows native readiness JSONをUTF-8 reportとして出力し、WSL側のHybrid一時projectへ `devos platform codex-readiness --from-file ... --save` でimportできることを確認しました。
- Windows実機上で `devos bootstrap --adapter fake --profile windows-primary`、`devos check`、`devos inbox` を実行し、fake縦断workflowが `TASK-001` merge済み、violationsなし、open Inboxなしで完了することを確認しました。
- Windows側のGit、PowerShell、Node.js、Corepack、Codex authは検出済みです。Windows native Codex CLI executableはPATH上で未検出のため、real Codex implementation runはToolchain Setup Card / Runner Capability Issueで `codex:setup_required` として正しく停止することを確認しました。
- 追加・更新検証: `go test ./...`、`corepack pnpm --dir ui lint`、`corepack pnpm --dir ui test`、`corepack pnpm --dir ui build`。
- Slice 7 の残差分として、Real Codex promptをargvではなくstdin入力へ変更し、command evidenceには `codex exec -` のargvだけを保存するようにしました。
- Slice 8+ のplanning laneを補完し、Feature RequestからFeature Detail Report、Impact Analysis Report、Decision Report Draft、Task Group Proposal、Risk Reportを生成するようにしました。Consolidatorはstale snapshotをcanonical commitへ進めず、Decision Draftをbatch化してからTask Group / proposed taskを作成します。
- Change Requestのimpact analysisで `trace_links` を保存し、Change Requestからplanning run / planning artifactへの証跡を追えるようにしました。
- `devos env set --value-stdin` の保存先を `.env.local` へ反映し、SQLiteにはsecret値を保存せずredacted metadataとfingerprintだけを保存するようにしました。
- Human Inbox UIを再確認し、未接続だったRequest Queue、Work / Planning status、Task一覧、Change Request、Dependency Risk、Environment Inputをdashboardへ追加しました。対応するHTTP APIとして `/api/tasks`、`/api/requests`、`/api/queue`、`/api/work/status`、`/api/planning/status`、`/api/change-requests`、`/api/dependency-risks`、`/api/env/bindings` を追加しました。
- `docs/ui-human-inbox.md` に実装済みdashboard panelとAPI surfaceを追記し、`docs/cli-design.md` のコマンド例を現在の `devos` usageに合わせて更新しました。
- 追加・更新検証: `go test ./...`、`corepack pnpm --dir ui lint`、`corepack pnpm --dir ui test`、`corepack pnpm --dir ui build`。
- WSL2上のこのリポジトリを実プロジェクトとして扱い、`devos bootstrap --adapter fake --profile wsl-primary` で `TASK-001` がmerge前reverifyを経て `merged` になることを確認しました。`devos check` はviolationsなし、`devos platform codex-readiness --save` は `wsl-main` / `codex-wsl` をreadyと判定しました。
- WSL実プロジェクト運用向けに、依存物ディレクトリ配下のsymlinkをpreflight noiseから除外し、完了済みtaskに残ったstale execution queue itemを `devos work start` で自動完了するようにしました。
- WSL実プロジェクトDBはopen Inbox / queued work / open decision / open merge queueが0件になりました。`devos serve` の主要API、`devos publish --dry-run`、`devos cleanup --dry-run --merged`、`devos platform doctor --include-codex --include-ui --save`、`devos platform codex-readiness --save` を確認済みです。
- 追加・更新検証: `go test ./...`、`corepack pnpm --dir ui lint`、`corepack pnpm --dir ui test`、`corepack pnpm --dir ui build`。

残る完了判定は、Windows native Codex CLIをWindows PATHへ導入した後のreal Codex implementation runです。現状はToolchain Setup Cardにより `codex:setup_required` として正しく停止します。

## Slice Progress

| Slice | Scope | Progress | Status | Evidence | Next |
| --- | --- | ---: | --- | --- | --- |
| 0 | Canonical Docs and Authority | 100% | 完了 | `474b591`, `8413a1d`, `7fad1c4` | context builderを実ワークフローへ接続する |
| 0.25 | Platform Model Docs | 100% | 完了 | `474b591` | platform modelを実装sliceでDB / runner contractへ反映する |
| 0.5 | Project Trust / Platform-aware Preflight | 98% | 進行中 | `c77f689`, `11603b3`, `b4129d6`, `de0d26f`, `dfe3fdf`, `bf80a4e`, `98a154c`, `f2081eb`, `80abbe6` | preflight/path mapping evidenceの運用検証へ進む |
| 1 | Core Storage, Platform Tables, State Machines | 98% | 進行中 | `c77f689`, `ff4e1bb`, `90d6cb0`, `de0d26f`, `bb9e6e1`, `8f8d83e`, `ab22a6f`, `63132bf`, `799354b`, `538d771`, `5b72158`, `0bbd6e9`, `c79bd76`, `d77433c`, `fd68d94`, `ac3521c`, `50221e6`, `d0841e5`, `623ad4e`, `2e99d79`, `22a46b9`, `853446f`, `20520f7`, `44d00a0`, `d5e2a80`, `276e201`, `ba1dfac`, `65d0f2e`, `80abbe6`, `18e3bd0`, `0cf8bb1`, `756bb0e` | Windows実機依存を除く追加invariantは運用検証へ進む |
| 1.5 | Schema Registry and Validation | 92% | 進行中 | `6e9bc7b`, `e11e947`, `414e79d`, `c9e8f76`, `402d04a`, `dfccfff`, `13e56c3`, `cb1caaa`, `18e3bd0`, `0cf8bb1` | 追加schema validation対象の運用検証へ進む |
| 2 | Artifact Lifecycle + Approval | 100% | 完了 | `538d771`, `6dcf191`, `5b72158`, `b66b705`, `e11e947`, `cfeda29`, `4d63a15`, `e978cca`, `c454e3e`, `fe4c76c`, `4c6f301`, `112e344`, `5d25cfd`, `e5e9658`, `276e201`, `ba1dfac` | higher workflow slicesでartifact監査を運用する |
| 2.25 | Runner and Platform Foundation | 77% | 進行中 | `019d1a9`, `f2309f3`, `bb9e6e1`, `799354b`, `642dbc0`, `e7ad761`, `065a1af` | runner capability issue projectionの実運用検証へ進む |
| 3 | Fake Run Workflow with Fake Platform Runners | 95% | 進行中 | `f2309f3`, `bb9e6e1`, `5b72158`, `d387305`, `53abe8d`, `1fd2c5e`, `5867765` | fake workflowの実運用検証へ進む |
| 2.5 | Environment-aware Git / Worktree / Patch Foundation | 72% | 進行中 | `642dbc0`, `37c10c2`, `06dd2ca`, `9840a43`, `6a87f34`, `98a154c`, `f2081eb`, `80abbe6`, `18e3bd0`, `182ce10` | mirrored cloneの運用拡張と実プロジェクトrun検証へ進む |
| 4 | Environment-aware Verification / Baseline / Gate | 72% | 進行中 | `f2309f3`, `bb9e6e1`, `8f8d83e`, `862ffe5`, `6fb1fd7`, `49ce487`, `fd68d94`, `8e2252f`, `5fbe86a`, `cfeda29`, `37ba640`, `13e56c3`, `e7ad761`, `5867765`, `18e3bd0` | baseline regressionの追加運用検証へ進む |
| 5 | Human Inbox + Approval Sources + Toolchain Setup | 99% | 進行中 | `dfe3fdf`, `a98622b`, `ab22a6f`, `5d52d90`, `02be517`, `6d02ed6`, `46ea815`, `f1a3a0f`, `7c82bb3`, `bf80a4e`, `9840a43`, `6a87f34`, `47aace5`, `7a0ab49`, `df52592`, `6fb1fd7`, `0851cf1`, `82ffff3`, `5d8ca13`, `e0e8a38`, `03e31ca`, `853446f`, `82431c8`, `99859d2`, `2418ea0`, `4d63a15`, `06c105f`, `37ba640`, `77f64ef`, `065a1af`, `dda2f96`, `55af214`, `756bb0e` | setup card / merge gateの実運用検証へ進む |
| 6 | Merge Queue + Reverify | 100% | 完了 | `63132bf`, `acd52bc`, `1fd2c5e`, `0bbd6e9`, `c79bd76`, `d77433c`, `989becc`, `31c099a`, `dd44f1b`, `117b6cc`, `1bf94f1`, `642dbc0`, `27cac3e`, `37c10c2`, `82b63f1`, `862ffe5`, `e2fd3c4`, `ac3521c`, `cf17193`, `6823d44`, `18e3bd0` | publish前確認はhigher workflowで運用する |
| 7 | Real Codex Windows / WSL Execution | 90% | 進行中 | `d4e790a`, `862ffe5`, `750c16a`, `580bf87`, `0851cf1`, `220a406`, `82ffff3`, `5d8ca13`, `ad0a1bc`, `faf0c7c`, `085da18`, `1c523a8`, `dca6533`, `82431c8`, `e7ad761`, `cb1caaa`, `dda2f96`, `e3ba9e1`, `182ce10` | Windows native Codex CLI導入後のreal Codex implementation runへ進む |
| 8+ | Auto Repair, Semantic Diff, Change Request, Planning Queue, UI | 99% | 進行中 | `dd44f1b`, `1a5af75`, `f983158`, `08b5a03`, `423e7dc`, `50221e6`, `d0841e5`, `623ad4e`, `2e99d79`, `817daec`, `a451d3f`, `dd2820a`, `c4e464b`, `40c4e60`, `22a46b9`, `adfe423`, `853446f`, `425e4cd`, `935d658`, `20520f7`, `414e79d`, `34b42b5`, `c9e8f76`, `1c523a8`, `44d00a0`, `d5e2a80`, `402d04a`, `dca6533`, `832ef45`, `4ee303f`, `99859d2`, `dfccfff`, `2418ea0`, `d0ed17c`, `451ec70`, `77f64ef`, `18e3bd0` | Windows native Codex CLI導入後のreal Codex implementation runへ進む |

## Current Focus

現在の実装対象は Slice 0.5、Slice 1、Slice 1.5、Slice 2、Slice 2.25、Slice 2.5、Slice 3、Slice 4、Slice 5、Slice 6、Slice 7、Slice 8+ です。Go module、`devos` CLI入口、canonical docs context filter、project root検出、platform-aware preflight、platform enum、WSL host detection、主要state machine API、PathMappingServiceの最小実装、Orchestrator-owned schema registry、`.devagent/schemas/` hash付きcopy、schema registry checksum preflight、schema registry mismatch repair、Task YAML ready化前Go validation、semantic behavior diff schema copy / validation、dependency risk ledger schema copy / validation、Human Inbox snapshot schema copy / validation、GateResult schema copy / validation、toolchain doctor skeleton、environment-specific `CODEX_HOME` / `codex-auth` 検出、WSL2 requirement、`platform doctor --env`、UI検証向けのNode/Corepack doctor検査、SQLite migration registryと001/002/003/004/005/006/007/008/009/010/011/012/013/014 DDL、work_queue_items、planning_runs、planning_artifacts、decision_report_drafts、task_group planning_unit、worker_runs、environment_bindings / audit、semantic_behavior_diffs category/summary/confidence、policy memories、dependency risk ledger、SQLite接続/migration apply、project init永続化、artifact versioning / approval、approved_with_notesの承認メモを含むtrusted artifact context読取API、artifact version content snapshotとhash検証付きtrusted content bundle、artifact lifecycle invariant監査、project invariant横断検査、RunProfile/primary environment invariant、task verification command invariant、command_event argv_json invariant、GateResult schema DB invariant、Inbox projection source invariant、`devos check`、`/api/check`、Human Inbox UIのProject Check表示、`devos artifacts trusted`、`/api/artifacts/trusted`、Human Inbox UIのTrusted Artifacts表示、新artifact承認時の旧approved version supersede、artifact review idempotency、Real Codexプロンプトへのtrusted artifact context接続、stale run artifact hashによるHuman Approval無効化、unresolved blocking GateResultによるapproval拒否、`devos artifacts`、approved artifactからのtask materialize、Task YAML由来verification_commands保存、materialized taskのexecution queue投入、work startによるfake execution queue処理、auto repair queue投入/処理、work queue lease recovery、planning consolidationによるproposed task生成、rolling checkpoint report保存、Human Inbox UI snapshot、baseline issue memoryのsnapshot/API可視化、React/Vite Human Inbox UI scaffold、pnpm lock、Corepack経由のUI lint/test/build検証、local HTTP API surface、Inbox approve API、Decision option API/UI approve、Inbox projection/source-of-truth invariant、toolchain setup card projection、toolchain setup card解消同期、Toolchain Setup Card手順表示/mark-installed/waive、`/api/platform/toolchain-setup`、Human Inbox UIのSetup Cards表示、merge blocker付き`/api/merge/status`、Human Inbox UIのMerge Gate表示、waiver expiry失効とmerge blocker再接続、preflight platform setup card projection、path mapping issue projection、Codex runtime readiness issue projection、未初期化Codex readinessの明示エラー、GateResultのreport/human_input/human_decision/hard_block Inbox投影、baseline verification failureのREPORT_ONLY扱い、baseline issue memory保存、baseline issue reportがないrunのapproval拒否、`devos inbox`、`devos inbox approve`、`devos decisions`、`devos env status/set`、Human Approval source、`devos review` semantic behavior diff生成、`devos review reject`、`devos approve --remember`、`devos memory`、`devos dependency risk add/list`、`devos ui snapshot`、`devos serve`、retry decision承認後のtask再実行復帰、`devos request`、`devos requests`、`devos queue`、`devos change request`、`devos change analyze`、`devos change approve`、`devos plan start`、`devos plan status`、`devos plan consolidate`、`devos plan checkpoint`、`devos work start`、`devos work status`、`devos work pause/resume`、merge queue entrypoint、Runner interfaceとfake Windows/WSL/Linux runner、LocalRunner、Linux/WSL primary local verification、複数environment対応のverification runner foundation、command output artifact保存、command event / verification result永続化、verification command失敗のcurrent_diff分類、GateResult evaluator / 永続化、`devos verify`、fake implementation run、Windows-primary/WSL-primary/Hybrid fake verification、optional sidecar REPORT_ONLY検証、required sidecar block検証、RunProfile implementation environment解決に対応したLinux/WSL/Windows runtime policy付きのReal Codex Adapter、Real Codex dry-run preview、`devos platform codex-readiness --save`、runtime不一致時のblocked run / Decision / Inbox正規化、Real Codex実行前のtoolchain hard preflight、Real Codex run summaryへのenvironment/codex adapter/sandbox/CODEX_HOME source metadata保存、Git repoでのtask worktree作成付き`devos run --real-codex --verify`、fake merge queue worker、`devos bootstrap --adapter fake`、`TestBootstrapFakeTaskMerges`、manual patch application repository、`devos patch export/status/mark-applied/verify-applied`、`runs.reverify_context_*` 保存、fake merge conflict handling、`devos merge --dry-run`、`devos merge queue --dry-run-real-git`、一時worktreeでのmerge前reverification付きlocal-only/ff-only/no-pushの`devos merge queue --process-real-git --execute`、merge前diff artifact必須化、real merge失敗時の`failure_class`、Git ref更新後のrollback status/error証跡、`devos publish --dry-run/--execute`、`devos platform doctor --save`、`devos platform doctor --include-ui`、`devos platform map add`、DB-backed PathMappingService、`devos cleanup --dry-run` plan、`devos cleanup --execute` guard、`devos cleanup --quarantine --execute`、`devos cleanup quarantine list/restore`、`devos cleanup --delete --execute`、worktree safety証跡、real dry-run分類、merge conflict retry/cancel、manual patch needs_decision復帰を追加済みです。Toolchain ReportとCodex Runtime Readinessのschema copy・保存前validation・readiness toolchain blocker連携、codex-readiness --saveからのToolchain Setup Card同期、Windows実機readiness JSONのimport経路、WSL実プロジェクトでのwsl-primary bootstrap / check / readiness運用検証を追加済みです。次はWindows実機でのreadiness report生成・import運用検証へ進みます。

## Commit Policy

変更は常に機能単位でコミットします。進捗が変わるコミットでは、この表の `Progress`、`Status`、`Evidence`、`Next` を更新します。

既存コミット:

- `474b591` `docs: add initial orchestrator design docs`
- `8413a1d` `docs: add codex implementation workflow`
