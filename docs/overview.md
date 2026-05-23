# Overview

## Purpose

Personal Dev OS は、AIに自由に「全部やって」と渡すのではなく、成果物、状態遷移、検証、承認境界を固定して開発を進めるローカルファーストの開発オーケストレーターです。

作る対象はAIコーディングエージェントではありません。作る対象は、Codex CLI / SDK のような既存のコーディングエンジンを安全に使うための制御層です。

## Non Goals

初期完成スコープでは、次のものは作りません。

- 独自コードエディタ
- 独自CI
- 複雑なマルチエージェント基盤
- クラウド実行基盤
- 複数ユーザー管理
- 課金
- 本番デプロイ自動化

## Fixed Artifacts, Adaptive Loop

```text
Concept
  -> PRD
  -> Architecture
  -> Roadmap
  -> Human approves initial artifacts
  -> Task Breakdown
  -> Autonomous Run Loop
  -> Human Approval
  -> Merge
  -> Rolling Planning Checkpoint
  -> Memory / Roadmap / Task Group Update
```

固定するのは線形工程ではなく、PRD、設計、ロードマップ、タスク、run、検証結果、gate結果、decision report、change request という成果物です。実行中のワークフローは、失敗を診断して自動修復、再計画、分割、人間判断へ振り分けるループとして扱います。

PRD、Architecture、Roadmap、Task Breakdownは初期生成で固定されるものではありません。TaskまたはTask Groupの完了後はRolling Planning Checkpointを実行し、現状コード、diff、検証結果、gate結果、未解決Decision、既存Roadmapを材料にして次のTask Group候補やartifact更新案を作ります。承認が必要な変更はHuman Inboxへ送り、承認後に新しいartifact versionとして反映します。

Coding Agentが読むPRD、Architecture、Roadmapは、draftではなく `approved` または `approved_with_notes` のartifactです。初期artifactが承認されるまで、Task YAMLを実装可能な `ready` 状態にしません。

```text
Plan
  -> Implement
  -> Verify
      -> pass: Review
      -> fail: Diagnose
          -> caused_by_agent_change: Auto Repair
          -> caused_by_environment: Environment Input Card or Environment Issue Report
          -> caused_by_existing_baseline: Baseline Issue
          -> caused_by_spec_gap: Decision Report
          -> task_too_large: Auto Split / Replan
  -> Review
      -> pass: Ready for Human Approval
      -> minor_issue: Auto Repair
      -> major_risk: Decision Report
  -> Merge
  -> Rolling Planning Checkpoint
  -> Update Memory / Roadmap / Task Group
```

## Core Principles

- 成果物と状態遷移を固定し、実行は自動ループで進める。
- 人間の仕事は、プロダクト判断、リスク許容、方針変更に圧縮する。
- コード編集とコマンド実行はCodexに任せる。
- オーケストレーターは状態、方針、承認、差分、検証、証拠、記録を管理する。
- 通常の実装エラー、lint、type error、現diff起因のテスト失敗は自動修復を試す。
- 仕様影響がない巨大化は自動分割または再計画し、人間判断にしない。
- 完了単位は「実用できる縦断ワークフロー」にする。
- ユーザーが判断すべき変更は、証拠付きDecision ReportとしてHuman Inboxへ集約する。

## Agent Roles

初期版では、独立した複数プロセスではなく、ロール別プロンプトで扱います。

| Role | Responsibility | Initial implementation |
| --- | --- | --- |
| Product Agent | コンセプトをPRD、実用スコープ、論点に変換する | Prompt template |
| Planner Agent | ロードマップとタスクYAMLを作る | Prompt template |
| Coding Agent | 1タスクだけを実装する | Codex CLI / SDK |
| Review / Decision Agent | 差分、テスト、リスク、判断点を整理する | Separate review prompt |
| Policy Engine | プロジェクト方針に照らして次アクションを決める | Go rule engine + YAML policy |
| Evidence Collector | diff、検証結果、ログ、artifact、trace linkを集める | Go module |

## Implementation Choice

- Backend、CLI、workerはGoで実装する。
- UIはReactで実装する。
- TypeScriptはUI側に限定し、オーケストレーターの中核ロジックはGo側に置く。
- SQLite、ファイル成果物、Codex実行、Git worktree操作はGoバックエンドが所有する。
