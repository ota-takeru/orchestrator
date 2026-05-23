# Initial Complete Workflow Scope

このスコープはMVPではなく、最初に実用完了と認める品質境界です。

実装単位は小さくしてよいですが、ここに含まれるワークフローは途中までで完了扱いしません。コーディングエージェントは、最小実装、MVP、Phase 2、後回しという理由で acceptance criteria、verification、evidence、human approval flow を省略してはなりません。

この文書は品質境界であり、最初の実装タスク一覧ではありません。Coding AgentはInitial Complete Scope全体を一括実装してはいけません。実装は [implementation-start.md](implementation-start.md) と [implementation-plan.md](implementation-plan.md) のImplementation Sliceごとに行い、各sliceは状態遷移、DB制約、証拠保存、テストを伴って完了します。

## Target Experience

初期完成スコープでは、次の1体験を最後まで通します。

> コンセプトを入力すると、PRDとロードマップとタスクに分解され、最初のタスクをCodexに実装委任し、検証失敗を自動修復または分類し、人間判断が必要なものだけ証拠付きレポートとして承認できる。

重要なのは機能数ではなく、この体験が失敗時や判断待ちを含めて最後まで成立することです。

複数の自然文要望がある場合はFeature RequestとしてRequest Queueに積み、要件詳細化、影響分析、Decision Report draft、Task Group proposalはbounded parallel planningで進めます。canonical artifactへの反映、実装、mergeはsequentialに処理します。

## Bootstrap Workflow

Bootstrap Workflowは、Initial Complete Scopeへ到達するために最初に通す縦断フローです。最初からreal Codex adapterやFull Dashboardを接続せず、Fake Coding Agent Adapterで状態、証拠保存、gate、Human Inbox、merge前再検証の背骨を検証します。

```text
devos init
  -> devos spec
  -> devos plan
  -> artifact approve
  -> devos tasks materialize
  -> fake agentで固定diff生成
  -> Orchestratorがverification実行
  -> gate result保存
  -> inbox表示
  -> final review approval
  -> merge approval
  -> merge queue投入
  -> reverify
  -> merged
```

`devos init` は初期化だけを担当します。Bootstrap Workflowを1 commandで通す場合は `devos bootstrap --adapter fake` を使います。PRD生成は `devos spec`、Architecture / Roadmap / Task draft生成は `devos plan`、承認済みartifactからのtask ready化は `devos tasks materialize` が担当します。

Bootstrap WorkflowはInitial Complete Scopeを縮小するものではありません。Real Codex Adapter、bounded parallel planning、Change Request、Environment Input、Policy Memory、Dependency Risk Ledger、Human Inbox UIは、Fake adapterでrun artifact ownership、state machine、verification、gate、inbox projectionが検証された後に接続します。

## Included

1. コンセプト入力
2. PRD生成
3. Architecture生成
4. Roadmap生成
5. タスクYAML生成
6. 複数のFeature Requestを登録し、Request Queueで管理するフロー
7. taskを最初からmicro taskへ分割せず、必要時だけadaptive splitする方針
8. bounded parallel planningでFeature Detail Report、Impact Analysis Report、Decision Report Draft、Task Group Proposal、Risk Reportを生成するフロー
9. Planning Consolidatorが並列分析結果を統合し、重複、依存、stale snapshot、decision batchingを整理するフロー
10. canonical artifact / task / roadmapへの反映をserial commitするフロー
11. TaskまたはTask Group完了後にRolling Planning Checkpointを実行するフロー
12. sequential execution workerでREADY workを順番に処理するフロー
13. Human Decision / Environment Inputで止まったtaskがある場合でも、依存しないplanningまたはtaskを継続できるフロー
14. CLIでのタスク一覧表示
15. 1タスクをCodex CLIに実装委任
16. テスト、lint、build実行
17. 失敗原因分類
18. 自動修復を最低1回試せるrepair loop
19. diff保存とsemantic summary
20. Decision Gate
21. Decision Report生成
22. Human Inbox
23. 承認後のmergeまたは手動適用フロー
24. Change Request 1件の影響分析とartifact更新案生成
25. Human Inboxから不足環境変数を入力し、保存後に該当runを再実行するフロー
26. Merge Queueで最新mainへのrebase / reverify後にmergeするフロー
27. Semantic Behavior Diffを証拠付きでHuman Reviewへ表示するフロー
28. 本番依存追加をDependency Risk Ledgerへ記録するフロー
29. 安全なworktree cleanup dry-run
30. Windows-primary / WSL-primary / Hybrid のplatform model
31. primary_environment selection
32. environment-aware runner and verification command
33. Windows / WSL path mapping
34. platform doctor and toolchain setup card
35. Windows local runner and WSL local runner
36. required_for_merge / optional verification handling

## Excluded

- 独自コードエディタ
- 独自CI
- 複雑なマルチエージェント会話
- Slack / Linear / GitHub連携
- 複数ユーザー管理
- 課金
- クラウド実行基盤
- 本番デプロイ自動化
- concurrent task execution
- remote runners
- automatic Visual Studio / SDK / package manager installation
- automatic admin elevation
- automatic code signing certificate management
- parallel implementation across Windows and WSL

## Completion Criteria

初期完成スコープは、次を満たしたら完了です。

