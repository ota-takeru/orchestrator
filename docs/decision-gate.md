# Decision Gate

Decision Gateは、AIが勝手に進めてはいけない変更を止めるだけの仕組みではありません。検証結果、diff、ログ、artifact、trace linkを証拠として集め、次に何をするべきかを決めるPolicy Engineです。

## Gate Actions

Gateは必ず次のいずれかを返します。

| Status | Meaning | Human required |
| --- | --- | --- |
| `PASS` | 自動継続できる | no |
| `AUTO_REPAIR` | 実装runまたはrepair runへ戻す | no |
| `AUTO_REPLAN` | タスク分割、順序変更、scope内再計画を行う | no |
| `REPORT_ONLY` | 通知と記録はするがブロックしない | no |
| `HUMAN_INPUT` | 値入力やscope選択など、方針判断ではない人間操作が必要 | yes |
| `HUMAN_DECISION` | 人間判断が必要 | yes |
| `HARD_BLOCK` | 危険なので停止。承認だけでは進めない | yes |

`needs_decision` はTaskStatusであり、GateResultのstatusではありません。GateResultは上記actionを返し、状態遷移側が `needs_input`、`needs_decision`、`repairing`、`blocked_on_policy` などへ写像します。写像の正規仕様は [state-machine.md](state-machine.md) のGateResult Mappingを優先します。

## Human Action Types

Human Inboxで扱う人間の関与は、次の種類を分けます。

| Type | Meaning | Example |
| --- | --- | --- |
| Human Input | 値やscopeを入力すれば解決する | API key入力、対象runだけskip |
| Human Decision | 方針、リスク、仕様の判断が必要 | 依存追加、DB migration、UX方向選択 |
| Human Review | 最終差分を確認してmerge可否を決める | merge前レビュー |
| Policy Approval | 過去判断や明示policyに基づく自動承認 | このprojectではzod追加を許可 |

`REPORT_ONLY` はHuman Inboxの判断待ちに混ぜません。報告として表示してよいですが、タスクの判断待ち件数には含めません。

## Rule-Based Detectors

各ルールは detector、default_action、severity、escalation を持ちます。

```yaml
rules:
  - id: dependency_added
    detector: package_dependency_added
    default_action: HUMAN_DECISION
    severity: high

  - id: test_failed
    detector: verification_failed
    default_action: AUTO_REPAIR
    severity: medium
    escalate_after_attempts: 2

  - id: diff_too_large
    detector: diff_size_exceeded
    default_action: AUTO_REPLAN
    severity: medium

  - id: env_read_attempt
    detector: protected_file_access
    default_action: HARD_BLOCK
    severity: critical
```

初期detector:

| Detector | Default action |
| --- | --- |
| `package_dependency_added` | `HUMAN_DECISION` |
| `lockfile_changed_without_explanation` | `HUMAN_DECISION` |
| `db_migration_added` | `HUMAN_DECISION` |
| `auth_or_permission_changed` | `HUMAN_DECISION` |
| `env_example_changed` | `HUMAN_DECISION` |
| `protected_file_access` | `HARD_BLOCK` |
| `external_api_client_added` | `HUMAN_DECISION` |
| `payment_or_billing_added` | `HUMAN_DECISION` |
| `personal_data_storage_added` | `HUMAN_DECISION` |
| `diff_size_exceeded` | `AUTO_REPLAN` |
| `task_requires_pre_split` | `AUTO_REPLAN` |
| `task_mixes_human_blocking_and_non_blocking_work` | `AUTO_REPLAN` |
| `worker_budget_exhausted` | `REPORT_ONLY` or `HUMAN_DECISION` |
| `all_ready_work_blocked_by_same_decision` | `HUMAN_DECISION` |
| `verification_failed_current_diff` | `AUTO_REPAIR` |
| `verification_failed_existing_baseline` | `REPORT_ONLY` |
| `environment_failure` | `REPORT_ONLY` |
| `missing_environment_variable` | `HUMAN_INPUT` |
| `invalid_environment_variable` | `HUMAN_INPUT` or `HUMAN_DECISION` |
| `secret_value_in_diff` | `HARD_BLOCK` |
| `secret_value_in_logs` | `HARD_BLOCK` |
| `env_local_changed_by_codex` | `HARD_BLOCK` |
| `canonical_artifact_modified_by_codex` | `HARD_BLOCK` or `HUMAN_DECISION` |
| `schema_modified_by_codex` | `HARD_BLOCK` |
| `AGENTS_md_modified_by_codex` | `HUMAN_DECISION` or `HARD_BLOCK` |
| `policy_modified_by_codex` | `HUMAN_DECISION` |
| `dangerous_command_requested` | `HARD_BLOCK` |
| `merge_conflict_detected` | `AUTO_REPAIR` or `HUMAN_DECISION` |
| `merge_reverification_failed` | `AUTO_REPAIR` |
| `artifact_contradiction_minor` | `REPORT_ONLY` |
| `artifact_contradiction_major` | `HUMAN_DECISION` |
| `platform_mismatch_detector` | `HUMAN_DECISION` |
| `path_mapping_detector` | `HUMAN_DECISION` |
| `toolchain_detector` | `HUMAN_INPUT` or `REPORT_ONLY` |
| `cross_environment_write_detector` | `HARD_BLOCK` |

