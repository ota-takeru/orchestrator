# Documentation Index

このディレクトリは、Personal Dev OS / Orchestrator の設計ドキュメントです。

## Reading Order

1. [overview.md](overview.md) - 目的、非目的、設計方針
2. [tech-stack.md](tech-stack.md) - Goバックエンド / React UI の技術前提
3. [platform-model.md](platform-model.md) - Windows / WSL / Hybrid 実行環境モデル
4. [runner-protocol.md](runner-protocol.md) - platform runnerの共通interfaceとcommand evidence
5. [path-mapping.md](path-mapping.md) - Windows / WSL path変換、same filesystem、isolated worktree
6. [toolchain-requirements.md](toolchain-requirements.md) - toolchain preflightとHuman Inbox setup card
7. [initial-complete-scope.md](initial-complete-scope.md) - 初期完成スコープに含めるもの、含めないもの、完了条件
8. [state-machine.md](state-machine.md) - Task / Run / Gate / Inbox / Merge Queue の正規状態遷移
9. [storage-schema.md](storage-schema.md) - SQLite migration、FK、CHECK、UNIQUE、index、path/JSON contract
10. [state-invariants.md](state-invariants.md) - DB制約だけでは表せないcross-table invariant
11. [autonomy-loop.md](autonomy-loop.md) - 自動修復、失敗分類、自動再計画のループ
12. [decision-gate.md](decision-gate.md) - GateResult、Policy Engine、Decision Report
13. [change-requests.md](change-requests.md) - 後からの人間変更要求と影響分析
14. [task-planning-and-work-queue.md](task-planning-and-work-queue.md) - 複数要望の受付、parallel planning、serial commit、順次worker
15. [ui-human-inbox.md](ui-human-inbox.md) - 人間判断を最小化するUI構造
16. [environment-variables.md](environment-variables.md) - UIからの環境変数入力、反映、secret管理
17. [architecture.md](architecture.md) - 全体アーキテクチャと内部モジュール
18. [cli-design.md](cli-design.md) - CLIの責務とコマンド設計
19. [data-model.md](data-model.md) - SQLite、Markdown、YAML成果物の設計
20. [execution-codex.md](execution-codex.md) - Codex CLI実行層、run artifact、レビュー
21. [security.md](security.md) - サンドボックス、権限、秘密情報の扱い
22. [implementation-start.md](implementation-start.md) - 最初に通すBootstrap Workflowと実装禁止範囲
23. [implementation-plan.md](implementation-plan.md) - Implementation Slice順序と完了条件
24. [openai-codex-reference.md](openai-codex-reference.md) - Codex公式ドキュメント確認メモ

## Codex Implementation Operating Docs

次の文書は、このリポジトリをCodexで実装していくための運用ガイドです。Orchestratorとして作る製品の正規仕様ではありません。

- [codex-implementation-workflow.md](codex-implementation-workflow.md) - Codexへの依頼単位、context選択、検証、レビュー、止まる条件

この区分の文書は、作業手順やプロンプト設計には使えますが、製品仕様、状態遷移、DB schema、Decision Gate、platform contractを定義しません。正規仕様と衝突する場合は "Canonical Implementation Docs" を優先します。

## Development Progress

- [progress.md](progress.md) - このリポジトリ自体の開発進行ログ

`progress.md` は人間向けの進行記録です。Orchestratorが将来保存するrun artifact、verification result、gate result、Decision Reportではなく、製品仕様としても扱いません。

## Canonical Implementation Docs

実装時に参照する正規仕様は次です。

- [overview.md](overview.md)
- [tech-stack.md](tech-stack.md)
- [platform-model.md](platform-model.md)
- [runner-protocol.md](runner-protocol.md)
- [path-mapping.md](path-mapping.md)
- [toolchain-requirements.md](toolchain-requirements.md)
- [initial-complete-scope.md](initial-complete-scope.md)
- [state-machine.md](state-machine.md)
- [storage-schema.md](storage-schema.md)
- [state-invariants.md](state-invariants.md)
- [autonomy-loop.md](autonomy-loop.md)
- [decision-gate.md](decision-gate.md)
- [change-requests.md](change-requests.md)
- [task-planning-and-work-queue.md](task-planning-and-work-queue.md)
- [ui-human-inbox.md](ui-human-inbox.md)
- [environment-variables.md](environment-variables.md)
- [architecture.md](architecture.md)
- [cli-design.md](cli-design.md)
- [data-model.md](data-model.md)
- [execution-codex.md](execution-codex.md)
- [security.md](security.md)
- [implementation-start.md](implementation-start.md)
- [implementation-plan.md](implementation-plan.md)
- [openai-codex-reference.md](openai-codex-reference.md)

この一覧にない `docs/*.md` は、実装仕様として使う前にこのindexへ追加してください。`docs/mvp-scope.md` のような旧MVPスコープ文書が再作成された場合はobsolete/non-canonicalとして扱い、Initial Complete Scopeを縮小する根拠にしてはいけません。

## Archived Background Document

- [archive/personal-dev-os-design.md](archive/personal-dev-os-design.md)

`archive/personal-dev-os-design.md` は背景資料であり、実装時の正規仕様ではありません。実装時はこのindexから参照されるテーマ別ドキュメントを正とします。古いdata model、CLI、state machine、Decision Gate、security、completion criteria が残っている可能性があるため、Coding Agentはこのファイルを実装仕様として使ってはいけません。

Codex context builderは `docs/archive/*`、obsolete/non-canonical docs、旧MVPスコープ文書を実装仕様として渡してはいけません。背景として渡す場合は、untrusted referenceであり正規仕様より優先しないことを明示します。

## Documentation Policy

- READMEはプロジェクトの入口に留める。
- `docs/index.md` は読む順番を示す。
- テーマ別ドキュメントは実装判断に使える粒度で保つ。
- タスクは原則として機能単位 / 成果単位で扱い、実装手順単位に細かく分けすぎない。
- 要件詳細化、影響分析、意思決定レポートdraft、task proposalはbounded parallel planningで扱ってよい。
- canonical artifact / task / roadmapへの反映はserial commit、implementation / mergeはsequentialを正とする。
- 複数要望と非同期実行の正規仕様は `task-planning-and-work-queue.md` を参照する。
- OS差を個別文書へ埋め込まず、platform-model、runner-protocol、path-mapping、toolchain-requirementsを正とする。
- verification command、Codex execution、Git worktree、path safetyは必ず `execution_environment_id` を持つ。
- Windows / WSL 両対応を理由にcore workflow、state machine、approval sourceを分岐させてはいけない。
- 原典を消さず、重要な設計判断はテーマ別ドキュメントへ反映する。
- OpenAI / Codexの仕様に依存する記述は、公式ドキュメント確認日とURLを残す。
