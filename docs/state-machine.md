# State Machine

この文書は、Task、Run、GateResult、Human Approval、Merge Queue、Manual Patch、Human Inbox projection、および主要planning entityの正規状態遷移を定義します。状態一覧だけではなく、許可される遷移、禁止遷移、GateResultからTaskStatusへの写像を実装仕様として固定します。

## Principles

- 状態変更は必ず1つのDB transaction内で `workflow_events` と一緒に保存する。
- TaskStatus、RunStatus、MergeQueueStatus は直接文字列代入せず、state machine APIだけで変更する。
- ProjectLifecycleStatus、ArtifactStatus、ArtifactVersionStatus、FeatureRequestStatus、PlanningRunStatus、PlanningArtifactStatus、DecisionReportDraftStatus、TaskGroupStatus、WorkQueueItemStatus、WorkerRunStatus、DecisionStatus、HumanApprovalStatus、ChangeRequestStatus、EnvironmentRequirementStatus、EnvironmentBindingStatus もstate machine APIまたは専用domain serviceだけで変更する。
- ExecutionEnvironmentStatus、RunProfileStatus、PathMappingStatus、TargetPlatformStatus、ToolchainRequirementStatus、CommandEventStatusもstate machine APIまたは専用domain serviceだけで変更する。
- 不正遷移は状態を変更せず error を返し、必要なら `workflow_events` に `invalid_transition_rejected` を記録する。
- `inbox_items` はsource of truthではない。状態遷移の結果として再生成または同期されるprojectionである。
- final reviewとmerge approvalのsource of truthは `human_approvals` であり、`inbox_items` ではない。manual applyの標準source of truthは `patch_applications` である。
- terminal stateからの再開は直接遷移ではなく、新しいtask、change request、または明示的なretry operationとして扱う。

## TaskStatus

この文書はTaskStatusの状態一覧と許可遷移の正です。[storage-schema.md](storage-schema.md) のCHECK制約とGo enumは、この一覧を反映します。[data-model.md](data-model.md) は概念説明であり、状態遷移や正規enumの優先ソースではありません。