`diff_size_exceeded` はmicro task化のための事前基準ではありません。feature chunkとして実行した結果、diff size、verification、review、gate resultの証拠からadaptive splitが必要だと判断するtriggerです。

危険コマンドの初期例:

- `rm -rf`
- `git reset --hard`
- `git clean -fd`
- `sudo`
- `chmod -R`
- `curl | sh`
- `npm install -g`

危険コマンド検出は文字列blacklistだけに依存しません。command eventを構造化し、次を組み合わせて判定します。

- argv正規化後の実行ファイル、subcommand、flag
- shell経由かdirect execか
- `bash -c`、`sh -c`、`python -c` などの二段実行
- protected pathへのread/write/delete疑い
- worktree外write/delete疑い
- canonical artifact、schema、policy、AGENTS.mdへのwrite疑い
- network lane違反
- package install lane違反
- lifecycle script実行疑い

destructive commandは原則 `HARD_BLOCK` とし、通常のHuman Inbox承認だけで継続させません。必要な場合はisolated runner、手動操作、または設計変更としてDecision Reportを作ります。

Platform detector details:

```yaml
platform_mismatch_detector:
  - verification command references unknown environment
  - task requires Windows but no Windows-capable environment exists
  - task requires WSL/Linux but no WSL/Linux environment exists

path_mapping_detector:
  - mapping missing
  - mapping points outside allowed root
  - same_filesystem mapping has competing write owners
  - case-sensitive filename collision

toolchain_detector:
  - required toolchain missing
  - toolchain version invalid
  - admin setup required

cross_environment_write_detector:
  - sidecar attempted to write canonical worktree
  - PathMappingService bypass suspected
```

GateResult mapping:

| Condition | GateResult |
| --- | --- |
| missing toolchain | `HUMAN_INPUT` or `REPORT_ONLY` depending `required_for_merge` |
| admin/toolchain install required | `HUMAN_DECISION` or `HARD_BLOCK` |
| cross-environment unauthorized write | `HARD_BLOCK` |
| path mapping invalid | `HUMAN_DECISION` |
| optional sidecar verification failed | `REPORT_ONLY` by default |

Toolchain missingはEnvironment Input CardではなくToolchain Setup Cardへprojectionします。required verificationに必要な場合はmerge block、optional verificationの場合はデフォルトREPORT_ONLYです。

## Initial Thresholds

```yaml
decision_gate:
  max_changed_files: 12
  max_added_lines: 800
  max_deleted_lines: 300
  max_deleted_files: 3
```

## GateResult Schema

実装時はこのJSON shapeをCodex構造化出力と `.devagent/schemas/gate-result.schema.json` の正規形にします。SQLiteの `gate_results` には同じ意味の列として保存し、次アクションの詳細は `recommended_next_action_json` に入れます。

```json
{
  "status": "AUTO_REPAIR",
  "severity": "medium",
  "reason": "ui_test_failed",
  "evidence": [
    {
      "type": "command_result",
      "command": "pnpm --dir ui test",
      "exit_code": 1,
      "summary": "TaskForm validation test failed after current diff"
    }
  ],
  "recommended_next_action": {
    "type": "rerun_codex_repair",
    "prompt_template": "repair-from-verification-failure",
    "max_attempts_remaining": 2
  },
  "human_required": false
}
```

必須フィールド:

