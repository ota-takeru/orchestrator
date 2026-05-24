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
| Initial Complete Scope end-to-end workflow | 98% | 進行中 |

## Slice Progress

| Slice | Scope | Progress | Status | Evidence | Next |
| --- | --- | ---: | --- | --- | --- |
| 0 | Canonical Docs and Authority | 100% | 完了 | `474b591`, `8413a1d`, `7fad1c4` | context builderを実ワークフローへ接続する |
| 0.25 | Platform Model Docs | 100% | 完了 | `474b591` | platform modelを実装sliceでDB / runner contractへ反映する |
| 0.5 | Project Trust / Platform-aware Preflight | 90% | 進行中 | `c77f689`, `11603b3`, `b4129d6`, `de0d26f`, `dfe3fdf`, `bf80a4e` | path mapping issue projectionへ進む |
| 1 | Core Storage, Platform Tables, State Machines | 84% | 進行中 | `c77f689`, `ff4e1bb`, `90d6cb0`, `de0d26f`, `bb9e6e1`, `8f8d83e`, `ab22a6f`, `63132bf`, `799354b`, `538d771`, `5b72158`, `0bbd6e9`, `c79bd76`, `d77433c`, `fd68d94`, `ac3521c`, `50221e6`, `d0841e5`, `623ad4e`, `2e99d79`, `22a46b9`, `853446f`, `20520f7`, `44d00a0`, `d5e2a80` | 追加DB invariant検証、schema/check matrixの穴埋めへ進む |
| 1.5 | Schema Registry and Validation | 78% | 進行中 | `6e9bc7b`, `e11e947`, `414e79d`, `c9e8f76`, `402d04a`, `dfccfff` | schema validation対象の拡張へ進む |
| 2 | Artifact Lifecycle + Approval | 64% | 進行中 | `538d771`, `6dcf191`, `5b72158`, `b66b705`, `e11e947`, `cfeda29`, `4d63a15` | approved notes trusted contextへ進む |
| 2.25 | Runner and Platform Foundation | 72% | 進行中 | `019d1a9`, `f2309f3`, `bb9e6e1`, `799354b`, `642dbc0` | platform doctorとの統合、runner capability issue projectionへ進む |
| 3 | Fake Run Workflow with Fake Platform Runners | 75% | 進行中 | `f2309f3`, `bb9e6e1`, `5b72158`, `d387305`, `53abe8d`, `1fd2c5e` | Windows-primary / WSL-primary / Hybrid profilesを明示したfake workflowへ拡張する |
| 2.5 | Environment-aware Git / Worktree / Patch Foundation | 42% | 進行中 | `642dbc0`, `37c10c2`, `06dd2ca`, `9840a43`, `6a87f34` | worktree safetyのmerge前接続、path mapping validationの拡張へ進む |
| 4 | Environment-aware Verification / Baseline / Gate | 52% | 進行中 | `f2309f3`, `bb9e6e1`, `8f8d83e`, `862ffe5`, `6fb1fd7`, `49ce487`, `fd68d94`, `8e2252f`, `5fbe86a`, `cfeda29`, `37ba640` | multi-environment local verificationの拡張へ進む |
| 5 | Human Inbox + Approval Sources + Toolchain Setup | 90% | 進行中 | `dfe3fdf`, `a98622b`, `ab22a6f`, `5d52d90`, `02be517`, `6d02ed6`, `46ea815`, `f1a3a0f`, `7c82bb3`, `bf80a4e`, `9840a43`, `6a87f34`, `47aace5`, `7a0ab49`, `df52592`, `6fb1fd7`, `0851cf1`, `82ffff3`, `5d8ca13`, `e0e8a38`, `03e31ca`, `853446f`, `82431c8`, `99859d2`, `2418ea0`, `4d63a15`, `06c105f`, `37ba640` | setup card UIとmerge gate surface拡張へ進む |
| 6 | Merge Queue + Reverify | 97% | 進行中 | `63132bf`, `acd52bc`, `1fd2c5e`, `0bbd6e9`, `c79bd76`, `d77433c`, `989becc`, `31c099a`, `dd44f1b`, `117b6cc`, `1bf94f1`, `642dbc0`, `27cac3e`, `37c10c2`, `82b63f1`, `862ffe5`, `e2fd3c4`, `ac3521c`, `cf17193` | real mergeの失敗分類とrollback evidenceをさらに強化する |
| 7 | Real Codex Windows / WSL Execution | 65% | 進行中 | `d4e790a`, `862ffe5`, `750c16a`, `580bf87`, `0851cf1`, `220a406`, `82ffff3`, `5d8ca13`, `ad0a1bc`, `faf0c7c`, `085da18`, `1c523a8`, `dca6533`, `82431c8` | Windows runtime側の実機検証準備をさらに進める |
| 8+ | Auto Repair, Semantic Diff, Change Request, Planning Queue, UI | 94% | 着手 | `dd44f1b`, `1a5af75`, `f983158`, `08b5a03`, `423e7dc`, `50221e6`, `d0841e5`, `623ad4e`, `2e99d79`, `817daec`, `a451d3f`, `dd2820a`, `c4e464b`, `40c4e60`, `22a46b9`, `adfe423`, `853446f`, `425e4cd`, `935d658`, `20520f7`, `414e79d`, `34b42b5`, `c9e8f76`, `1c523a8`, `44d00a0`, `d5e2a80`, `402d04a`, `dca6533`, `832ef45`, `4ee303f`, `99859d2`, `dfccfff`, `2418ea0`, `d0ed17c`, `451ec70` | 残りのschema validation対象拡張、Windows runtime readinessの実機検証連携へ進む |