| From | Allowed To | Trigger |
| --- | --- | --- |
| `proposed` | `ready` | PRD、Architecture、Roadmap、Task YAMLが承認済みで実装可能 |
| `proposed` | `needs_decision` | scope、仕様、設計判断が不足 |
| `proposed` | `cancelled` | 人間またはpolicyが中止 |
| `ready` | `implementing` | implementation run開始 |
| `ready` | `needs_input` | 実装開始前に必須入力が不足 |
| `ready` | `needs_decision` | 実装開始前にDecision Reportが必要 |
| `ready` | `cancelled` | 人間またはpolicyが中止 |
| `implementing` | `verifying` | implementation run成功、diff収集完了 |
| `implementing` | `repairing` | implementation runが自己修復可能な失敗で終了 |
| `implementing` | `needs_input` | Codex approval/input eventをOrchestratorが正規化 |
| `implementing` | `needs_decision` | scope外、依存、DB、auth等の判断が必要 |
| `implementing` | `blocked_on_policy` | HARD_BLOCK、protected path、secret混入 |
| `implementing` | `failed` | retry不能な実行失敗、または予算超過 |
| `verifying` | `reviewing` | verification PASS |
| `verifying` | `diagnosing` | verification failed/error |
| `verifying` | `blocked_on_environment` | verification runnerが環境不足を検出 |
| `verifying` | `failed` | verification runner自体の回復不能エラー |
| `diagnosing` | `repairing` | current diff起因かつrepair budget内 |
| `diagnosing` | `ready` | scope内のAUTO_REPLANで再実行可能 |
| `diagnosing` | `proposed` | artifact/task再生成が必要 |
| `diagnosing` | `blocked_on_environment` | baseline/environment要因 |
| `diagnosing` | `needs_decision` | spec gap、scope変更、依存判断 |
| `diagnosing` | `failed` | unknown failureかつ予算超過 |
| `repairing` | `verifying` | repair run成功 |
| `repairing` | `diagnosing` | repair run失敗 |
| `repairing` | `needs_decision` | repair中にscope外判断が発生 |
| `repairing` | `blocked_on_policy` | HARD_BLOCK |
| `reviewing` | `ready_for_human_review` | review、semantic diff、gate再評価がPASSまたはREPORT_ONLY |
| `reviewing` | `repairing` | review/gateがAUTO_REPAIR |
| `reviewing` | `proposed` | AUTO_REPLANでtask/artifact更新が必要 |
| `reviewing` | `needs_input` | HUMAN_INPUT |
| `reviewing` | `needs_decision` | HUMAN_DECISION |
| `reviewing` | `blocked_on_policy` | HARD_BLOCK |
| `needs_input` | `ready` | 実装開始前の入力解決 |
| `needs_input` | `verifying` | verification中の入力解決後に再検証 |
| `needs_input` | `cancelled` | 人間が中止 |
| `blocked_on_environment` | `ready` | 環境要因解決後に実装から再開 |
| `blocked_on_environment` | `verifying` | 環境要因解決後に検証から再開 |
| `blocked_on_environment` | `cancelled` | 人間が中止 |
| `needs_decision` | `ready` | 判断結果により実装前から再開 |
| `needs_decision` | `repairing` | 判断結果によりrepair続行 |
| `needs_decision` | `reviewing` | 判断結果によりreview続行 |
| `needs_decision` | `cancelled` | rejectまたは中止 |
| `blocked_on_policy` | `cancelled` | 中止 |
| `blocked_on_policy` | `proposed` | policy違反を避ける新taskへ再計画 |
| `ready_for_human_review` | `approved_for_merge` | 同一head commit / diff hash / verification evidenceに対するfinal review approvalとmerge approvalが `human_approvals` でapproved |
| `ready_for_human_review` | `repairing` | 人間レビューで修正要求 |
| `ready_for_human_review` | `needs_decision` | 人間レビューで判断要求 |
| `approved_for_merge` | `queued_for_merge` | merge queue投入 |
| `approved_for_merge` | `patch_exported` | 手動適用用patchをexport |
| `approved_for_merge` | `ready_for_human_review` | 承認取り消し |
| `queued_for_merge` | `rebasing` | queue worker開始 |
| `queued_for_merge` | `cancelled` | queueから中止 |
| `rebasing` | `reverifying` | rebase/merge成功 |
| `rebasing` | `merge_conflict` | conflict検出 |
| `rebasing` | `blocked_on_policy` | protected path等の違反 |
| `merge_conflict` | `rebasing` | conflict解決後に再試行 |
| `merge_conflict` | `needs_decision` | conflict解決方針が必要 |
| `merge_conflict` | `cancelled` | 中止 |
| `reverifying` | `merged` | merge前再検証とgate再評価がPASS |
| `reverifying` | `repairing` | current diff起因で修復可能 |
| `reverifying` | `blocked_on_environment` | 環境要因 |
| `reverifying` | `needs_decision` | 判断が必要 |
| `reverifying` | `blocked_on_policy` | HARD_BLOCK |
| `patch_exported` | `manually_applied` | 人間がpatch適用commitを登録 |
| `patch_exported` | `ready_for_human_review` | patch適用を中止して再レビューへ戻す |
| `patch_exported` | `cancelled` | 手動適用を中止 |
| `manually_applied` | `reverifying` | 登録commitに対して検証開始 |
| `manually_applied` | `needs_decision` | 適用commitが期待patchと一致しない |
| `reverifying` | `applied` | 手動適用commitの再検証とgate再評価がPASS |

Terminal state:

- `merged`
- `applied`
- `failed`
- `cancelled`

terminal stateから他のTaskStatusへ直接戻してはいけません。再開が必要な場合はChange Request、retry task、または新しいtaskを作ります。

