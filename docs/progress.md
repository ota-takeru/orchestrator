# Implementation Progress

この文書は、このリポジトリ自体の実装進捗を [implementation-plan.md](implementation-plan.md) のsliceに沿って確認するためのトラッカーです。単なる時系列ログではなく、計画に対して何が完了し、何が未着手かを見えるようにします。

これはOrchestratorが将来保存するrun artifact、verification result、gate result、Decision Reportではありません。正規仕様は [index.md](index.md) の "Canonical Implementation Docs" を優先します。

## Summary

最終更新: 2026-05-24

| Area | Progress | Status |
| --- | ---: | --- |
| Documentation baseline | 100% | 完了 |
| Codex implementation operating docs | 100% | 完了 |
| Product implementation code | 77% | 着手 |
| Initial Complete Scope end-to-end workflow | 45% | 進行中 |

## Slice Progress

| Slice | Scope | Progress | Status | Evidence | Next |
| --- | --- | ---: | --- | --- | --- |
| 0 | Canonical Docs and Authority | 100% | 完了 | `474b591`, `8413a1d`, `7fad1c4` | context builderを実ワークフローへ接続する |
| 0.25 | Platform Model Docs | 100% | 完了 | `474b591` | platform modelを実装sliceでDB / runner contractへ反映する |
| 0.5 | Project Trust / Platform-aware Preflight | 90% | 進行中 | `c77f689`, `11603b3`, `b4129d6`, `de0d26f`, `dfe3fdf`, `bf80a4e` | path mapping issue projectionへ進む |
| 1 | Core Storage, Platform Tables, State Machines | 72% | 進行中 | `c77f689`, `ff4e1bb`, `90d6cb0`, `de0d26f`, `bb9e6e1`, `8f8d83e`, `ab22a6f`, `63132bf`, `799354b`, `538d771`, `5b72158`, `0bbd6e9`, `c79bd76`, `d77433c` | 追加DB invariant検証、schema/check matrixの穴埋めへ進む |
| 1.5 | Schema Registry and Validation | 0% | 未着手 | なし | Slice 1のDB基盤後に着手する |
| 2 | Artifact Lifecycle + Approval | 52% | 進行中 | `538d771`, `6dcf191`, `5b72158`, `b66b705` | approved notes trusted context、task YAML schema検証へ進む |
| 2.25 | Runner and Platform Foundation | 72% | 進行中 | `019d1a9`, `f2309f3`, `bb9e6e1`, `799354b`, `642dbc0` | platform doctorとの統合、runner capability issue projectionへ進む |
| 3 | Fake Run Workflow with Fake Platform Runners | 75% | 進行中 | `f2309f3`, `bb9e6e1`, `5b72158`, `d387305`, `53abe8d`, `1fd2c5e` | Windows-primary / WSL-primary / Hybrid profilesを明示したfake workflowへ拡張する |
| 2.5 | Environment-aware Git / Worktree / Patch Foundation | 42% | 進行中 | `642dbc0`, `37c10c2`, `06dd2ca`, `9840a43`, `6a87f34` | worktree safetyのmerge前接続、path mapping validationの拡張へ進む |
| 4 | Environment-aware Verification / Baseline / Gate | 30% | 進行中 | `f2309f3`, `bb9e6e1`, `8f8d83e`, `862ffe5` | baseline classification、required_for_merge failure policy、task YAML由来のverification_commandsへ進む |
| 5 | Human Inbox + Approval Sources + Toolchain Setup | 66% | 進行中 | `dfe3fdf`, `a98622b`, `ab22a6f`, `5d52d90`, `02be517`, `6d02ed6`, `46ea815`, `f1a3a0f`, `7c82bb3`, `bf80a4e`, `9840a43`, `6a87f34`, `47aace5`, `7a0ab49`, `df52592` | waiver Decision flow、setup card action拡張へ進む |
| 6 | Merge Queue + Reverify | 92% | 進行中 | `63132bf`, `acd52bc`, `1fd2c5e`, `0bbd6e9`, `c79bd76`, `d77433c`, `989becc`, `31c099a`, `dd44f1b`, `117b6cc`, `1bf94f1`, `642dbc0`, `27cac3e`, `37c10c2`, `82b63f1`, `862ffe5` | real merge前reverificationのlocal adapter化、pushは後続判断 |
| 7 | Real Codex Windows / WSL Execution | 23% | 進行中 | `d4e790a`, `862ffe5` | Linux/current env real-codex後のlocal verification接続を縦断テストへ拡張、Windows/WSLは後続判断 |
| 8+ | Auto Repair, Semantic Diff, Change Request, Planning Queue, UI | 4% | 着手 | `dd44f1b`, `1a5af75` | cleanup quarantineまたは恒久削除は後続判断、UIは初期縦断workflow後に扱う |

## Current Focus

現在の実装対象は Slice 0.5、Slice 1、Slice 2、Slice 2.25、Slice 2.5、Slice 3、Slice 4、Slice 5、Slice 6、Slice 7 です。Go module、`devos` CLI入口、canonical docs context filter、project root検出、platform-aware preflight、platform enum、主要state machine API、PathMappingServiceの最小実装、toolchain doctor skeleton、SQLite migration registryと001/002 DDL、SQLite接続/migration apply、project init永続化、artifact versioning / approval、`devos artifacts`、approved artifactからのtask materialize、toolchain setup card projection、toolchain setup card解消同期、Toolchain Setup Card手順表示/mark-installed、preflight platform setup card projection、path mapping issue projection、`devos inbox`、`devos inbox approve`、`devos decisions`、`devos env status`、Human Approval source、`devos review reject`、`devos approve`、merge queue entrypoint、Runner interfaceとfake Windows/WSL/Linux runner、LocalRunner、複数environment対応のverification runner foundation、command output artifact保存、command event / verification result永続化、GateResult evaluator / 永続化、`devos verify`、fake implementation run、Linux/current env限定のReal Codex Adapter v1、fake merge queue worker、`devos bootstrap --adapter fake`、`TestBootstrapFakeTaskMerges`、manual patch application repository、`devos patch export/status/mark-applied/verify-applied`、`runs.reverify_context_*` 保存、fake merge conflict handling、`devos merge --dry-run`、`devos merge queue --dry-run-real-git`、local-only/ff-only/no-pushの`devos merge queue --process-real-git --execute`、`devos platform doctor --save`、`devos platform map add`、DB-backed PathMappingService、`devos cleanup --dry-run` plan、`devos cleanup --execute` guard、worktree safety証跡、real dry-run分類、merge conflict retry/cancel、manual patch needs_decision復帰を追加済みです。次はReal Codex後の縦断検証、real merge前reverification接続、cleanup quarantine、またはpush/permanent delete/Windows adapter方針の判断が必要です。

## Commit Policy

変更は常に機能単位でコミットします。進捗が変わるコミットでは、この表の `Progress`、`Status`、`Evidence`、`Next` を更新します。

既存コミット:

- `474b591` `docs: add initial orchestrator design docs`
- `8413a1d` `docs: add codex implementation workflow`
