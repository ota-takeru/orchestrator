# Implementation Progress

この文書は、このリポジトリ自体の実装進捗を [implementation-plan.md](implementation-plan.md) のsliceに沿って確認するためのトラッカーです。単なる時系列ログではなく、計画に対して何が完了し、何が未着手かを見えるようにします。

これはOrchestratorが将来保存するrun artifact、verification result、gate result、Decision Reportではありません。正規仕様は [index.md](index.md) の "Canonical Implementation Docs" を優先します。

## Summary

最終更新: 2026-05-23

| Area | Progress | Status |
| --- | ---: | --- |
| Documentation baseline | 100% | 完了 |
| Codex implementation operating docs | 100% | 完了 |
| Product implementation code | 14% | 着手 |
| Initial Complete Scope end-to-end workflow | 0% | 未着手 |

## Slice Progress

| Slice | Scope | Progress | Status | Evidence | Next |
| --- | --- | ---: | --- | --- | --- |
| 0 | Canonical Docs and Authority | 100% | 完了 | `474b591`, `8413a1d` | 実装開始後、context builderが非正規docsを除外することをテストで固定する |
| 0.25 | Platform Model Docs | 100% | 完了 | `474b591` | platform modelを実装sliceでDB / runner contractへ反映する |
| 0.5 | Project Trust / Platform-aware Preflight | 65% | 進行中 | `c77f689`, `11603b3`, `b4129d6` | preflight結果とtoolchain doctor結果の永続化、setup card projectionへ進む |
| 1 | Core Storage, Platform Tables, State Machines | 25% | 進行中 | `c77f689`, `ff4e1bb` | SQLite driver接続、repository transaction、Go enum/state machineとDB CHECKの整合テストを追加する |
| 1.5 | Schema Registry and Validation | 0% | 未着手 | なし | Slice 1のDB基盤後に着手する |
| 2 | Artifact Lifecycle + Approval | 0% | 未着手 | なし | artifact versioningとapproval source of truthを実装する |
| 2.25 | Runner and Platform Foundation | 0% | 未着手 | なし | Runner interfaceとfake platform runnerを実装する |
| 2.5 | Environment-aware Git / Worktree / Patch Foundation | 0% | 未着手 | なし | canonical Git environment resolverとworktree基盤を実装する |
| 3 | Fake Run Workflow with Fake Platform Runners | 0% | 未着手 | なし | Fake Coding Agent Adapterで縦断workflowを通す |
| 4 | Environment-aware Verification / Baseline / Gate | 0% | 未着手 | なし | verification commandとGateResult保存を実装する |
| 5 | Human Inbox + Approval Sources + Toolchain Setup | 0% | 未着手 | なし | Human Inbox projectionとapproval commandsを実装する |
| 6 | Merge Queue + Reverify | 0% | 未着手 | なし | merge queue state machineとreverificationを実装する |
| 7 | Real Codex Windows / WSL Execution | 0% | 未着手 | なし | Fake workflow完了後にReal Codex adapterへ進む |
| 8+ | Auto Repair, Semantic Diff, Change Request, Planning Queue, UI | 0% | 未着手 | なし | 初期縦断workflow後に順次扱う |

## Current Focus

現在の実装対象は Slice 0.5 と Slice 1 の入口です。Go module、`devos` CLI入口、project root検出、platform-aware preflight、platform enum、主要state machine API、PathMappingServiceの最小実装、toolchain doctor skeleton、SQLite migration registryと001/002 DDLを追加済みです。次はSQLite driver接続、repository transaction、preflight/toolchain reportの保存へ進みます。

## Commit Policy

変更は常に機能単位でコミットします。進捗が変わるコミットでは、この表の `Progress`、`Status`、`Evidence`、`Next` を更新します。

既存コミット:

- `474b591` `docs: add initial orchestrator design docs`
- `8413a1d` `docs: add codex implementation workflow`