`reverifying` の場合、必ず `reverify_context` を持ちます。`reverify_context = merge_queue_entry` または `patch_application` のどちらかです。merge queue由来の `reverifying` は `merged` にだけ進め、patch application由来の `reverifying` は `applied` にだけ進めます。この文脈は `runs.reverify_context_type` / `runs.reverify_context_id` をsource of truthとしてDBに保存し、必要に応じて `workflow_events.evidence_json` にも記録します。

`ready_for_human_review -> approved_for_merge` はFinal ReviewとMerge Approvalの両方が必要です。`devos review approve TASK` は `approval_type = final_review` をapprovedにしますが、この時点ではtaskは `ready_for_human_review` のままでもよいです。`devos merge approve TASK` は `approval_type = merge` をapprovedにします。同一head commit / diff hash / verification evidenceに対して両方がapprovedになった時だけ、同じtransactionで `approved_for_merge` へ進めます。head commit、diff hash、verification result、gate resultが変わったら両approvalを再要求します。

## Prohibited Transitions

以下は代表例です。実装ではallowed tableに存在しない遷移を全て拒否します。

| From | To | Reason |
| --- | --- | --- |
| `proposed` | `implementing` | 承認済みartifactとready化を飛ばしている |
| `implementing` | `merged` | verification、review、human approval、merge queueを飛ばしている |
| `verifying` | `merged` | reviewとmerge前手順が不足 |
| `needs_decision` | `merged` | 未解決Decisionを残している |
| `blocked_on_policy` | `approved_for_merge` | HARD_BLOCKは承認だけでは解除できない |
| `failed` | `implementing` | terminal stateから直接再開している |
| `merged` | `repairing` | main反映後の変更は新taskで扱う |
| `patch_exported` | `merged` | 手動適用commitの登録と再検証を飛ばしている |
| `manually_applied` | `applied` | Orchestrator verificationとGate再評価を飛ばしている |

## RunStatus

| Status | Meaning |
| --- | --- |
| `pending` | run record作成済み、process未開始 |
| `running` | processまたはOrchestrator runner実行中 |
| `succeeded` | runnerが期待成果物を生成し、schema検証に成功 |
| `failed` | runnerが失敗、またはschema/exit/event検証に失敗 |
| `cancelled` | 人間またはpolicyにより停止 |
| `timed_out` | process timeoutまたはidle timeout |
| `blocked` | approval、policy、環境要因で停止し、別sourceへprojection済み |

| From | Allowed To |
| --- | --- |
| `pending` | `running`, `cancelled` |
| `running` | `succeeded`, `failed`, `cancelled`, `timed_out`, `blocked` |
| `blocked` | `cancelled` |

`blocked` はprocess attemptとしてはterminalに近い停止状態です。人間入力、承認、環境修正、policy判断の後も同じrunを `running` に戻さず、新しいrun attemptを作ります。

`succeeded`、`failed`、`cancelled`、`timed_out` はrun terminal stateです。同じrunを再利用せず、新しいattemptを作ります。

例外として、Codex session resumeを明示採用した将来のlaneでは `blocked -> running` を許可できます。ただし初期標準pathでは禁止し、そのlaneは別のRunPolicy、監査ログ、resume対象session idを必須にします。

timestamp rule:

- `pending`: `started_at` はNULL、`completed_at` はNULL
- `running`: `started_at` はNOT NULL、`completed_at` はNULL
- terminal status: `started_at` と `completed_at` はNOT NULL

この検証はstate transition serviceで行います。SQLite CHECKで複雑に表現しません。

## ProjectLifecycleStatus

ProjectLifecycleStatusはUIの大まかな進捗表示に使います。正規の実装可否はartifact approval、TaskStatus、Decision/Inputの未解決状態で判定し、ProjectLifecycleStatus単体を権限判定に使ってはいけません。