- `devos init` で `.devagent/` の初期ディレクトリと管理skeletonが生成される
- `devos init` は `.devagent/concept.md`、policy skeleton、schema registry copy、project record、preflight reportだけを作り、Taskをreadyにしない
- `devos spec` でPRD draft、`devos plan` でArchitecture / Roadmap / Task draftが生成される
- PRD、Architecture、Roadmap、Task YAMLが保存される
- `devos tasks materialize` が承認済みartifactだけからTask YAMLとcanonical taskを生成またはready化できる
- `devos artifacts approve ...` でPRD、Architecture、Roadmapの承認状態を保存できる
- PRD、Architecture、Roadmapは `approved` または `approved_with_notes` になるまで実装タスクを `ready` にしない
- `devos request` で複数の自然文要望を登録できる
- Feature Requestのplanningをbounded parallelで実行できる
- planning workerがplanning artifact / decision draft / task proposalだけを作り、canonical artifactを直接変更しない
- planning runがartifact snapshotを持ち、stale snapshotを検出できる
- Planning Consolidatorが並列分析結果を統合し、Decisionをbatch化できる
- canonical artifact / task / roadmapへの反映がserial commitで行われる
- Feature RequestがTask GroupまたはChange Requestへ展開される
- TaskまたはTask Group完了後にRolling Planning Checkpointが実行される
- Rolling Planning Checkpointが現状コード、diff、verification result、gate result、semantic behavior diff、未解決Decision、既存Roadmapから次のTask Group候補またはartifact更新案を生成できる
- Roadmap、Architecture、Task Breakdownは初期生成後も承認付きartifact versionとして更新できる
- execution workerがREADY workを順番に処理できる
- Human Inbox待ちのwork itemがあっても、依存しない別work itemを継続できる
- task分解はfeature chunkを基本とし、micro taskへの過剰分解を避けるpolicyがある
- `devos run TASK-001` がworktreeを作ってCodexを実行する
- JSONLログ、stdout、stderr、diff、summaryがOrchestrator-owned artifactとして保存される
- verification commandはOrchestrator process runnerだけが正規実行し、Codex自己申告のtest結果をsource of truthにしない
- base commit と head commit のverification結果が記録され、baseline failureを根拠付きで分類できる
- テスト失敗時に最低1回は自動修復できる
- 修復不能な場合、原因分類が出る
- Decision Gateが `PASS` / `AUTO_REPAIR` / `AUTO_REPLAN` / `REPORT_ONLY` / `HUMAN_INPUT` / `HUMAN_DECISION` / `HARD_BLOCK` を返す
- Decision Gateが依存追加、migration、env、auth、差分サイズ、検証失敗を検知する
- `devos review TASK-001` が構造化レビューを生成する
- semantic behavior diffがuser-visible、non-user-visible、riskをファイル証拠付きで示す
- Decision Reportがwhy human required、impact、evidence、after approval actionsを含む
- `devos inbox` が人間判断待ちだけを表示する
- Inbox ItemがDecision、Environment Input、Final Review、Report、Merge Conflictを表示用queueとして扱える
- Final ReviewとMerge Approvalは `human_approvals` がsource of truthで、Manual Applyの標準source of truthは `patch_applications` とし、Inbox Itemはprojectionとして同期される
- Human Inboxで不足環境変数を入力し、`.env.local` またはsecret storeへ反映できる
- secret値がSQLite、prompt、JSONL、stdout、stderr、summary、Decision Reportへ保存されない
- 未解決Decisionがあるタスクは `merged` にならない
- merge経路では承認後に `approved_for_merge`、`queued_for_merge`、`rebasing`、`reverifying` を通り、最新mainで再検証してからmergeできる
- 手動適用経路では `approved_for_merge`、`patch_exported`、`manually_applied`、`reverifying`、`applied` を通り、登録commitを再検証してから完了できる
- 人間の変更要求を1件取り込み、PRD / Roadmap / Taskの更新案を生成できる
- 承認済み判断がpolicy / memoryに反映され、次回以降の判断回数を減らせる
- policy memoryがscope、期限、失効条件を持てる
- 本番依存追加がDependency Risk Ledgerへ記録される
- `devos cleanup --dry-run` が削除予定worktreeを安全条件付きで表示できる
- Windows-primary profileでFake bootstrapが通る
- WSL-primary profileでFake bootstrapが通る
- Hybrid profileでsidecar optional verificationを記録できる
- Hybrid verificationで1つのverification runに複数environment resultを記録できる
- run artifactが複数commandのstdout/stderrを保存できる
- same_filesystem mappingで同時writeが拒否される
- proposed artifact versionがapproved contextを置き換えない
- missing toolchainがHuman Inbox Toolchain Setup Cardになる
- PatchApplicationがneeds_decision後に判断結果で再検証または別commit登録へ復帰できる

## Workflow Acceptance

- 新規プロジェクトで、コンセプト入力から承認済み差分の反映までを1回通せる。
- 失敗した実行は、原因、ログ、差分、次の操作が残る。
- 自動修復可能な失敗はHuman Inboxへ出さずにrepair loopへ戻る。
- 判断待ちの項目はHuman Inboxに集約され、タスク完了をブロックする。
- `HARD_BLOCK` は人間承認だけでは進めず、隔離環境や設計変更を要求する。
- ユーザーが確認できない変更はmergeまたは手動適用できない。
- 主要ワークフロー上の必須操作が、外部の手作業メモに依存しない。
