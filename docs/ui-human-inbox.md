# Human Inbox UI

## Principle

UIの主役はTask Boardではありません。人間の注意を最小化するため、最上位はHuman Inboxです。ログを見せるのではなく、判断に必要な証拠だけを圧縮して表示します。

## Top Level

```text
Project: Personal Dev OS

Autonomy Status:
- Running: 2 tasks
- Auto-repairing: 1 task
- Waiting for human: 2 decisions
- Blocked: 0
- Last successful merge: 18 minutes ago

Request Queue:
- Queued feature requests: 3
- Planning workers: 3 running
- Current work item: PLANRUN-021 impact analysis
- Waiting for human: DEC-014
- Continuing unblocked work: yes

Planning Summary:
- Completed feature requests: 4
- Ready without decision: Today View, AI priority suggestion
- Need decisions: PDF task generation, Slack notification

Needs Your Judgment:
1. DEC-014: Add production dependency for form validation
   Recommendation: Approve
   Risk: Medium
   Why you need to decide: production dependency policy
   [Review Report]

2. DEC-015: Choose dashboard layout direction
   Recommendation: Option B
   Risk: Low
   Why you need to decide: UX preference
   [Review Report]
```

## Primary Views

1. Human Inbox
   今、人間が判断すべきものだけを表示する。

2. Autonomous Run Monitor
   AIが自走している状態を表示する。操作は基本不要。

3. Request Queue / Worker Status
   非同期に何が進んでいるかを表示する。操作を強制する画面ではない。

4. Decision Report View
   判断理由、推奨、選択肢、証拠、影響を表示する。

5. Semantic Diff Review
   生diffではなく、仕様、ファイル、リスク別に整理したdiffを表示する。

6. Change Request Center
   後から変更したいことを入力し、影響分析と反映案を見る。

7. Policy / Preference Editor
   どこまで自動承認するか、何を必ず止めるかを管理する。

8. Run Trace / Logs
   必要時だけ掘る詳細ログ。

9. Environment Input
   不足している環境変数を人間が入力し、保存後に該当runを再実行する。詳細は [environment-variables.md](environment-variables.md) を参照する。

10. Platform Setup
    primary environment、runner capability、path mapping、toolchain setupをHuman Inboxで分離して扱う。

## Request Queue View

Human Inboxの隣に、Request Queue / Worker Statusを表示します。これは人間に操作を強制する画面ではなく、非同期に何が進んでいるかを把握するための画面です。

表示するもの:

- queued Feature Requests
- running planning workers
- planning consolidation status
- current work item
- waiting_for_human items
- skipped but unblocked items
- next candidate task
- last completed task
- stale planning artifacts
- serial canonical commit status

## Decision Batch View

並列planningの結果、Human InboxにはDecision Report draftを個別に並べず、関連する判断をbatch表示します。

```text
Planning completed for 4 feature requests.

Ready without decision:
- Today View
- AI priority suggestion

Need decisions:
- PDF task generation
  - file retention policy
  - OCR support
- Slack notification
  - external service integration
  - notification trigger policy

Recommended next:
- Implement Today View first
- Ask decisions for PDF and Slack
- Continue with AI priority suggestion while waiting
```

Decision batch cardは、次を明示します。

- どのFeature Requestに関係するか
- どの判断をまとめているか
- 推奨option
- 選ぶとcanonical commitで何が確定するか
- 選ばなくても継続できるunblocked workがあるか

## Inbox Triage Rules

Human Inboxは「全部承認待ち」にしません。人間が今判断すべきものだけを上に出し、報告と入力と最終レビューを分けます。

- すべてのカードに推奨アクションを1つ出す。
- default safe actionを明示する。
- 低リスクで同種の判断はbatch approveできる。
- 承認済み判断はpolicy memoryへ記録し、次回以降の同じ質問を減らす。
- 技術詳細、raw diff、raw logsは折りたたむ。
- 重大度順に並べる。
- `REPORT_ONLY` は判断待ちに混ぜず、報告として別表示にする。
- Toolchain missingはHuman Inputではない。
- Platform mapping invalidはHuman DecisionまたはSetup Card。
- Optional sidecar verification failureは、policyが昇格しない限りHuman waiting countに含めない。
- cancel / pause / resume をrun単位で提供する。

人間の関与種別:

| Type | Purpose | UI |
| --- | --- | --- |
| Human Input | 値やscopeを入力する | Environment Input Card |
| Human Decision | 方針やリスクを判断する | Decision Report |
| Human Review | merge前に最終差分を確認する | Semantic Diff Review |
| Policy Approval | 過去判断に基づき次回以降を減らす | Policy / Preference Editor |
| Platform Setup | 実行環境やrunner設定を整える | Platform Setup Card |
| Toolchain Setup | Git、MSBuild、SDK、bubblewrapなどを人間が用意する | Toolchain Setup Card |

## Inbox Item Model

Human Inboxに出るものはDecisionだけではありません。Environment Input Card、Final Review Approval、Baseline Issue Report、Policy Block、Change Request Approval、Merge Conflict Report、Dependency Approvalなどを同じ一覧で扱うため、UIは `inbox_items` を表示します。

`inbox_items` はsource of truthではなく、表示用のprojection / queueです。

source of truth:

- `decisions`
- `human_approvals`
- `environment_bindings`
- `gate_results`
- `verification_results`
- `change_requests`
- `dependency_risk_ledger`
- `patch_applications`