| From | Allowed To | Trigger |
| --- | --- | --- |
| `concept` | `spec_ready`, `blocked` | PRD draft生成またはpreflight block |
| `spec_ready` | `roadmap_ready`, `blocked` | Architecture / Roadmap / Task draft生成 |
| `roadmap_ready` | `implementing`, `blocked` | approved artifactsからtask materialize |
| `implementing` | `blocked`, `complete` | project-level block、または全scope完了 |
| `blocked` | `spec_ready`, `roadmap_ready`, `implementing`, `complete` | block解消後に直前の有効段階へ復帰 |
| `complete` | `implementing` | `reopen_for_feature_request` または `reopen_for_change_request` による明示的reopen |

`complete` からの追加要望はFeature RequestまたはChange Requestとして扱います。ただし暗黙に `implementing` へ戻してはいけません。`reopen_for_feature_request` または `reopen_for_change_request` のworkflow eventを保存し、未解決のplanning episodeまたはtask groupが作成された場合だけ `implementing` へ戻します。

## Artifact Lifecycle

Artifact versionがtrusted contextのsource of truthです。`artifacts.approved_version_id` はtrusted contextとして使う最新approved versionを指し、`artifacts.latest_version_id` はdraft / proposedを含む最新versionを指します。承認判断そのものは `artifact_versions.status`、`reviewed_by`、`reviewed_at`、`approval_notes`、`rejected_reason` に保存します。

ArtifactVersionStatus:

| From | Allowed To | Trigger |
| --- | --- | --- |
| `draft` | `proposed`, `rejected`, `superseded` | agent生成完了、検証失敗、または新draftで置換 |
| `proposed` | `approved`, `approved_with_notes`, `rejected`, `superseded` | 人間レビュー |
| `approved` | `superseded` | Change Requestまたは新version承認 |
| `approved_with_notes` | `superseded` | Change Requestまたは新version承認 |
| `rejected` | `superseded` | 修正版を新versionとして作成 |

`approved`、`approved_with_notes`、`rejected` は同じversion内ではterminalです。修正する場合は同じversionを書き換えず、新しいartifact versionを作ります。v1 approved、v2 proposedが存在する間、trusted contextは `approved_version_id = v1` を使い、Change Requestやimpact analysisは `latest_version_id = v2` を使います。

ArtifactStatus:

| From | Allowed To | Trigger |
| --- | --- | --- |
| `draft` | `proposed`, `superseded` | current versionがproposed、または新versionで置換 |
| `proposed` | `approved`, `approved_with_notes`, `rejected`, `superseded` | current versionのレビュー結果 |
| `approved` | `superseded`, `proposed` | 新version draft/proposedが作成された場合は集約状態を更新 |
| `approved_with_notes` | `superseded`, `proposed` | 新version draft/proposedが作成された場合は集約状態を更新 |
| `rejected` | `proposed`, `superseded` | 修正版の新version作成 |

Task materialize条件:

- PRD、Architecture、Roadmapの `approved_version_id` がすべて `approved` または `approved_with_notes` のartifact versionを指している。
- `approved_with_notes` の `approval_notes` はTask YAML生成時のtrusted contextへ含める。
- `rejected` artifact versionからTaskを `ready` にしてはいけない。
- Change Requestで新versionを承認した場合、旧approved versionは `superseded` にし、`trace_links` で旧新versionをつなぐ。

## GateResult Mapping

GateResultはTaskStatusではありません。GateResultを受けたworkflow stepが、現在の工程に応じてTaskStatusへ写像します。

| GateResult | Default TaskStatus | Inbox Item | Notes |
| --- | --- | --- | --- |
| `PASS` | 次工程へ | no | verification後は`reviewing`、review後は`ready_for_human_review`、merge queueのreverification後は`merged`、manual applyのreverification後は`applied`へ進む |
| `AUTO_REPAIR` | `repairing` | no | repair budget内のみ。予算超過時は`needs_decision`または`failed` |
| `AUTO_REPLAN` | `proposed` or `ready` | maybe `report` | scope不変なら人間不要。artifact/task再生成が必要なら`proposed` |
| `REPORT_ONLY` | 状態維持または次工程へ | `report` | Human waiting countに含めない |
| `HUMAN_INPUT` | `needs_input` or `blocked_on_environment` | `human_input` | 環境変数、選択値、scope指定など。Decision Reportは不要 |
| `HUMAN_DECISION` | `needs_decision` | `human_decision` | Decision Report必須 |
| `HARD_BLOCK` | `blocked_on_policy` | `hard_block` | 承認だけでは進めない。隔離runner、設計変更、または中止が必要 |

