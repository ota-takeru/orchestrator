# Archived Background Document

> DO NOT USE THIS FILE AS IMPLEMENTATION SPEC.
> This file is historical background only.
> If it conflicts with docs/index.md canonical docs, canonical docs win.

このファイルは背景資料であり、実装時の正規仕様ではありません。

実装時は [docs/index.md](../index.md) から参照されるテーマ別ドキュメントを正とします。このファイル内の data model、CLI、state machine、Decision Gate、security、completion criteria は古い可能性があります。Coding Agent はこのファイルを実装仕様として使ってはいけません。

最終更新: 2026-05-21  
対象: ローカルで動くAI開発オーケストレーター

## 1. 概要

Personal Dev OS は、ユーザーのコンセプトからPRD、実用スコープ、設計、ロードマップ、実装タスク、検証、レビュー、承認までを、固定成果物と自動ループで進めるローカルファーストの開発基盤である。

この基盤が作るべきものは、AIコーディングエージェントそのものではない。作るべきものは、既存のコーディングエージェントを安全に使い、タスク、状態、承認、記録、差分、テスト結果、失敗分類、修復、再計画、変更要求を管理するローカル司令塔である。

推奨構成は次の通り。

```text
Local Web UI / CLI
        ↓
Orchestrator API
        ↓
SQLite / Local Files
        ↓
Task Runner
        ↓
Codex CLI or Codex SDK
        ↓
Git Worktree / Local Repo
        ↓
Verification / Evidence / Decision Report
```

## 1.1 2026-05-21設計更新: 固定成果物 + 自動ループ

この原典の古い節には、Concept -> PRD -> Architecture -> Roadmap -> Task Breakdown -> Implementation -> Verification -> Review -> Decision / Merge という線形フローの説明が残っている。入口の説明としては有効だが、実装時の正規設計は線形フローではなく、固定成果物と自動ループである。

固定する成果物:

- PRD
- Architecture
- Roadmap
- Task YAML
- Run
- Verification Result
- Gate Result
- Decision Report
- Change Request
- Artifact Version
- Trace Link
- Policy
- Memory

正規ループ:

```text
Plan
  -> Implement
  -> Verify
      -> pass: Review
      -> fail: Diagnose
          -> caused_by_agent_change: Auto Repair
          -> caused_by_environment: Environment Report
          -> caused_by_existing_baseline: Baseline Issue
          -> caused_by_spec_gap: Decision Report
          -> task_too_large: Auto Split / Replan
  -> Review
      -> pass: Ready for Human Approval
      -> minor_issue: Auto Repair
      -> major_risk: Decision Report
  -> Merge
  -> Update Memory / Roadmap
```

実装時はテーマ別ドキュメントの [autonomy-loop.md](../autonomy-loop.md)、[decision-gate.md](../decision-gate.md)、[change-requests.md](../change-requests.md)、[ui-human-inbox.md](../ui-human-inbox.md) を優先する。

## 2. 設計方針

### 2.1 自由なAIではなく成果物と状態遷移が固定されたAIにする

AIに「必要なことを全部やって」と渡すのではなく、成果物、状態遷移、承認境界を固定する。AIは各工程の成果物を作り、失敗時はオーケストレーターが診断、自動修復、自動再計画、人間判断へ振り分ける。

```text
Concept
  ↓
PRD
  ↓
Usable Scope
  ↓
Architecture
  ↓
Roadmap
  ↓
Task Breakdown
  ↓
Implementation
  ↓
Verification
  ↓
Diagnose / Auto Repair / Auto Replan
  ↓
Review
  ↓
Human Approval / Merge
```

### 2.2 最初から複雑なマルチエージェントにしない

初期実用版では、実体として独立した複数エージェントを作らない。1つのLLM実行基盤にロール別プロンプトを渡す。

| ロール | 役割 | 初期実用版での実体 |
| --- | --- | --- |
| Product Agent | コンセプトをPRD、実用スコープ、論点に変換する | プロンプトテンプレート |
| Planner Agent | ロードマップとタスクYAMLを作る | プロンプトテンプレート |
| Coding Agent | 1タスクだけを実装する | Codex CLI / Codex SDK |
| Review / Decision Agent | 差分、テスト、リスク、判断点を整理する | 別実行のレビュープロンプト |

将来、並列性や専門性が必要になった段階で、プロセスやキュー単位に分離する。

### 2.3 コーディングエンジンは自作しない

コード編集、コマンド実行、テスト修正、差分生成は Codex CLI / Codex SDK に任せる。自作する対象は以下に限定する。

- どのタスクを実装させるか
- どの権限で実行するか
- いつ止めるか
- どの判断を人間に戻すか
- 実装結果をどう検証するか
- 判断履歴と実行ログをどう保存するか

### 2.4 中途半端な機能ではなく、完了するワークフローを単位にする

この設計では「初期版だから機能が途中まででもよい」という考え方を採らない。ユーザーはすでに作りたいものを持っている前提なので、採用するスコープはユーザーの主要ワークフローが最後まで通ることを条件にする。

実装単位は小さくしてよいが、リリース単位は実用できる縦断ワークフローにする。途中までの入力画面、確認できない差分、承認できない実行結果、復旧できない失敗状態は、ユーザーの作業を増やすだけなので完了扱いにしない。

