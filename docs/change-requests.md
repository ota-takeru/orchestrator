# Change Requests

## Purpose

人間があとから仕様、UI、技術方針、優先順位を変えたい場合、直接PRD、Architecture、Roadmap、Task、Memoryを書き換えません。Change Requestを作り、影響範囲分析と更新案を生成してから承認します。

## Feature Request vs Change Request

ユーザーが「欲しい機能」を自然文で複数伝えた場合、最初はFeature Requestとして保存します。

Feature Requestは、既存仕様に収まる場合は直接Task Groupへ展開できます。PRD、Architecture、Roadmap、Policyに影響する場合はChange Requestを作ります。

Feature Requestの詳細化、影響分析、Decision Report draft、Task Group proposalはplanning artifactとして並列生成できます。ただし、PRD、Architecture、Roadmap、Taskへの確定反映はPlanning Consolidator後のserial canonical commitで行います。

## Flow

```text
Human Change Input
  -> Change Request作成
  -> 影響範囲分析
  -> PRD / Architecture / Roadmap / Tasks / Memory の更新案生成
  -> 差分提示
  -> 人間承認
  -> 既存タスクの再分類
  -> 必要ならrollback / refactor / migration taskを生成
```

## Request Tiers

すべての変更要求を同じ重さで扱いません。小さなUI文言変更まで毎回PRD / Architecture / Roadmap全体の影響分析に回すと、人間の負担が大きくなります。

| Tier | Examples | Handling |
| --- | --- | --- |
| `minor_change` | UI copy、small layout、non-functional preference | lightweight change request。関連taskとUI artifactだけを確認する |
| `workflow_change` | user flow、feature behavior | normal change request。PRD / Roadmap / Tasksへの影響を分析する |
| `architecture_change` | DB、auth、external service、permission | full impact analysis。Decision GateとHuman Decisionを必須にする |

## YAML Example

```yaml
id: CR-004
title: タスク画面をカンバンではなく今日の実行リスト中心に変える
type: ui
status: proposed

requested_change: >
  タスク管理画面はカンバンより、今日やることに集中できる画面を優先したい。

impact_analysis:
  prd:
    - section: 主要画面
      impact: update
  architecture:
    - section: UI structure
      impact: minor_update
  roadmap:
    - task: TASK-006-kanban-board
      impact: replace
  tasks:
    obsolete:
      - TASK-006
    new:
      - TASK-011-today-focus-view
  task_impacts:
    keep:
      - TASK-003
    modify:
      - task: TASK-004
        reason: acceptance criteria must include Today Focus View
    obsolete:
      - TASK-006
    new:
      - TASK-011-today-focus-view
    split: []
    merge: []

recommendation:
  option: A
  summary: 初期完成スコープではToday Focus Viewを優先し、カンバンは別Change Requestとして扱う。

options:
  - id: A
    label: Today Focus Viewを優先
  - id: B
    label: カンバンのまま進める
  - id: C
    label: 両方入れるが初期完成スコープが大きくなる

after_approval:
  update_artifacts:
    - .devagent/prd.md
    - .devagent/architecture.md
    - .devagent/roadmap.yaml
  regenerate_tasks: true
  mark_obsolete_tasks:
    - TASK-006
  create_task_group:
    id: TG-004
    title: Today Focus View
  enqueue_work: true
```

## Impact Analysis Requirements

Change Impact Reportは次を必ず含めます。

- 変更種別: product / architecture / ui / policy / priority / technical
- 影響artifact: PRD / Architecture / Roadmap / Tasks / Memory
- obsoleteになるtask
- 追加されるtask
- repair / refactor / migration taskの必要性
- 既にmerge済みの実装への影響
- 推奨option
- 人間が選ぶべき判断軸
- 承認後に自動で行う更新

## Task Impact Classification

Change Impact Reportは既存taskを次の分類で扱います。

| Impact | Meaning |
| --- | --- |
| `keep` | 変更なしで継続 |
| `modify` | acceptance criteria、constraints、verificationを更新 |
| `split` | 既存taskを複数taskへ分割 |
| `merge` | 複数taskを1つのfeature chunkへ統合 |
| `obsolete` | 不要化 |
| `supersede` | 新task / task groupに置換 |
| `new` | 新規taskを追加 |
| `blocked` | decision / input / dependency待ち |

## Trace Links

影響分析は `trace_links` を使います。PRD requirement、Architecture decision、Task、Run、Verification、Decisionがつながっていない場合は、LLM推測だけに頼らず「trace不足」をreportに明記します。

`trace_links` はChange Request Flowの前提データです。Migration 001 coreで作成し、artifact承認、task materialize、run、verification、gate、decision / human approvalの各タイミングで最小linkを保存します。Change Request実装時に後付けmigrationとして作ると影響分析の根拠に使えないため、初期storage sliceで利用可能にします。