代表例:

| Detector | GateResult | TaskStatus | RunStatus | Inbox Item |
| --- | --- | --- | --- | --- |
| `verification_failed_current_diff` | `AUTO_REPAIR` | `repairing` | verification runは`failed` | no |
| `verification_failed_existing_baseline` | `REPORT_ONLY` | 状態維持または`reviewing` | verification runは`succeeded`または`failed`を保存 | `report` |
| `missing_environment_variable` | `HUMAN_INPUT` | `blocked_on_environment` | runは`blocked` | `human_input` |
| `dependency_added` | `HUMAN_DECISION` | `needs_decision` | runは`blocked` | `human_decision` |
| `secret_value_in_diff` | `HARD_BLOCK` | `blocked_on_policy` | runは`blocked` | `hard_block` |

## Retry Budget

初期値:

```yaml
retry_budget:
  max_implementation_attempts: 3
  max_repair_attempts_per_task: 2
  max_repair_attempts_per_failure_signature: 1
  max_rebase_attempts: 2
  max_reverify_repair_attempts: 1
```

同じ `failure_signature` に対するrepairが失敗した場合は、同じ修復を繰り返さず `needs_decision`、`blocked_on_environment`、または `failed` へ進めます。

## Merge Queue Sync

`merge_queue_entries.status` と `tasks.status` は次のように同期します。

MergeQueueStatus:

| From | Allowed To |
| --- | --- |
| `queued` | `rebasing`, `cancelled` |
| `rebasing` | `reverifying`, `merge_conflict`, `cancelled` |
| `merge_conflict` | `rebasing`, `cancelled` |
| `reverifying` | `merged`, `merge_conflict`, `cancelled` |

`merged` と `cancelled` はterminalです。

TaskStatusとの同期は同じtransactionで行います。

| Event | TaskStatus |
| --- | --- |
| Task `approved_for_merge` -> MergeQueue `queued` | `queued_for_merge` |
| MergeQueue `rebasing` | `rebasing` |
| MergeQueue `reverifying` | `reverifying` |
| MergeQueue `merged` | `merged` |
| MergeQueue `merge_conflict` | `merge_conflict` |

| MergeQueueStatus | TaskStatus |
| --- | --- |
| `queued` | `queued_for_merge` |
| `rebasing` | `rebasing` |
| `reverifying` | `reverifying` |
| `merge_conflict` | `merge_conflict` |
| `merged` | `merged` |
| `cancelled` | `cancelled` or `ready_for_human_review` |

queue workerは、taskに未解決の `HUMAN_INPUT`、`HUMAN_DECISION`、`HARD_BLOCK`、未完了verification、未保存diff artifactがある場合は開始してはいけません。

## HumanApprovalStatus

`human_approvals` は、人間がどの証拠に対して承認したかを保存するsource of truthです。Final ReviewとMerge ApprovalはDecisionではなくHuman Approvalとして扱います。

| From | Allowed To | Trigger |
| --- | --- | --- |
| `open` | `approved` | 人間が提示されたevidence bundleを承認 |
| `open` | `rejected` | 人間が差し戻し |
| `open` | `revised` | 人間が修正条件付きで返却 |
| `open` | `cancelled` | 関連task/runが中止 |
| `approved` | `revoked` | merge queue投入前に承認を取り消し |
| `rejected` | `open` | 新しいrun/review evidenceで再提示 |
| `revised` | `open` | 修正後のevidenceで再提示 |