スコープ判断の基準:

- ユーザーが実際に始めて、終えられる作業か
- 失敗時、判断待ち、再実行時の扱いが定義されているか
- 成果物、ログ、差分、テスト結果をユーザーが確認できるか
- その機能がないと主要ワークフローが詰まるか
- その機能を入れてもワークフローが複雑になりすぎないか

## 3. 最新前提と参照情報

この設計は 2026-05-21 時点で公式ドキュメントを確認した前提で書いている。実装時はCLIの細かいフラグやSDKの成熟度を再確認する。

- Codex CLI はローカル端末で動くコーディングエージェントで、選択したディレクトリ内のコードを読める、変更できる、コマンドを実行できる。参照: [Codex CLI](https://developers.openai.com/codex/cli)
- `codex exec` はスクリプトやCI向けの非対話モード。JSONLイベント、最終メッセージ出力、JSON Schemaによる構造化出力に対応する。参照: [Non-interactive mode](https://developers.openai.com/codex/noninteractive)
- 新しい自動化では、互換用の `--full-auto` より、明示的な `--sandbox workspace-write` を優先する。参照: [Non-interactive mode](https://developers.openai.com/codex/noninteractive)
- Codex SDK は自作アプリやワークフローからローカルCodexを制御するための将来選択肢である。初期実用版ではGoバックエンドからCodex CLIをサブプロセス実行する。参照: [Codex SDK](https://developers.openai.com/codex/sdk)
- Codexは `AGENTS.md` を読み込み、グローバル指示とプロジェクト指示を階層的に適用する。参照: [Custom instructions with AGENTS.md](https://developers.openai.com/codex/guides/agents-md)
- サンドボックスと承認は別の制御であり、サンドボックスは技術的境界、承認は境界を越えるときの停止条件である。参照: [Sandbox](https://developers.openai.com/codex/concepts/sandboxing)
- エージェント設計では、人間承認、ガードレール、ツール承認、構造化された入力を組み合わせるのが安全側の設計になる。参照: [Safety in building agents](https://developers.openai.com/api/docs/guides/agent-builder-safety)

## 4. 推奨技術スタック

| 領域 | 推奨 |
| --- | --- |
| UI | ローカルWeb UI + CLI |
| フロントエンド | React / TypeScript / Vite / Tailwind / shadcn/ui |
| バックエンド | Go |
| DB | SQLite |
| SQLiteアクセス | `database/sql` + SQLite driver |
| 状態管理 | 自作ステートマシン |
| コーディング実行 | Codex CLI を初期実用版の標準、必要に応じて Codex SDK |
| 作業環境 | Git worktree |
| プロジェクト記憶 | Markdown + YAML + SQLite |
| 承認管理 | Human Inbox / Decision Report |
| ログ | JSONL + Markdown summary + immutable run artifacts |
| バリデーション | Go struct validation + JSON Schema |
| セキュリティ | Codex sandbox / Docker / Git diff確認 / Decision Gate |

初期実用版では Go バックエンドから Codex CLI を child process として呼び出す。SDK移行は、ストリーミング制御、スレッド再開、アプリ内統合、イベント処理を深く扱う必要が出てからでよい。

## 5. 全体アーキテクチャ

```text
┌────────────────────────────┐
│ Local UI                   │
│ - Concept Chat             │
│ - Product Spec View        │
│ - Human Inbox              │
│ - Autonomous Run Monitor   │
│ - Decision Report View     │
│ - Semantic Diff / Logs     │
└──────────────┬─────────────┘
               ↓
┌────────────────────────────┐
│ Orchestrator API           │
│ - workflow state machine   │
│ - task dispatcher          │
│ - policy engine            │
│ - decision gate            │
│ - evidence collector       │
│ - repair loop controller   │
│ - change impact analyzer   │
│ - artifact manager         │
│ - memory manager           │
└──────────────┬─────────────┘
               ↓
┌────────────────────────────┐
│ Project State              │
│ - SQLite                   │
│ - Markdown artifacts       │
│ - YAML task definitions    │
│ - JSONL run logs           │
└──────────────┬─────────────┘
               ↓
┌────────────────────────────┐
│ Execution Layer            │
│ - Codex CLI / SDK          │
│ - Git worktree             │
│ - sandbox profile          │
│ - approval policy          │
│ - verification runner      │
│ - diff analyzer            │
└──────────────┬─────────────┘
               ↓
┌────────────────────────────┐
│ Local Repository           │
│ - source code              │
│ - AGENTS.md                │
│ - tests                    │
│ - generated docs           │
└────────────────────────────┘
```

## 6. 初期実用スコープ

最初の実用スコープは、次の1体験に絞る。

> コンセプトを入力すると、PRDとロードマップとタスクに分解され、最初のタスクをCodexに実装委任し、差分とテスト結果を見て承認できる。

ここで重要なのは「機能数を減らすこと」ではなく、「この1体験が最後まで成立すること」である。途中まで動く生成機能だけでは完了扱いにしない。実装委任、検証、差分確認、判断待ち、承認後の反映までを一連のワークフローとして扱う。

初期実用版に含める機能:

1. コンセプト入力
2. PRD生成
3. Architecture生成
4. Roadmap生成
5. タスクYAML生成
6. CLIでのタスク一覧表示
7. 1タスクをCodex CLIに実装委任
8. テスト、lint、build実行
9. diff保存と表示
10. Decision Gate
11. Decision Report生成
12. 承認後のmergeまたは手動適用フロー

初期実用版に含めないもの:

- 独自コードエディタ
- 独自CI
- 複雑なマルチエージェント会話
- Slack / Linear / GitHub連携
- 複数ユーザー管理
- 課金
- クラウド実行基盤
- 本番デプロイ自動化

除外する理由は「要件外にしたいから」ではなく、上記の主要ワークフローを成立させるための必須条件ではないからである。逆に、主要ワークフローを成立させるために必要な機能は、見た目が小さくても初期実用版に含める。

## 7. UI設計

UIはチャットだけにしない。チャットは初期入力には便利だが、長期プロジェクトの状態、差分、判断待ち、実行履歴を管理しにくい。

### 7.1 Concept Chat

最初のアイデアを入力する場所。入力例:

```text
個人向けのタスク管理アプリを作りたい。
AIが今日やるべきタスクを提案してくれる。
スマホでも見られるようにしたい。
```

生成する成果物:

- `.devagent/concept.md`
- `.devagent/prd.md`
- `.devagent/architecture.md`
- `.devagent/roadmap.yaml`
- `.devagent/tasks/*.yaml`

### 7.2 Product Spec View

PRDと設計を確認する画面。

表示項目:

- プロダクト概要
- ユーザー像
- 解決する課題
- 実用スコープ
- 非スコープ
- 主要機能
- 画面一覧
- データモデル
- 技術スタック案
- 未決定事項

PRDとArchitectureは人間承認の対象にする。Coding Agentが勝手に変更してはいけない。

### 7.3 Roadmap / Task Board

タスクを状態別に確認する画面。

カラム:

- Backlog
- Ready
- Implementing
- Verifying
- Needs Decision
- Reviewing
- Done
- Failed

各タスクの表示項目:

- ID
- タイトル
- 目的
- 受け入れ条件
- 実装対象
- 依存タスク
- 推奨テスト
- リスク
- 状態
- 実行ログ
- 差分

### 7.4 Human Inbox

人間の判断が必要なものだけを並べる画面。この基盤の中核である。Task Boardより優先して作る。

例:

```text
Needs Your Judgment:
DEC-014: 認証方式の選定

Why human required:
認証方式はsecurity、DB設計、product behaviorに影響する。

Recommendation:
A. Supabase Auth

選択肢:
A. Supabase Auth
B. Clerk
C. 自前認証

推奨:
A. Supabase Auth

理由:
初期実用版の主要ワークフローに必要なユーザー別データ分離を、DB・認証・権限設計とあわせて扱いやすい。

Actions:
[Approve A] [Choose B] [Choose C] [Ask revision]
```

環境変数不足のように、人間が方針判断ではなく値入力だけを行えば解決するものは、Decision ReportではなくEnvironment Input CardとしてHuman Inboxに出す。

```text
Action Required: Missing environment variables

Why:
OPENAI_API_KEY が未設定のため、verification commandを実行できない。

Input:
[OPENAI_API_KEY] [••••••••••••••••]

Apply to:
(*) This project only
( ) This task run only
( ) User-level default for future projects

Actions:
[Save and Rerun] [Skip This Run] [Reject Requirement]
```

secret値はSQLite、prompt、events.jsonl、stdout、stderr、summary、Decision Reportへ保存しない。Orchestrator APIだけが `.env.local` またはsecret storeへ反映し、Codexからは `.env` / `.env.local` をdeny-readにする。

### 7.5 Run Log / Diff View

AIが何をしたか確認する画面。

表示項目:

- 実行開始時刻
- 対象タスク
- 使用したプロンプト
- 実行コマンド
- 変更ファイル
- テスト結果
- 失敗ログ
- AIの修正ループ
- 最終サマリー
- Git diff

ここがないと、AIが何をしたのか検証できない。

## 8. CLI設計

初期実用版ではWeb UIより先にCLIを作る。CLIでコア体験を固め、その後UIを被せる。

ただし、CLI先行は「UIを作らなくてよい」という意味ではない。CLIだけで、コンセプト入力から実装委任、検証、レビュー、承認、反映までが完結することを先に確認する。Web UIは、その状態と判断を見やすく扱うための表示層として追加する。

```text
devos init "AIタスク管理アプリを作りたい"
devos spec
devos plan
devos tasks
devos run TASK-001
devos review TASK-001
devos decisions
devos approve DEC-001 --option A
devos merge TASK-001
```

CLIの責務:

- `.devagent/` 初期化
- SQLiteへの登録
- Markdown/YAML成果物の生成
- タスク状態遷移
- Codex CLI実行
- JSONLログ保存
- diff保存
- Decision Gate判定
- レビュー結果表示

## 9. データ設計

SQLiteは検索、一覧、状態管理、UI表示の正規データに使う。Markdown/YAMLはAIと人間が読みやすい成果物として残す。

### 9.1 テーブル

```sql
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root_path TEXT NOT NULL,
  concept TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  path TEXT NOT NULL,
  content TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  acceptance_criteria TEXT NOT NULL,
  status TEXT NOT NULL,
  priority TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  assigned_agent TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  status TEXT NOT NULL,
  executor TEXT NOT NULL,
  prompt_path TEXT NOT NULL,
  stdout_path TEXT NOT NULL,
  stderr_path TEXT NOT NULL,
  jsonl_log_path TEXT NOT NULL,
  diff_path TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  task_id TEXT,
  title TEXT NOT NULL,
  background TEXT NOT NULL,
  options TEXT NOT NULL,
  recommendation TEXT NOT NULL,
  risk TEXT NOT NULL,
  status TEXT NOT NULL,
  selected_option TEXT,
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  FOREIGN KEY (project_id) REFERENCES projects(id),
  FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  scope TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  importance INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);
```

JSON文字列で保存する列は、Go側で構造体へdecodeして検証する。Codexの構造化出力にはJSON Schemaを使う。

## 10. ローカルファイル構成

各プロジェクトには `.devagent/` を置く。

```text
my-app/
  AGENTS.md
  go.mod
  cmd/
  internal/
  ui/
    package.json
    src/
  tests/

  .devagent/
    concept.md
    prd.md
    architecture.md
    roadmap.yaml
    decisions/
      DEC-001-auth.md
      DEC-002-database.md
    tasks/
      TASK-001-project-setup.yaml
      TASK-002-auth.yaml
      TASK-003-dashboard.yaml
    runs/
      RUN-20260521-001/
        prompt.md
        stdout.log
        stderr.log
        events.jsonl
        diff.patch
        review.json
        summary.md
    memory/
      project-rules.md
      user-preferences.md
      implementation-notes.md
```

DBだけにしない理由:

- AIに読ませやすい
- 人間が確認しやすい
- Git管理しやすい
- 破損時に復旧しやすい
- 実行履歴が監査可能になる

## 11. 状態管理

### 11.1 ProjectStatus

```ts
type ProjectStatus =
  | "concept"
  | "spec_ready"
  | "roadmap_ready"
  | "implementing"
  | "blocked"
  | "complete";
```

### 11.2 TaskStatus

```ts
type TaskStatus =
  | "draft"
  | "ready"
  | "implementing"
  | "verifying"
  | "diagnosing"
  | "repairing"
  | "reviewing"
  | "needs_decision"
  | "blocked_on_environment"
  | "blocked_on_policy"
  | "ready_for_human_review"
  | "merged"
  | "failed";
```

### 11.3 状態遷移

```text
DRAFT
  ↓
READY
  ↓
IMPLEMENTING
  ↓
VERIFYING
  ↓
DIAGNOSING / REPAIRING
  ↓
REVIEWING
  ↓
READY_FOR_HUMAN_REVIEW
  ↓
MERGED
```

分岐:

- 判断が必要: `NEEDS_DECISION`
- 環境起因で停止: `BLOCKED_ON_ENVIRONMENT`
- policy上停止: `BLOCKED_ON_POLICY`
- 修復不能または予算超過: `FAILED`
- 検証失敗: `DIAGNOSING` から `REPAIRING`、`BLOCKED_ON_ENVIRONMENT`、`NEEDS_DECISION` のいずれかへ遷移
- レビューで重大リスク: `NEEDS_DECISION`

`merged` にできない条件:

- verification command が失敗し、未分類またはrepair budget未消化
- 未解決のDecisionがある
- diffが空でないのにレビューが未完了
- Decision Gateで `HUMAN_DECISION` または `HARD_BLOCK` が残っている
- task acceptance criteria が未確認

## 12. 1タスク実行フロー

```text
1. 次のREADYタスクを選ぶ
2. Git worktreeを作る
3. タスク用プロンプトを生成する
4. Codexに実装させる
5. JSONLイベントを保存する
6. テスト・lint・buildを実行する
7. 失敗したら修正を依頼する
8. 差分を取得する
9. Decision Gateを実行する
10. レビュー用Codex実行を別途走らせる
11. Decision Reportが必要なら作成する
12. 問題なければユーザーにレビュー提示する
13. 承認されたらmainに反映する
14. roadmapとmemoryを更新する
```

1回の実装タスクは小さく保つ。

悪いタスク:

```text
アプリ全体を作る
```

良いタスク:

```yaml
id: TASK-003
title: Implement task creation API and form
goal: ユーザーが新しいタスクを作成できるようにする
acceptance_criteria:
  - タスク名、期限、優先度を入力できる
  - 入力値バリデーションがある
  - 作成後に一覧へ反映される
  - APIテストが追加される
  - 関連する検証コマンドが通る
```

## 13. Codex実行層

### 13.1 初期実用版ではCodex CLIを使う

最初は `codex exec` をサブプロセスで呼び出す。

```bash
codex exec \
  --sandbox workspace-write \
  --ask-for-approval untrusted \
  --json \
  --output-schema .devagent/schemas/run-result.schema.json \
  -o .devagent/runs/RUN-20260521-001/final.json \
  "Read .devagent/tasks/TASK-003.yaml and implement it. Follow AGENTS.md. Run tests. Do not change unrelated files."
```

実装時はpromptをコマンドライン引数ではなくstdinで渡す。

```bash
codex exec \
  --sandbox workspace-write \
  --ask-for-approval untrusted \
  --json \
  --output-schema .devagent/schemas/run-result.schema.json \
  -o .devagent/runs/RUN-20260521-001/final.json \
  -C .devagent-worktrees/TASK-003 \
  -
```

実装上の注意:

- `stdout` のJSONLを `.devagent/runs/{run_id}/events.jsonl` に保存する
- `stderr` は `.devagent/runs/{run_id}/stderr.log` に保存する
- 最終メッセージは `-o` / `--output-last-message` で保存する
- `turn.completed` とプロセス終了コードの両方を見る
- `turn.failed` / `error` は失敗扱いにする
- タイムアウトを必ず設定する
- 長時間無出力タイムアウトも設定する
- 実行前後で `git diff --stat` と `git diff` を保存する

### 13.2 SDKへ移行する条件

Codex SDKへ移行するのは次の条件が出てからでよい。

- アプリ内でスレッドを継続制御したい
- JSONLを直接扱うだけでは状態管理が重い
- Run単位のイベントをUIにリアルタイム反映したい
- SDKの型付きAPIでプロンプト、出力、resumeを扱いたい
- Codexを自作ワークフローの内部コンポーネントとして深く組み込みたい

初期実用版ではCLIの方が実装が単純で、障害時の切り分けもしやすい。

## 14. 実装タスクYAML

```yaml
id: TASK-003
title: Implement task creation API and form
status: ready
priority: high
risk_level: medium

goal: >
  ユーザーが新しいタスクを作成できるようにする。

context:
  prd: .devagent/prd.md
  architecture: .devagent/architecture.md

acceptance_criteria:
  - ユーザーはタスク名を入力できる
  - ユーザーは期限を設定できる
  - ユーザーは優先度を設定できる
  - 入力値バリデーションがある
  - 作成後にタスク一覧に表示される
  - 関連テストが通る

constraints:
  - 認証方式は変更しない
  - 新しい本番依存パッケージを追加しない
  - 関係ないUIを変更しない

verification_commands:
  - go test ./...
  - pnpm --dir ui test
  - pnpm --dir ui lint
  - pnpm --dir ui build

decision_triggers:
  - DBスキーマ変更が必要な場合
  - 新しい外部ライブラリが必要な場合
  - 認証・権限設計に影響する場合
```

## 15. 実装プロンプト

```text
You are implementing a single bounded task.

Read:
- AGENTS.md
- .devagent/prd.md
- .devagent/architecture.md
- .devagent/tasks/TASK-003.yaml

Implement only TASK-003.

Rules:
- Do not modify unrelated files.
- Do not introduce new production dependencies without creating a Decision Report.
- Do not change authentication or database schema unless the task explicitly requires it.
- Do not stop for ordinary implementation errors.
- Attempt repair when tests fail due to your changes, lint fails, type checks fail, generated files are missing, or acceptance criteria are partially unmet.
- Stop and request a Decision Report only when product behavior is ambiguous, architecture must change, dependency/auth/DB/external API/payment/personal data is involved, or fixing would exceed task scope.
- Run the verification commands listed in the task file.
- Archived obsolete instruction: do not write run artifacts from Coding Agent. Current canonical behavior is to return structured output and let Orchestrator collect diff, logs, verification results, gate results, and summary.
```

## 16. レビューフロー

実装後は、Coding Agentとは別のCodex実行でレビューする。実装した本人に最終判定まで任せない。

```text
Review the current git diff against the task requirements.

Check:
- Does the implementation satisfy all acceptance criteria?
- Are there unrelated changes?
- Are there security concerns?
- Are there missing tests?
- Are there fragile assumptions?
- Did the agent introduce dependencies?
- Did the agent change database schema?
- Should this require human approval?

Return a structured review:
- pass: boolean
- issues: list
- required_decisions: list
- recommended_next_action
```

レビュー結果は `.devagent/runs/{run_id}/review.json` に保存する。

## 17. Decision Gate

Decision Gateは、AIが勝手に進めてはいけない変更を止める仕組みである。ルールベース判定とAI判定を併用する。

### 17.1 ルールベースで必ず止める条件

以下は機械的に `NEEDS_DECISION` にする。

- `go.mod` / `go.sum` に本番依存が増えた
- `package.json` の `dependencies` が増えた
- lockfileが更新されたが依存追加の説明がない
- DB migrationファイルが作られた
- 認証・権限関連ファイルが変更された
- `.env.example` が変更された
- `.env` が読まれた、または変更されようとした
- 外部APIクライアントが追加された
- 支払い・課金関連コードが追加された
- 個人情報を保存するDBカラムが追加された
- ファイル削除が一定数を超えた
- 変更ファイル数が上限を超えた
- 追加行数または削除行数が上限を超えた
- テストが失敗している
- buildが失敗している
- `rm -rf`、`git reset --hard`、`git clean -fd`、`sudo`、`chmod -R`、`curl | sh`、`npm install -g` が実行されようとした

初期しきい値:

```yaml
decision_gate:
  max_changed_files: 12
  max_added_lines: 800
  max_deleted_lines: 300
  max_deleted_files: 3
```

### 17.2 AIに判定させる条件

以下はReview / Decision Agentに判定させる。

- 仕様に曖昧さがある
- 実用スコープを超えている
- 実装方針が複数ある
- UI/UXの好みが分かれそう
- 技術的負債を受け入れるか判断が必要
- 将来拡張性と実装速度のトレードオフがある
- タスクが大きすぎて分割すべき

### 17.3 Decision Report形式

Decision Reportは、人間がapprove / reject / reviseだけで判断できる粒度に圧縮する。必須項目は次の通り。

- なぜ人間判断が必要か
- 判断対象の粒度: product / architecture / dependency / security / UX / cost / schedule
- 推奨判断
- 推奨理由
- 選択肢
- 各選択肢の影響: 実装量、リスク、将来変更容易性、セキュリティ、UI/UX、コスト、スケジュール
- 人間が選ぶべき観点
- 選択後に自動で行うこと
- 証拠: diff、ログ、テスト結果、該当task、該当PRD項目
- 推奨アクション

```markdown
# Decision Report: 認証方式の選定

## 1. なぜ人間判断が必要か

認証方式はproduct behavior、security、DB設計に影響し、project policyで自動決定できないため。

## 2. 判断対象の粒度

architecture / security

## 3. 推奨判断

A. Supabase Auth

## 4. 推奨理由

初期実装が速く、DBとの相性がよい。ユーザーごとのデータ分離を初期完成スコープ内で実現しやすい。

## 5. 選択肢

### A. Supabase Auth

メリット:
- 実装が速い
- DBとの相性がよい
- メール認証やOAuthに拡張しやすい

デメリット:
- Supabaseに依存する

### B. Clerk

メリット:
- 実装が速い
- UI付きの認証導入がしやすい

デメリット:
- 外部サービス依存が強い
- 初期実用版では過剰な可能性がある

### C. 自前認証

メリット:
- 自由度が高い

デメリット:
- セキュリティリスクが高い
- 初期実用版の主要ワークフローに対して重い

## 6. 各選択肢の影響

| Option | 実装量 | リスク | 将来変更容易性 | セキュリティ | UI/UX | コスト | スケジュール |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | medium | medium | medium | positive | neutral | medium | positive |
| B | medium | medium | medium | positive | positive | medium | positive |
| C | high | high | medium | negative | neutral | none | negative |

## 7. 人間が選ぶべき観点

外部サービス依存を許容して初期速度を取るか、自前実装の自由度とセキュリティ責任を取るか。

## 8. 選択後に自動で行うこと

選択した認証方式をArchitecture、Roadmap、Task YAML、policy memoryへ反映し、関連taskを再生成する。

## 9. 証拠

- related PRD: user data isolation
- affected tasks: TASK-004, TASK-005
- affected artifacts: .devagent/architecture.md, .devagent/roadmap.yaml

## 10. 推奨アクション

[Approve A] [Choose B] [Choose C] [Ask Revision] [Reject and Replan]
```

## 18. Git worktree設計

タスクごとにworktreeを作る。

```text
main repository
  └── .devagent-worktrees/
        TASK-001/
        TASK-002/
        TASK-003/
```

基本コマンド:

```bash
git worktree add .devagent-worktrees/TASK-003 -b devos/TASK-003 main
```

メリット:

- タスクごとに差分を分離できる
- 失敗した作業を破棄しやすい
- 並列実行しやすい
- mainを壊しにくい
- 差分レビューが明確になる

注意:

- 同じブランチを複数worktreeでcheckoutしない
- worktreeごとに依存インストールが必要になる場合がある
- build cacheやnode_modulesで容量が増える
- cleanupコマンドを用意する

## 19. サンドボックスとセキュリティ

基本方針:

- デフォルトはread-only
- 実装時だけworkspace-write
- `danger-full-access` はDockerやCI runnerなど外部隔離済み環境でのみ使う
- ホームディレクトリ全体を読ませない
- `.env` 本体は deny
- APIキーはプロンプトに入れない
- destructive commandは承認制
- Git diffとテスト結果を見ない限り `merged` にしない

推奨Codex実行:

```bash
codex exec \
  --sandbox workspace-write \
  --ask-for-approval untrusted \
  --json \
  -C .devagent-worktrees/TASK-003 \
  -
```

より厳密にする場合は `~/.codex/config.toml` にpermission profileを定義し、`.env` をdeny、ネットワークを必要ドメインだけallowする。

## 20. AGENTS.mdテンプレート

```markdown
# AGENTS.md

## Project Goal

このプロジェクトは、ユーザーのコンセプトから実用的なWebアプリを作るためのアプリケーションである。

## Working Agreements

- 変更前に対象タスクを読むこと。
- `.devagent/prd.md` と `.devagent/roadmap.yaml` を確認すること。
- 関係ないファイルを変更しないこと。
- 大規模なリファクタリングを勝手に行わないこと。
- 新しい本番依存パッケージを追加する場合はDecision Reportを作ること。
- 認証、権限、DBマイグレーション、外部API、課金、個人情報に関わる変更は必ずDecision Reportを作ること。
- 実装後は必ず関連テストを実行すること。
- テストがない場合は、可能な範囲で最小限のテストを追加すること。
- 失敗したテストを無視して完了扱いしないこと。

## Commands

- Backend test: `go test ./...`
- UI install: `pnpm --dir ui install`
- UI dev: `pnpm --dir ui dev`
- UI test: `pnpm --dir ui test`
- UI lint: `pnpm --dir ui lint`
- UI build: `pnpm --dir ui build`

## Code Style

- Goコードは小さなpackage境界と明示的なerror handlingを前提とする。
- UIのTypeScriptはstrict modeを前提とする。
- UIコンポーネントは小さく分割する。
- ビジネスロジックはUIから分離する。
- APIレスポンスには型を付ける。
- エラー処理を省略しない。

## Decision Report Required When

- 新しい外部サービスを導入する
- 新しい有料APIを使う
- DBスキーマを破壊的に変更する
- 認証・権限設計を変更する
- 個人情報を保存する
- 本番デプロイに影響する
- セキュリティ上の懸念がある
- タスクの仕様が曖昧で複数解釈できる
- 差分が大きくなりすぎた
```

## 21. メモリ設計

メモリは3種類に分ける。

### 21.1 User Preference Memory

ユーザーの好み。

- UIはシンプルで余白多め
- BackendはGo、UIはReact + TypeScriptを優先
- DBは最初はSQLiteまたはSupabaseを優先
- 中途半端な機能追加より、ユーザーの主要ワークフロー完了を優先
- 認証と個人情報は安全側に倒す

保存先:

- SQLite `memories`
- `.devagent/memory/user-preferences.md`

### 21.2 Project Memory

プロジェクト固有の判断。

- 認証はSupabase Authを採用
- 初期実用版ではチーム機能を含めない
- 通知機能は別Change Request
- 課金機能は要件外

保存先:

- SQLite `memories`
- `.devagent/memory/project-rules.md`

### 21.3 Implementation Memory

実装上の注意。

- Task型は `internal/tasks` 配下
- APIは `internal/api` 配下
- UIコンポーネントは `ui/src` 配下
- Goの入力検証は明示的なvalidate関数に寄せる
- UIのフォームバリデーションはReact側で最小限に行い、最終検証はGo APIで行う
- Backendテストは `go test ./...`
- UIテストはReact側のテストランナーを採用後に固定する
- E2Eは未導入

保存先:

- SQLite `memories`
- `.devagent/memory/implementation-notes.md`

## 22. 検証設計

プロジェクト単位で検証コマンドを定義する。

```yaml
verification:
  backend_test: go test ./...
  ui_install: pnpm --dir ui install
  ui_lint: pnpm --dir ui lint
  ui_test: pnpm --dir ui test
  ui_build: pnpm --dir ui build
```

タスク単位で追加検証を定義する。

```yaml
verification_commands:
  - go test ./...
  - pnpm --dir ui test
  - pnpm --dir ui lint
```

ルール:

- テスト失敗時は原因分類し、current diff起因ならrepair budget内で自動修復する
- build失敗時は原因分類し、current diff起因ならrepair budget内で自動修復する
- Decision未解決時は `merged` にできない
- 検証未実行の場合は `merged` にできない
- 検証が環境要因で実行できない場合は、その理由をRun Summaryに残す

## 23. 内部モジュール設計

```text
personal-dev-os/
  cmd/
    devos/
      main.go
    orchestrator/
      main.go
  internal/
    api/
    app/
    artifacts/
    codex/
    decisions/
    gitworktree/
    memory/
    projects/
    storage/
    tasks/
    verifier/
  ui/
    src/
  data/
    dev-os.sqlite
  projects/
    my-first-app/
```

主要関数:

```go
type CoreUseCases interface {
	CreateProject(ctx context.Context, concept string) (Project, error)
	GeneratePRD(ctx context.Context, projectID string) (Artifact, error)
	GenerateArchitecture(ctx context.Context, projectID string) (Artifact, error)
	GenerateRoadmap(ctx context.Context, projectID string) (Artifact, error)
	CreateTasks(ctx context.Context, projectID string) ([]Task, error)
	PickNextTask(ctx context.Context, projectID string) (*Task, error)
	RunImplementation(ctx context.Context, taskID string) (Run, error)
	RunVerification(ctx context.Context, taskID string) (VerificationResult, error)
	EvaluateDiff(ctx context.Context, taskID string) (DecisionGateResult, error)
	CreateDecisionReport(ctx context.Context, taskID string) (Decision, error)
	ApproveDecision(ctx context.Context, decisionID string, selectedOption string) (Decision, error)
	MergeTask(ctx context.Context, taskID string) error
	UpdateMemory(ctx context.Context, projectID string) error
}
```

型イメージ:

```ts
type RiskLevel = "low" | "medium" | "high";

type Task = {
  id: string;
  projectId: string;
  title: string;
  description: string;
  acceptanceCriteria: string[];
  dependencies: string[];
  status: TaskStatus;
  riskLevel: RiskLevel;
  suggestedCommands: string[];
  expectedFiles?: string[];
};

type Decision = {
  id: string;
  projectId: string;
  taskId?: string;
  title: string;
  background: string;
  options: {
    id: string;
    label: string;
    pros: string[];
    cons: string[];
  }[];
  recommendation: string;
  risk: string;
  status: "pending" | "approved" | "rejected" | "resolved";
  selectedOption?: string;
};
```

## 24. 実装順序

### Step 1: Core artifact model

project、artifact、artifact_version、task、run、verification_result、gate_result、decision、policy、change_request、trace_linkを先に作る。

### Step 2: Workflow state machine

TaskStatus、Run attempt、repair attempt、allowed transition、retry budget、stop reason、next actionを実装する。

### Step 3: Report schemas

run summary、verification result、gate result、decision report、change impact reportのschemaを作る。

### Step 4: CLI 初期完成スコープ

```text
devos init
devos spec
devos plan
devos tasks
devos run TASK-001
devos review TASK-001
devos inbox
devos decisions
devos approve DEC-001 --option A
devos change request "..."
devos change analyze CR-001
devos change approve CR-001 --option A
devos merge TASK-001
```

### Step 5: Codex execution

内部ではsandbox、approval policy、stdin prompt、JSONL captureを明示してCodex CLIを呼ぶ。

### Step 6: Verification + auto repair

verification command実行、failure classification、repair prompt再実行、baseline failure記録を入れる。

### Step 7: Decision Gate

`PASS` / `AUTO_REPAIR` / `AUTO_REPLAN` / `REPORT_ONLY` / `HUMAN_DECISION` / `HARD_BLOCK` を返す。

### Step 8: Human Inbox UI

最初のWeb UIはフルダッシュボードではなく、人間判断に必要なものだけを表示する。

### Step 9: Change Request flow

変更要求、影響分析、artifact更新案、task再分類を実装する。

### Step 10: Full dashboard

roadmap、task board、run log、semantic diff、policy editorを追加する。

## 25. 初期実用版の完了条件

初期実用版は以下を満たしたら完了とする。

- `devos init` で `.devagent/` と初期成果物が生成される
- PRD、Architecture、Roadmap、Task YAMLが保存される
- `devos run TASK-001` がworktreeを作ってCodexを実行する
- JSONLログ、stdout、stderr、diff、summaryが保存される
- verification commandが実行される
- テスト失敗時に最低1回は自動修復できる
- 修復不能な場合、原因分類が出る
- Decision Gateが `PASS` / `AUTO_REPAIR` / `AUTO_REPLAN` / `REPORT_ONLY` / `HUMAN_DECISION` / `HARD_BLOCK` を返す
- Decision Gateが依存追加、migration、env、auth、差分サイズ、検証失敗を検知する
- `devos review TASK-001` が構造化レビューを生成する
- Decision Reportが証拠、影響、why human required、after approval actionsを含む
- `devos inbox` が人間判断待ちだけを表示する
- Human Inboxで不足環境変数を入力し、secret値をログやDBに残さず `.env.local` またはsecret storeへ反映できる
- 未解決Decisionがあるタスクは `merged` にならない
- 承認後にmergeまたは手動適用できる
- 人間の変更要求を1件取り込み、PRD / Roadmap / Taskの更新案を生成できる

ワークフロー受け入れ条件:

- 新規プロジェクトで、コンセプト入力から承認済み差分の反映までを1回通せる
- 失敗した実行は、原因、ログ、差分、次の操作が残る
- 判断待ちの項目はHuman Inboxに集約され、タスク完了をブロックする
- ユーザーが確認できない変更はmergeまたは手動適用できない
- 主要ワークフロー上の必須操作が、外部の手作業メモに依存しない

## 26. 将来拡張

### 26.1 LangGraphを検討する条件

最初から必須ではない。以下が出てきたら検討する。

- 複数エージェントの分岐が増えた
- 中断・再開が複雑になった
- 長時間実行が増えた
- Human-in-the-loopをフレームワーク側で扱いたい
- 状態遷移をグラフとして可視化・検証したい

### 26.2 OpenHandsを検討する条件

Codex CLI/SDKよりも実行環境やGUI付きエージェント基盤を深く制御したくなったら検討する。初期実用版では主要ワークフローに対して重い可能性が高い。

### 26.3 GitHub連携

初期実用版の後でよい。まずローカルで完結させる。

追加候補:

- PR作成
- CI結果取得
- review comment取り込み
- release note生成

## 27. 最終推奨

v1の推奨設計は次で固定する。

- Local-first
- SQLite-backed
- Markdown/YAML artifact-based
- Git worktree execution
- Codex CLI first, Codex SDK later
- AGENTS.md instruction layer
- Explicit state machine
- Decision Gate
- JSONL run logs
- Diff review
- Test verification

自作する中核:

- Orchestrator
- Decision Gate
- Artifact Manager
- Task State Machine
- Local UI / CLI

自作しない中核:

- Code editing engine
- Shell execution agent
- Patch generation engine
- IDE
- CI/CD

この構成なら、最初は小さく作れて、後からSDK、LangGraph、OpenHands、GitHub連携、クラウド実行を足せる。最初から全部入りにしないことが、完成確率を上げる。