## Current Focus

現在の実装対象は Slice 0.5、Slice 1、Slice 1.5、Slice 2、Slice 2.25、Slice 2.5、Slice 3、Slice 4、Slice 5、Slice 6、Slice 7、Slice 8+ です。Go module、`devos` CLI入口、canonical docs context filter、project root検出、platform-aware preflight、platform enum、主要state machine API、PathMappingServiceの最小実装、Orchestrator-owned schema registry、`.devagent/schemas/` hash付きcopy、schema registry checksum preflight、schema registry mismatch repair、Task YAML ready化前Go validation、semantic behavior diff schema copy / validation、dependency risk ledger schema copy / validation、Human Inbox snapshot schema copy / validation、toolchain doctor skeleton、environment-specific `CODEX_HOME` / `codex-auth` 検出、WSL2 requirement、`platform doctor --env`、SQLite migration registryと001/002/003/004/005/006/007/008/009/010/011/012/013/014 DDL、work_queue_items、planning_runs、planning_artifacts、decision_report_drafts、task_group planning_unit、worker_runs、environment_bindings / audit、semantic_behavior_diffs category/summary/confidence、policy memories、dependency risk ledger、SQLite接続/migration apply、project init永続化、artifact versioning / approval、stale run artifact hashによるHuman Approval無効化、unresolved blocking GateResultによるapproval拒否、`devos artifacts`、approved artifactからのtask materialize、task YAML由来verification_commands保存、materialized taskのexecution queue投入、work startによるfake execution queue処理、auto repair queue投入/処理、work queue lease recovery、planning consolidationによるproposed task生成、rolling checkpoint report保存、Human Inbox UI snapshot、baseline issue memoryのsnapshot/API可視化、React/Vite Human Inbox UI scaffold、pnpm lock、UI lint/test/build検証、local HTTP API surface、Inbox approve API、Inbox projection/source-of-truth invariant、toolchain setup card projection、toolchain setup card解消同期、Toolchain Setup Card手順表示/mark-installed/waive、waiver expiry失効とmerge blocker再接続、preflight platform setup card projection、path mapping issue projection、Codex runtime readiness issue projection、GateResultのreport/human_input/human_decision/hard_block Inbox投影、baseline verification failureのREPORT_ONLY扱い、baseline issue memory保存、baseline issue reportがないrunのapproval拒否、`devos inbox`、`devos inbox approve`、`devos decisions`、`devos env status/set`、Human Approval source、`devos review` semantic behavior diff生成、`devos review reject`、`devos approve --remember`、`devos memory`、`devos dependency risk add/list`、`devos ui snapshot`、`devos serve`、retry decision承認後のtask再実行復帰、`devos request`、`devos requests`、`devos queue`、`devos change request`、`devos change analyze`、`devos change approve`、`devos plan start`、`devos plan status`、`devos plan consolidate`、`devos plan checkpoint`、`devos work start`、`devos work status`、`devos work pause/resume`、merge queue entrypoint、Runner interfaceとfake Windows/WSL/Linux runner、LocalRunner、複数environment対応のverification runner foundation、command output artifact保存、command event / verification result永続化、verification command失敗のcurrent_diff分類、GateResult evaluator / 永続化、`devos verify`、fake implementation run、RunProfile implementation environment解決に対応したLinux/WSL/Windows runtime policy付きのReal Codex Adapter、Real Codex dry-run preview、`devos platform codex-readiness --save`、runtime不一致時のblocked run / Decision / Inbox正規化、Real Codex実行前のtoolchain hard preflight、Real Codex run summaryへのenvironment/codex adapter/sandbox/CODEX_HOME source metadata保存、`devos run --real-codex --verify`、fake merge queue worker、`devos bootstrap --adapter fake`、`TestBootstrapFakeTaskMerges`、manual patch application repository、`devos patch export/status/mark-applied/verify-applied`、`runs.reverify_context_*` 保存、fake merge conflict handling、`devos merge --dry-run`、`devos merge queue --dry-run-real-git`、一時worktreeでのmerge前reverification付きlocal-only/ff-only/no-pushの`devos merge queue --process-real-git --execute`、`devos publish --dry-run/--execute`、`devos platform doctor --save`、`devos platform map add`、DB-backed PathMappingService、`devos cleanup --dry-run` plan、`devos cleanup --execute` guard、`devos cleanup --quarantine --execute`、`devos cleanup quarantine list/restore`、`devos cleanup --delete --execute`、worktree safety証跡、real dry-run分類、merge conflict retry/cancel、manual patch needs_decision復帰を追加済みです。次は残りのschema validation対象拡張、Windows runtime readinessの実機検証連携へ進みます。

## Commit Policy

変更は常に機能単位でコミットします。進捗が変わるコミットでは、この表の `Progress`、`Status`、`Evidence`、`Next` を更新します。

既存コミット:

- `474b591` `docs: add initial orchestrator design docs`
- `8413a1d` `docs: add codex implementation workflow`