`approved` は、対象taskが `queued_for_merge`、`patch_exported`、`merged`、`applied` のいずれかに進んだ後は履歴として固定します。`revoked`、`cancelled` はterminalです。

承認条件:

- `approval_type = final_review` は `semantic_behavior_diffs`、review result、GateResult、diff hash、head commitをevidenceに含める。
- `approval_type = merge` はfinal review approval id、対象head commit、未解決Decision/Inputがないこと、verification result idsをevidenceに含める。
- `approval_type = manual_apply` は初期標準pathでは使いません。project policyが手動適用後の追加承認を要求する場合だけ、verify-applied後のfinal acknowledgementとして使い、exported patch id、適用先commit、verify-applied結果をevidenceに含めます。
- `inbox approve` は `inbox_items` を直接sourceにせず、対応する `human_approvals`、`decisions`、`environment_bindings`、`change_requests` のいずれかを更新する。

## Manual Patch Application

手動適用フローは、merge queueとは別の反映経路です。外部都合でOrchestratorがmainへmergeできない場合でも、patch export、適用commit登録、再検証、Gate再評価の証拠を残します。

標準経路では、`devos patch mark-applied TASK-001 --commit <sha>` はapprovalではなくhuman attestationです。「このcommitにpatchを適用した」という申告を `patch_applications` に保存します。`applied` はOrchestratorがpatch一致確認、verification、Gate再評価を通した後だけ許可します。

PatchApplicationStatus:

| From | Allowed To | Trigger |
| --- | --- | --- |
| `exported` | `manually_applied` | 人間が適用commitを登録 |
| `exported` | `cancelled` | 手動適用を中止 |
| `manually_applied` | `verifying` | Orchestratorが適用commitの検証開始 |
| `verifying` | `verified` | patch一致確認、verification、Gate再評価がPASS |
| `verifying` | `needs_decision` | patch不一致、追加差分、検証失敗、policy risk |
| `verifying` | `failed` | 回復不能な検証失敗 |
| `needs_decision` | `manually_applied` | 人間判断後に別commitを登録し直す |
| `needs_decision` | `verifying` | 人間判断後に同じcommitで再検証 |
| `needs_decision` | `cancelled` | manual applyを中止 |
| `needs_decision` | `failed` | 継続不能として終了 |

TaskStatusとの同期:

| PatchApplicationStatus | TaskStatus |
| --- | --- |
| `exported` | `patch_exported` |
| `manually_applied` | `manually_applied` |
| `verifying` | `reverifying` |
| `verified` | `applied` |
| `needs_decision` | `needs_decision` |
| `cancelled` | `ready_for_human_review` or `cancelled` |

## Non-Task State Machines

主要entityは以下のallowed transitionだけを許可します。詳細な副作用は各domain serviceで定義しますが、表にない遷移は拒否します。

### ExecutionEnvironmentStatus

| From | Allowed To |
| --- | --- |
| `detected` | `configured`, `invalid`, `disabled` |
| `configured` | `checking`, `ready`, `invalid`, `disabled` |
| `checking` | `ready`, `missing`, `invalid`, `disabled` |
| `ready` | `checking`, `invalid`, `disabled` |
| `missing` | `configured`, `disabled` |
| `invalid` | `checking`, `configured`, `disabled` |
| `disabled` | `configured` |

### RunProfileStatus

| From | Allowed To |
| --- | --- |
| `draft` | `active`, `invalid`, `disabled` |
| `active` | `invalid`, `disabled` |
| `invalid` | `active`, `disabled` |
| `disabled` | `active` |

### PathMappingStatus

| From | Allowed To |
| --- | --- |
| `active` | `invalid`, `disabled` |
| `invalid` | `active`, `disabled` |
| `disabled` | `active` |

### TargetPlatformStatus

| From | Allowed To |
| --- | --- |
| `draft` | `active`, `unsupported`, `disabled` |
| `active` | `unsupported`, `disabled` |
| `unsupported` | `active`, `disabled` |
| `disabled` | `active` |