- `status`: `PASS` / `AUTO_REPAIR` / `AUTO_REPLAN` / `REPORT_ONLY` / `HUMAN_INPUT` / `HUMAN_DECISION` / `HARD_BLOCK`
- `severity`: `low` / `medium` / `high` / `critical`
- `reason`: machine-readableな短い理由
- `evidence`: 空配列不可。少なくとも検証結果、diff、ログ、artifact、trace linkのいずれかを含める
- `recommended_next_action`: `PASS` 以外では原則必須
- `human_required`: `HUMAN_INPUT`、`HUMAN_DECISION`、`HARD_BLOCK` では `true`

SQLiteの `gate_results.human_action_type` は、Human Inbox projectionを安定させるための補助列です。正規値は `input`、`decision`、`review`、`policy_approval` です。人間対応が不要なGateResultではNULLにします。

`action` という別フィールドは使いません。Gateのactionは `status` そのものです。

## AI-Assessed Conditions

Review / Decision Agentは、ルールだけでは分からない意味的リスクを判定します。ただし最終actionはPolicy Engineが決めます。

- 仕様に曖昧さがある
- 実用スコープを超えている
- 実装方針が複数ある
- UI/UXの好みが分かれそう
- 技術的負債を受け入れるか判断が必要
- 将来拡張性と実装速度のトレードオフがある
- タスクが大きすぎて分割すべき
- PRD、Architecture、Task、実装の間に矛盾がある

## Artifact Contradiction Detector

PRD、Architecture、Task、実装が矛盾していないかを検出します。通常のテストでは見つからない設計逸脱を扱うためのdetectorです。

例:

- Architectureがlocal-first / SQLiteを要求しているのに、外部hosted DB clientが追加された。
- PRDがHuman Inbox firstを要求しているのに、Task Boardがトップ画面になっている。

false positiveが出やすいため、初期状態では多くを `REPORT_ONLY` にします。ただし、次の原則違反は `HUMAN_DECISION` または既存ruleにより `HARD_BLOCK` へ上げます。

- local-firstを破る
- secret handlingを変える
- authを入れる
- cloud dependencyを入れる

## Decision Report Requirements

`HUMAN_DECISION` のときだけDecision Reportを必須にします。単なる説明文ではなく、人間が approve / reject / revise だけで判断できる粒度に圧縮します。

環境変数不足のように、人間が方針判断ではなく値入力だけを行えば解決するものは、`HUMAN_INPUT` として通常のDecision ReportではなくEnvironment Input CardをHuman Inboxへ出します。secret値の扱いは [environment-variables.md](environment-variables.md) に従います。

```markdown
# Decision Report: フォームバリデーション方式

## 1. なぜ人間判断が必要か
本番依存パッケージ追加はproject policyで自動承認できないため。

## 2. 判断対象の粒度
dependency / architecture

## 3. 推奨判断
A. zodを追加する。

## 4. 推奨理由
3つのフォームで同じvalidationが必要で、手書きでは重複が増える。今後API schemaと共有しやすい。

## 5. 選択肢
- A: zodを追加する
- B: 手書きvalidationで進める
- C: 今回の承認済みスコープではschema共有を要件外とし、現在のacceptance criteriaを満たす範囲で手書きvalidationを採用する。将来のschema共有は別Change Requestとして扱う

## 6. 各選択肢の影響
| Option | 実装量 | リスク | 将来変更容易性 | セキュリティ | UI/UX | コスト | スケジュール |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | low | medium | high | neutral | positive | low | positive |
| B | medium | low | medium | neutral | neutral | none | neutral |
| C | low | low | low | neutral | neutral | none | positive |

## 7. 人間が選ぶべき観点
依存追加を許容して実装速度と保守性を取るか、依存を増やさず手書きで進めるか。

## 8. 選択後に自動で行うこと
Aを選ぶと、policy memoryへ依存追加判断を記録し、TASK-003のrepair runを再開する。

## 9. 証拠
- changed files: `ui/package.json`, `ui/pnpm-lock.yaml`, `ui/src/TaskForm.tsx`
- verification: `pnpm --dir ui test`
- related task: `TASK-003`
- related PRD: `R-004 入力値バリデーション`

## 10. 推奨アクション
[Approve A] [Choose B] [Ask Revision] [Reject and Replan]
```

## Decision Question Quality

悪い質問:

```text
zodを入れていいですか？
```

良い質問:

```text
フォームバリデーションを今後UI側で型安全に拡張しやすくするため、本番依存としてzodを追加する案を推奨します。
判断軸は「依存追加を許容して実装速度と保守性を取るか、依存を増やさず手書きバリデーションで進めるか」です。
```
