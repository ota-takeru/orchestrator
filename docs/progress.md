# Implementation Progress

この文書は、このリポジトリ自体の実装進捗を [implementation-plan.md](implementation-plan.md) のsliceに沿って確認するためのトラッカーです。単なる時系列ログではなく、計画に対して何が完了し、何が未着手かを見えるようにします。

これはOrchestratorが将来保存するrun artifact、verification result、gate result、Decision Reportではありません。正規仕様は [index.md](index.md) の "Canonical Implementation Docs" を優先します。

## Summary

最終更新: 2026-05-23

| Area | Progress | Status |
| --- | ---: | --- |
| Documentation baseline | 100% | 完了 |
| Codex implementation operating docs | 100% | 完了 |
| Product implementation code | 32% | 着手 |
| Initial Complete Scope end-to-end workflow | 0% | 未着手 |

## Slice Progress

| Slice | Scope | Progress | Status | Evidence | Next |
| --- | --- | ---: | --- | --- | --- |
| 0 | Canonical Docs and Authority | 100% | 完了 | `474b591`, `8413a1d`, `7fad1c4` | context builderを実ワークフローへ接続する |
| 0.25 | Platform Model Docs | 100% | 完了 | `474b591` | platform modelを実装sliceでDB / runner contractへ反映する |
| 0.5 | Project Trust / Platform-aware Preflight | 85% | 進行中 | `c77f689`, `11603b3`, `b4129d6`, `de0d26f`, `dfe3fdf` | platform setup / path mapping issue projectionへ進む |
| 1 | Core Storage, Platform Tables, State Machines | 52% | 進行中 | `c77f689`, `ff4e1bb`, `90d6cb0`, `de0d26f`, `bb9e6e1`, `8f8d83e` | human_approvals repository、DB CHECKの追加整合検証を実装する |
| 1.5 | Schema Registry and Validation | 0% | 未着手 | なし | Slice 1のDB基盤後に着手する |
| 2 | Artifact Lifecycle + Approval | 0% | 未着手 | なし | artifact versioningとapproval source of truthを実装する |
| 2.25 | Runner and Platform Foundation | 50% | 進行中 | `019d1a9`, `f2309f3`, `bb9e6e1` | run artifact保存、platform doctorとの統合へ進む |
| 3 | Fake Run Workflow with Fake Platform Runners | 15% | 進行中 | `f2309f3`, `bb9e6e1` | fake implementation adapter、run artifact保存、invalid state transition rejectionを縦断workflowへ接続する |
| 2.5 | Environment-aware Git / Worktree / Patch Foundation | 0% | 未着手 | なし | canonical Git environment resolverとworktree基盤を実装する |
| 3 | Fake Run Workflow with Fake Platform Runners | 0% | 未着手 | なし | Fake Coding Agent Adapterで縦断workflowを通す |
| 4 | Environment-aware Verification / Baseline / Gate | 15% | 進行中 | `f2309f3`, `bb9e6e1`, `8f8d83e` | baseline classification、required_for_merge failure policy、GateResultからTaskStatusへの写像へ進む |
| 5 | Human Inbox + Approval Sources + Toolchain Setup | 18% | 進行中 | `dfe3fdf`, `a98622b` | approval source、platform/path setup cardsへ進む |
| 6 | Merge Queue + Reverify | 0% | 未着手 | なし | merge queue state machineとreverificationを実装する |
| 7 | Real Codex Windows / WSL Execution | 0% | 未着手 | なし | Fake workflow完了後にReal Codex adapterへ進む |
| 8+ | Auto Repair, Semantic Diff, Change Request, Planning Queue, UI | 0% | 未着手 | なし | 初期縦断workflow後に順次扱う |

## Current Focus

現在の実装対象は Slice 0.5、Slice 1、Slice 2.25、Slice 3、Slice 4、Slice 5 の入口です。Go module、`devos` CLI入口、canonical docs context filter、project root検出、platform-aware preflight、platform enum、主要state machine API、PathMappingServiceの最小実装、toolchain doctor skeleton、SQLite migration registryと001/002 DDL、SQLite接続/migration apply、project init永続化、toolchain setup card projection、`devos inbox`、Runner interfaceとfake Windows/WSL/Linux runner、複数environment対応のverification runner foundation、command event / verification result永続化、GateResult evaluator / 永続化を追加済みです。次はapproval sourceの保存へ進みます。

## Commit Policy

変更は常に機能単位でコミットします。進捗が変わるコミットでは、この表の `Progress`、`Status`、`Evidence`、`Next` を更新します。

既存コミット:

- `474b591` `docs: add initial orchestrator design docs`
- `8413a1d` `docs: add codex implementation workflow`