### ToolchainRequirementStatus

| From | Allowed To |
| --- | --- |
| `missing` | `detected`, `setup_required`, `waived`, `unsupported` |
| `setup_required` | `detected`, `waived`, `unsupported` |
| `invalid` | `detected`, `setup_required`, `waived`, `unsupported` |
| `detected` | `invalid`, `revoked` |
| `waived` | `missing`, `detected`, `revoked` |
| `unsupported` | `missing`, `revoked` |
| `revoked` | `missing` |

### CommandEventStatus

| From | Allowed To |
| --- | --- |
| `pending` | `running`, `cancelled` |
| `running` | `succeeded`, `failed`, `timed_out`, `blocked`, `cancelled` |
| `blocked` | `cancelled` |

CommandEventは再開しません。人間判断後は新しいcommand_eventを作ります。

timestamp ruleはRunStatusと同じです。`pending` は `started_at` / `completed_at` がNULL、`running` は `started_at` のみNOT NULL、terminal statusは両方NOT NULLです。

### FeatureRequestStatus

| From | Allowed To |
| --- | --- |
| `queued` | `analyzing`, `cancelled`, `superseded` |
| `analyzing` | `planned`, `waiting_for_human`, `cancelled`, `superseded` |
| `planned` | `running`, `waiting_for_human`, `completed`, `cancelled`, `superseded` |
| `running` | `waiting_for_human`, `completed`, `cancelled`, `superseded` |
| `waiting_for_human` | `planned`, `running`, `cancelled`, `superseded` |

`completed`、`cancelled`、`superseded` はterminalです。

### PlanningRunStatus

| From | Allowed To |
| --- | --- |
| `queued` | `running`, `cancelled`, `stale` |
| `running` | `succeeded`, `failed`, `cancelled`, `stale` |
| `failed` | `queued`, `cancelled`, `stale` |

`succeeded`、`cancelled`、`stale` はterminalです。再分析は新しいplanning runを作ります。

### PlanningArtifactStatus

| From | Allowed To |
| --- | --- |
| `draft` | `proposed`, `rejected`, `stale` |
| `proposed` | `accepted`, `rejected`, `superseded`, `stale` |
| `accepted` | `superseded`, `stale` |

`rejected`、`superseded`、`stale` はterminalです。`accepted` はSerial Canonical Commit前なら取り消しではなく新artifactで置換します。

### DecisionReportDraftStatus

| From | Allowed To |
| --- | --- |
| `draft` | `batched`, `promoted`, `rejected`, `stale` |
| `batched` | `promoted`, `rejected`, `superseded`, `stale` |
| `promoted` | `superseded` |

`rejected`、`superseded`、`stale` はterminalです。`promoted` 後のsource of truthは `decisions` です。

### TaskGroupStatus

| From | Allowed To |
| --- | --- |
| `proposed` | `ready`, `waiting_for_human`, `cancelled` |
| `ready` | `running`, `waiting_for_human`, `completed`, `cancelled` |
| `running` | `waiting_for_human`, `completed`, `cancelled` |
| `waiting_for_human` | `ready`, `running`, `cancelled` |

`completed`、`cancelled` はterminalです。

### WorkQueueItemStatus

| From | Allowed To |
| --- | --- |
| `queued` | `leased`, `cancelled` |
| `leased` | `running`, `heartbeat_lost`, `queued`, `blocked`, `cancelled` |
| `running` | `heartbeat_lost`, `waiting_for_human`, `blocked`, `completed`, `failed`, `cancelled` |
| `heartbeat_lost` | `queued`, `failed`, `cancelled` |
| `waiting_for_human` | `queued`, `cancelled` |
| `blocked` | `queued`, `failed`, `cancelled` |
| `failed` | `queued`, `cancelled` |

`completed`、`cancelled` はterminalです。`failed -> queued` は `attempt_no < max_attempts` の場合だけ許可します。