Final Review ApprovalとMerge Approvalは `human_approvals` がsource of truthです。Manual Applyは標準では `patch_applications` をsource of truthにし、`mark-applied` のhuman attestation、適用commit、patch hash、verify-applied結果を保存します。`inbox_items.item_type = approval` は表示と操作入口であり、承認済みevidence bundle、承認者、承認時刻、対象head commit、patch hashはsource側のevidenceに保存します。

item type:

- `human_decision`
- `human_input`
- `approval`
- `report`
- `hard_block`
- `change_request`
- `merge_conflict`
- `platform_setup`
- `toolchain_setup`

platform source type:

- `execution_environment`
- `path_mapping`
- `toolchain_requirement`
- `run_profile`

sourceがresolvedになったらinbox itemも同じtransactionでresolvedへ同期します。ズレが出た場合はsource側を正とし、inbox itemを再生成します。projection同期の正規ルールは [state-machine.md](state-machine.md) のInbox Projection Syncを優先します。

同期ルール:

- `dedupe_key` は `project_id + task_id + source_type + source_id + item_type` を基本にする。
- `batch_key` は同種低リスク判断だけに付ける。production dependency、DB migration、auth変更は原則batch不可。
- snooze中にsource severityが上がった場合はsnoozeを解除する。
- `REPORT_ONLY` は判断待ち件数に含めない。
- `hard_block` itemはdismiss不可。source解消、再計画、中止のいずれかでresolvedにする。
- resolved sourceが再openした場合は同じdedupe_keyで再openするか、履歴保持が必要なら新itemを作る。

## Decision Card

人間にはまずこの粒度だけ見せます。

```text
Decision: フォームバリデーション方式

Recommendation:
A. zodを追加する

Why:
- 3つのフォームで同じvalidationが必要
- 手書きだと重複が増える
- 今後API schemaと共有しやすい

Risk:
Medium - production dependency追加

Impact:
- package.json変更
- lockfile変更
- TaskForm / ProjectForm / SettingsFormに影響

Options:
[Approve A] [Use handwritten validation] [Ask for lighter alternative]
```

## Environment Input Card

環境変数不足は、通常のDecision Reportより軽い入力カードとして扱います。人間に実装方針を判断させるのではなく、必要な値を入力してもらい、Orchestratorが反映と再実行を行います。

```text
Action Required: Missing environment variables

Task:
TASK-008 外部API接続の疎通確認

Why:
OPENAI_API_KEY が未設定のため、verification commandを実行できません。

Input:
[OPENAI_API_KEY] [••••••••••••••••]

Apply to:
(*) Recommended: This project only
( ) This task run only
( ) User-level default for future projects

Reason:
このAPIキーは現在のプロジェクト検証にだけ必要です。

After Apply:
- .env.local に値を書き込む
- environment_bindings にredacted metadataを保存する
- RUN-20260521-008 を再実行する

Actions:
[Save and Rerun] [Skip verification that needs this key] [Mark this requirement as not needed]
```

このカードはsecret値を表示しません。保存後はredacted previewだけを表示し、UI stateから入力値を破棄します。

## Platform Setup Cards

追加するカード:

- Platform Setup Card
- Toolchain Setup Card
- Path Mapping Issue Card
- Runner Capability Issue Card

Toolchain Setup Card例:

```text
Action Required: Windows toolchain setup

Environment:
windows-main

Missing:
- MSBuild
- Windows SDK

Why:
TASK-004 の windows-build verification に必要です。

Actions:
[Mark installed and rerun doctor]
[Waive this check]
[Open setup instructions]
```

Toolchain Setup Cardはsecret値入力を持ちません。Environment Input Cardでtoolchain installを促してはいけません。

下層に置く詳細:

- changed files
- test results
- semantic diff
- raw diff
- logs
- related PRD section
- related task acceptance criteria
- required environment keys
- redacted environment binding status

## Merge Review Layers

merge前の表示は3層にします。

1. 何が変わったか、主なリスク、推奨アクション。
2. semantic diff、verification result、関連acceptance criteria。
3. raw diff、redacted logs、command events。

semantic diffは、ユーザーがまず「実際の挙動がどう変わったか」を把握できるようにします。各項目にはdiff由来の証拠を付けます。

```text
User-visible changes:
- タスク作成フォームにタイトル必須バリデーションが追加された
  Evidence: ui/src/TaskForm.tsx, ui/src/TaskForm.test.tsx

Non-user-visible changes:
- TaskRepositoryにCreateメソッドが追加された
  Evidence: internal/tasks/repository.go

Risk:
- 既存のTaskForm snapshot testを更新している
  Evidence: ui/src/TaskForm.test.tsx
```

LLMがdiffにない挙動を説明するリスクを下げるため、evidenceがないsemantic diff itemはHuman Reviewの上位表示に出しません。
evidence fileが対象runの `diff.patch` に存在しない場合、そのitemはinvalidとして扱います。line rangeが取れないitem、deleted file、generated fileだけを根拠にしたitemはconfidenceを下げて表示します。

## UX Rule

Human Inboxでは、次の問いに答える情報だけを最初に出します。

- なぜ人間判断が必要か
- 何を選べばよいか
- 推奨は何か
- リスクは何か
- 選ぶと何が自動で起きるか
