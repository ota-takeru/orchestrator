# Personal Dev OS / Orchestrator

Personal Dev OS は、ユーザーのコンセプトからPRD、設計、ロードマップ、実装タスク、検証、レビュー、承認までを、固定成果物と自動修復ループで進めるローカルファーストの開発オーケストレーターです。

このリポジトリで作るものは、AIコーディングエージェントそのものではありません。Codex CLI / SDK のような既存のコーディングエンジンを安全に使い、タスク、状態、承認、記録、差分、テスト結果、失敗分類、修復、再計画、変更要求を管理するローカル司令塔です。

バックエンドはGo、UIはReactで実装します。

このOrchestratorは、Windows / WSL 両対応の単一製品です。Core workflow、状態機械、DB schema、CLI UXは共有し、OS固有の振る舞いはrunner / platform adapterで扱います。各projectは必ず `primary_environment` を定義し、Windows-primaryとWSL-primaryの両方をサポートします。Hybrid modeもサポートしますが、Git / merge のcanonical environmentは常に1つに固定します。

## Documentation

まず読む順番と正規仕様は [docs/index.md](docs/index.md) を入口にしてください。実装時は `docs/index.md` の "Canonical Implementation Docs" に載っている文書だけを正規仕様として扱います。

Codexでこのリポジトリを実装していくための運用ガイドは [docs/codex-implementation-workflow.md](docs/codex-implementation-workflow.md) に分けています。この文書はプロダクト仕様ではなく、Codexへの依頼単位、context選択、検証、レビューの進め方を定義します。

開発進行の記録は [docs/progress.md](docs/progress.md) に残します。このログは人間向けの進行記録であり、Orchestratorが将来保存するrun artifactやDecision Reportの代替ではありません。

1. [docs/index.md](docs/index.md) - ドキュメント全体の入口
2. [docs/overview.md](docs/overview.md) - 目的と設計方針
3. [docs/tech-stack.md](docs/tech-stack.md) - Goバックエンド / React UI の技術前提
4. [docs/platform-model.md](docs/platform-model.md) - Windows / WSL / Hybrid 実行環境モデル
5. [docs/runner-protocol.md](docs/runner-protocol.md) - platform runnerの共通interfaceとcommand evidence
6. [docs/path-mapping.md](docs/path-mapping.md) - Windows / WSL path変換
7. [docs/toolchain-requirements.md](docs/toolchain-requirements.md) - toolchain preflightとsetup card
8. [docs/initial-complete-scope.md](docs/initial-complete-scope.md) - 初期完成スコープと完了条件
9. [docs/state-machine.md](docs/state-machine.md) - 状態遷移
10. [docs/storage-schema.md](docs/storage-schema.md) - SQLite schema contract
11. [docs/state-invariants.md](docs/state-invariants.md) - 状態と証拠の不変条件
12. [docs/autonomy-loop.md](docs/autonomy-loop.md) - 自動修復、失敗分類、自動再計画
13. [docs/decision-gate.md](docs/decision-gate.md) - GateResultとDecision Report
14. [docs/task-planning-and-work-queue.md](docs/task-planning-and-work-queue.md) - 複数要望、parallel planning、serial commit、順次実装worker
15. [docs/implementation-plan.md](docs/implementation-plan.md) - 実装順序

背景資料:

- [docs/archive/personal-dev-os-design.md](docs/archive/personal-dev-os-design.md)

背景資料、archive配下、obsolete/non-canonical docs、旧MVPスコープ文書は実装仕様ではありません。Initial Complete Scopeを縮小する根拠にも使いません。

## Target Workflow

初期完成スコープでは、次の1体験を最後まで通します。

```text
concept
  -> PRD
  -> architecture
  -> roadmap
  -> feature requests
  -> request queue
  -> bounded parallel planning
  -> planning consolidation
  -> serial canonical commit
  -> task planning
  -> task YAML
  -> sequential execution worker
  -> Codex implementation
  -> verification
  -> environment input if required
  -> auto repair / diagnose
  -> review
  -> decision gate
  -> approve / merge
  -> memory / roadmap update
```

## Recommended Shape

```text
Local Web UI / CLI
  -> Orchestrator Core (Go)
  -> SQLite / Orchestrator-owned artifacts
  -> Platform Manager
      -> Windows / WSL / Linux runners and adapters
      -> Path Mapping Service
      -> Toolchain Doctor
  -> Verification / Evidence / Decision Report
```

## Current Status

このリポジトリは設計・ドキュメント整備段階です。artifact model、状態管理、report schema、CLI、Codex実行層、自動修復、Human Inboxの順で実装します。各段階は完了条件を満たすまで完了扱いせず、初回リリースは縦断ワークフローが最後まで通ることを基準にします。