lease recovery:

- worker起動時は、`leased` または `running` で `lease_expires_at < now` のitemを検出する。
- `attempt_no` は `queued -> leased` でlease取得に成功した時だけ増やす。
- recovery可能なら `leased/running -> heartbeat_lost -> queued` とし、recovery時には `attempt_no` を増やさず、`workflow_events` に `work_queue_lease_recovered` を記録する。
- `attempt_no >= max_attempts` の場合は `failed` にし、`error_json` を保存する。
- `idempotency_key` が同じopen itemを重複作成してはいけない。

### WorkerRunStatus

| From | Allowed To |
| --- | --- |
| `running` | `paused`, `stopped`, `failed`, `heartbeat_lost` |
| `paused` | `running`, `stopped`, `failed` |
| `heartbeat_lost` | `failed`, `stopped` |

`stopped`、`failed` はterminalです。

### DecisionStatus

| From | Allowed To |
| --- | --- |
| `open` | `approved`, `rejected`, `revised`, `superseded` |
| `revised` | `open`, `approved`, `rejected`, `superseded` |

`approved`、`rejected`、`superseded` はterminalです。Final ReviewやMerge Approvalは `decisions` ではなく `human_approvals` を使います。

### ChangeRequestStatus

| From | Allowed To |
| --- | --- |
| `proposed` | `impact_analyzed`, `rejected`, `cancelled` |
| `impact_analyzed` | `approved`, `rejected`, `cancelled` |
| `approved` | `applying`, `cancelled` |
| `applying` | `applied`, `needs_decision`, `failed` |
| `needs_decision` | `approved`, `rejected`, `cancelled` |

`applied`、`rejected`、`cancelled`、`failed` はterminalです。

### Environment Status

EnvironmentRequirementStatus:

| From | Allowed To |
| --- | --- |
| `missing` | `requested`, `waived`, `configured` |
| `requested` | `configured`, `waived`, `cancelled` |
| `configured` | `invalid`, `revoked` |
| `invalid` | `requested`, `configured`, `waived` |

EnvironmentBindingStatus:

| From | Allowed To |
| --- | --- |
| `missing` | `configured`, `revoked` |
| `configured` | `invalid`, `revoked` |
| `invalid` | `configured`, `revoked` |

`waived`、`cancelled`、`revoked` はterminal扱いです。再設定は新しいrequirement/binding recordで扱います。

## Inbox Projection Sync

`inbox_items.source_type/source_id` はsource of truthを指します。source更新とinbox同期は同一transactionで行います。

| Source State | Inbox Sync |
| --- | --- |
| source open | matching open itemを作成または更新 |
| source resolved | matching open/snoozed itemを`resolved`へ変更 |
| source rejected/cancelled | itemを`resolved`または`dismissed`へ変更し、理由を保持 |
| source reopened | dedupe_keyが同じitemを再open。解決済みitemを履歴として残す場合は新itemを作る |

`dedupe_key` は `project_id + task_id + source_type + source_id + item_type` を基本にします。同じroot causeを複数runで共有する場合だけ `failure_signature` または `decision_type` を含めます。

`batch_key` は同種判断をまとめるため、`project_id + item_type + source_type + risk_family` を基本にします。production dependencyなど個別証拠が重要なものはbatch不可にできます。

`hard_block` itemはdismiss不可です。sourceが解消、再計画、中止のいずれかになるまで残します。

`REPORT_ONLY` itemはHuman Inboxの判断待ち件数に含めません。通知、履歴、run summaryには表示できます。

## Invalid Transition Handling

不正遷移を検出した場合:

1. DB更新をrollbackする。
2. callerへtyped errorを返す。
3. 可能なら `workflow_events` に `invalid_transition_rejected` を記録する。
4. UI/APIでは現在状態、要求状態、必要な正規操作を返す。

不正遷移を自動で別遷移に変換してはいけません。状態遷移の曖昧さは実装バグとして扱います。
