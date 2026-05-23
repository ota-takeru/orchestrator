# Autonomy Loop

## Goal

Personal Dev OSの中核は、Codexに実装を委任することではなく、失敗を分類し、自動修復できるものを直し、仕様判断が必要なものだけ人間へ渡すことです。

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

Autonomy Loopは、最初に作ったTaskを最後まで機械的に消化するループではありません。TaskまたはTask Groupの完了後は、次の実装へ進む前にRolling Planning Checkpointを実行し、実装summary、diff、verification results、gate results、semantic behavior diff、未解決Decision、既存Roadmapを確認します。

Checkpointの結果、scope内で次のTaskを継続できる場合はそのまま進めます。Task Group再構成、Roadmap更新、Architecture更新、PRD更新、Human Decision Reportが必要な場合は、planning artifactとして提案を作り、Planning ConsolidatorとSerial Canonical Commitを通して反映します。人間承認が必要な変更はHuman Inboxへ送ります。

## Failure Classification

| Class | Meaning | Default action |
| --- | --- | --- |
| `current_diff` | 今回の変更で壊した | `AUTO_REPAIR` |
| `environment` | 依存未install、network、権限、ローカル環境 | `REPORT_ONLY` |
| `baseline` | 変更前から落ちている | `REPORT_ONLY` |
| `spec_gap` | 仕様が足りず実装判断できない | `HUMAN_DECISION` |
| `task_too_large` | 差分や影響範囲が大きすぎる | `AUTO_REPLAN` |
| `policy_blocked` | policy上進められない | `HUMAN_DECISION` or `HARD_BLOCK` |
| `unknown` | 分類不能 | repair予算内なら `AUTO_REPAIR`、超過後 `HUMAN_DECISION` |

## Baseline Verification

verification failureを分類するrunは、次の情報を必ず記録します。

- `base_commit`
- `head_commit`
- `baseline_verification_run_id`
- `implementation_verification_run_id`
- `changed_files`
- `touched_test_files`
- `failure_signature`

分類規則:

| Condition | Failure class |
| --- | --- |
| base commitでも同じfailure signatureが出る | `baseline` |
| base commitではpassし、head commitでfailする | `current_diff` |
| 環境変数、依存、network、permissionで実行不能 | `environment` |
| acceptance criteriaが曖昧で判定不能 | `spec_gap` |

baseline判定は推測で済ませません。可能なら実装前のbase commitで同じverification commandを実行します。実装前に取れなかった場合は、base worktreeまたはmain/base worktreeで同じcommandを再実行し、そのrun idを証拠として保存します。

baseline verificationの実行契約:

- base commit用worktreeはOrchestratorが作成し、run artifactはhead側と別runとして保存する。
- base/headでdependency install状態を共有しない。lockfileがhead側で変わった場合、base側はbase commitのlockfileに従う。
- base側のdependency setupが環境要因で不能な場合、`current_diff` と断定せず `environment` または `unknown` にする。
- flaky test疑いがある場合はfailure signatureと直近履歴を保存し、初期実装では自動pass扱いにしない。
- failure signatureはcommand、exit code、正規化stderr/stdout、失敗test名、主要stack frameから作る。
- baseline runの再利用は `base_commit + command + environment fingerprint + dependency fingerprint` が一致する場合だけ許可する。
- baseline verificationのコストが高い場合でも、未実行ならその事実をverification resultへ保存し、classification confidenceを下げる。

## Auto Repair Rules

人間に聞かず修復してよいもの:

- current diff起因のtest failure
- lint failure
- type error
- missing generated file
- acceptance criteriaの部分未達

人間に聞くもの:

- product behaviorが曖昧
- architecture変更が必要
- dependency、auth、DB schema、external API、payment、personal dataが関係する
- 修正がtask scopeを超える

## Auto Replan Rules

人間に聞かず再計画してよいもの:

- scopeを変えずにタスクを分割する
- 実装順序を変える
- 巨大diffを複数taskへ分ける
- 受け入れ条件を保持したままtechnical subtasksへ分解する

人間に聞くもの:

- PRD要件を削る
- UX方向性を変える
- architecture decisionを変える
- 初期完成スコープを変える

## Task Granularity

Orchestratorは、Codexを細かい作業手順に縛るためにtaskを分解しません。デフォルトでは、taskはユーザー価値または検証可能な成果に対応する `feature_chunk` として扱います。

分割は、実装手順ではなく、証拠に基づく安全境界で行います。

分割してよい理由:

- `task_too_large` gate result
- diff size threshold超過
- verification failureの原因が局所化できない
- DB schema、auth、external API、production dependency、personal dataが混ざる
- rollback単位が異なる
- 人間判断が必要な部分と不要な部分が混ざる

分割しない理由:

- 1ファイルごと
- 1コンポーネントごと
- source fileとtest fileの分離
- helper関数単位
- 実装手順単位

## Runtime Budgets

```yaml
run_policy:
  max_implementation_attempts: 3
  max_repair_attempts: 2
  max_total_runtime_minutes: 30
  auto_split_if_diff_exceeds:
    changed_files: 12
    added_lines: 800
```

`auto_split_if_diff_exceeds` は事前にtaskを細かくする基準ではありません。feature chunkとして実行した後、diff、verification、review、gate resultの証拠に基づいてadaptive splitするためのtriggerです。

予算超過は即failedではありません。`stop_reason`、`failure_class`、`next_action` を保存し、Human Inboxへ出すべきか、Environment Issue Reportとして通知するだけかをPolicy Engineが決めます。値入力で解決するものはEnvironment Input Card、入力では解決しない環境要因はEnvironment Issue Report、既存baseline問題はBaseline Issue Reportとして分けます。
