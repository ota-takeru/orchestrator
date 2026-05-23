# Architecture

## Core Architecture

```text
Local Web UI / CLI
  -> Orchestrator Core (Go)
  -> SQLite / Orchestrator-owned artifacts
  -> Platform Manager
      -> Windows Platform Adapter
          -> PowerShell Runner
          -> Git for Windows Provider
          -> Codex Windows Adapter
          -> Windows Verification Runner
      -> WSL / Linux Platform Adapter
          -> Bash Runner
          -> Linux Git Provider
          -> Codex WSL Adapter
          -> Linux Verification Runner
      -> Path Mapping Service
  -> Verification / Diff / Evidence / Decision Report
```

## Layers

```text
Local UI
  - Concept Chat
  - Human Inbox
  - Autonomous Run Monitor
  - Decision Report View
  - Semantic Diff Review
  - Change Request Center
  - Environment Input
  - Policy / Preference Editor
  - Run Trace / Logs

Orchestrator API
  - workflow state machine
  - platform manager
  - execution environment registry
  - runner dispatcher
  - path mapping service
  - toolchain doctor
  - run profile resolver
  - context builder
  - request intake
  - planner
  - planning lane
  - planning consolidator
  - canonical commit coordinator
  - work queue
  - lane scheduler
  - execution worker
  - scheduler
  - repair loop controller
  - task dispatcher
  - merge queue
  - policy engine
  - decision gate
  - evidence collector
  - semantic behavior diff generator
  - change impact analyzer
  - environment manager
  - artifact manager
  - memory manager
  - worktree garbage collector

Project State
  - SQLite
  - Markdown artifacts
  - YAML task definitions
  - JSONL run logs

Execution Layer
  - Platform adapters
  - Runner protocol
  - Codex adapter per environment
  - sandbox profile per environment
  - Git provider per environment
  - worktree manager per environment
  - verification runner per environment
  - path mapping service

Local Repository
  - source code
  - AGENTS.md
  - tests
  - generated docs
```

## Platform Model

Platform Modelの正は [platform-model.md](platform-model.md) です。Core workflow、state machine、DB schema、Human Inbox、Decision Gateは共通にし、process runner、shell runner、Git provider、worktree manager、Codex adapter、verification runner、path mapping、sandbox profile、toolchain doctorはenvironment別adapterへ分離します。

projectは必ず1つのprimary environmentを持ち、canonical Git / merge / artifact writeのsource of truthを1つに固定します。

## Runner Architecture

Runner Protocolの正は [runner-protocol.md](runner-protocol.md) です。Coreは `RunCommandRequest` を作り、Platform Managerが対象environmentのrunnerへdispatchします。command evidenceは `command_events` と `run_artifacts` に保存し、1つのverification runが複数environmentのcommand resultを持てます。

## Recommended Stack

| Area | Recommendation |
| --- | --- |
| UI | Local Web UI + CLI |
| Frontend | React / TypeScript / Vite / Tailwind / shadcn/ui |
| Backend | Go |
| DB | SQLite |
| SQLite access | `database/sql` + SQLite driver |
| State management | explicit state machine |
| Coding execution | Codex CLI first, Codex SDK later |
| Workspace isolation | Git worktree |
| Project memory | Markdown + YAML + SQLite |
| Approval | Human Inbox / Decision Report |
| Human attention | Human Inbox first, Task Board second |
| Logs | JSONL + Markdown summary + Orchestrator-owned immutable run artifacts |
| Validation | Go validation + JSON Schema for Codex outputs |

## Internal Modules

```text
cmd/
  devos/
    main.go
  orchestrator/
    main.go
internal/
  api/
  app/
  artifacts/
  changes/
  context/
  decisions/
  evidence/
  environment/
  platforms/
  runners/
  pathmap/
  gitproviders/
  toolchains/
  runprofiles/
  capabilities/
  gitworktree/
  memory/
  planner/
  planningartifacts/
  policies/
  projects/
  repair/
  requests/
  scheduler/
  storage/
  tasks/
  verifier/
  worker/
  workqueue/
  consolidator/
  canonicalcommit/
  mergequeue/
  cleanup/
  semanticdiff/
  codex/
    windows/
    wsl/
ui/
  src/
data/
  dev-os.sqlite
projects/
```

既存 `gitworktree/`、`verifier/`、`codex/` は、環境非依存のinterfaceと共通ロジックだけにします。OS固有実装は `platforms/`、`runners/`、`gitproviders/`、`codex/windows/`、`codex/wsl/` 側へ寄せます。

## Context Builder

Context builderはCodexへ渡す入力を組み立てます。実装仕様として渡せるのは [index.md](index.md) のCanonical Implementation Docs、AGENTS.md、Task YAML、承認済みartifactだけです。

ルール:

- `docs/archive/*` は実装仕様として渡さない。
- obsolete/non-canonical docs、旧MVPスコープ文書は除外する。
- repository files、logs、test output、external docsはuntrusted dataとして明示する。
- protected path、secret、`.env`、`.env.local` は読み込まない。
- contextに含めたartifact version、hash、pathをrun recordへ保存する。

## Service Interfaces

CoreUseCasesを巨大な実装interfaceとして作ってはいけません。下の分類は責務境界です。CLIやAPI層でfacadeを置くことはできますが、domain/application層のmock対象は小さなservice interfaceに分けます。

```go
type ProjectService interface {
	CreateProject(ctx context.Context, concept string) (Project, error)
	GetProject(ctx context.Context, projectID string) (Project, error)
	ArchiveProject(ctx context.Context, projectID string) error
}

type ArtifactService interface {
	GeneratePRDDraft(ctx context.Context, projectID string) (ArtifactVersion, error)
	GeneratePlanDrafts(ctx context.Context, projectID string) ([]ArtifactVersion, error)
	ApproveArtifactVersion(ctx context.Context, artifactID string, version int, input ApprovalInput) (ArtifactVersion, error)
	CreateTasksFromApprovedArtifacts(ctx context.Context, projectID string) ([]Task, error)
}

type PlanningService interface {
	CreateFeatureRequest(ctx context.Context, projectID string, input string) (FeatureRequest, error)
	StartPlanning(ctx context.Context, projectID string, input PlanningInput) ([]PlanningRun, error)
	ConsolidatePlanningResults(ctx context.Context, projectID string, input ConsolidationInput) (ConsolidationResult, error)
	CommitPlanningProposal(ctx context.Context, consolidationID string) (CanonicalCommitResult, error)
}

type WorkQueueService interface {
	EnqueueWork(ctx context.Context, input WorkQueueInput) (WorkQueueItem, error)
	PickNextWorkItem(ctx context.Context, projectID string) (*WorkQueueItem, error)
	StartWorker(ctx context.Context, projectID string, input WorkerInput) (WorkerRun, error)
}

type ExecutionService interface {
	RunImplementation(ctx context.Context, taskID string) (Run, error)
	RunAutoRepair(ctx context.Context, runID string) (Run, error)
	RunReview(ctx context.Context, runID string) (Run, error)
}

type VerificationService interface {
	RunVerification(ctx context.Context, runID string) ([]VerificationResult, error)
	DiagnoseFailure(ctx context.Context, runID string) (FailureDiagnosis, error)
}

type DecisionService interface {
	EvaluateGate(ctx context.Context, runID string) ([]DecisionGateResult, error)
	CreateDecisionReport(ctx context.Context, taskID string) (Decision, error)
	ApproveDecision(ctx context.Context, decisionID string, selectedOption string) (Decision, error)
	CreateHumanApproval(ctx context.Context, taskID string, input HumanApprovalInput) (HumanApproval, error)
	ResolveHumanApproval(ctx context.Context, approvalID string, input HumanApprovalResolution) (HumanApproval, error)
}

type MergeService interface {
	ApproveTaskForMerge(ctx context.Context, taskID string) error
	QueueTaskForMerge(ctx context.Context, taskID string) error
	RebaseBeforeMerge(ctx context.Context, taskID string) (Run, error)
	ReverifyBeforeMerge(ctx context.Context, taskID string) ([]VerificationResult, error)
	MergeTask(ctx context.Context, taskID string) error
}

type PatchApplicationService interface {
	ExportPatch(ctx context.Context, taskID string) (PatchApplication, error)
	MarkPatchApplied(ctx context.Context, taskID string, commit string) (PatchApplication, error)
	VerifyAppliedPatch(ctx context.Context, taskID string) (PatchApplication, error)
}

type EnvironmentService interface {
	SaveEnvironmentBinding(ctx context.Context, projectID string, input EnvironmentBindingInput) (EnvironmentBinding, error)
	RerunAfterEnvironmentUpdate(ctx context.Context, runID string) (Run, error)
}

type PlatformService interface {
	DetectEnvironments(ctx context.Context, projectID string) ([]ExecutionEnvironment, error)
	AddEnvironment(ctx context.Context, projectID string, input ExecutionEnvironmentInput) (ExecutionEnvironment, error)
	SetPrimaryEnvironment(ctx context.Context, projectID string, environmentID string) error
	GetRunProfile(ctx context.Context, projectID string, profileName string) (RunProfile, error)
}

type RunnerService interface {
	RunCommand(ctx context.Context, input RunCommandInput) (RunCommandResult, error)
	GetCapabilities(ctx context.Context, environmentID string) (RunnerCapabilities, error)
}

type ToolchainService interface {
	RunDoctor(ctx context.Context, projectID string, environmentID string) (ToolchainReport, error)
	ResolveRequirements(ctx context.Context, projectID string, targetPlatformID string) ([]ToolchainRequirement, error)
}

type PathMappingService interface {
	MapPath(ctx context.Context, projectID string, fromEnvironmentID string, toEnvironmentID string, path string) (string, error)
	ValidatePath(ctx context.Context, environmentID string, path string, purpose PathPurpose) error
}

type ChangeRequestService interface {
	CreateChangeRequest(ctx context.Context, projectID string, input string) (ChangeRequest, error)
	AnalyzeChangeImpact(ctx context.Context, changeRequestID string) (ChangeImpactReport, error)
	ApplyApprovedChange(ctx context.Context, changeRequestID string) error
}
```

CoreUseCasesという名前を使う場合は、上記serviceを束ねるAPI/CLI facade候補としてのみ扱います。実装で1つの巨大interfaceを作ると責務境界、テスト、mockが重くなるため禁止します。

## Agent Roles

Product / Planner / Consolidator / Review / Decision Agentは、直接DBやcanonical artifactを書き換える実体ではなく、Orchestratorが呼び出すadapterです。初期実装はFake adapterから始め、Real Codex / OpenAI API接続はschema、state machine、artifact ownershipが通った後に追加します。

```go
type ArtifactGenerationAgent interface {
	GeneratePRD(ctx context.Context, input PRDGenerationInput) (PRDDraft, error)
	GenerateArchitecture(ctx context.Context, input ArchitectureGenerationInput) (ArchitectureDraft, error)
	GenerateRoadmap(ctx context.Context, input RoadmapGenerationInput) (RoadmapDraft, error)
}

type PlanningAgent interface {
	GenerateFeatureDetail(ctx context.Context, input FeatureDetailInput) (PlanningArtifactDraft, error)
	GenerateImpactAnalysis(ctx context.Context, input ImpactAnalysisInput) (PlanningArtifactDraft, error)
	GenerateDecisionDraft(ctx context.Context, input DecisionDraftInput) (DecisionReportDraft, error)
	GenerateTaskGroupProposal(ctx context.Context, input TaskGroupProposalInput) (PlanningArtifactDraft, error)
	GenerateRiskReport(ctx context.Context, input RiskReportInput) (PlanningArtifactDraft, error)
}

type ConsolidationAgent interface {
	ConsolidatePlanningArtifacts(ctx context.Context, input ConsolidationAgentInput) (ConsolidationDraft, error)
}

type ReviewAgent interface {
	ReviewDiff(ctx context.Context, input ReviewInput) (ReviewOutput, error)
	GenerateSemanticBehaviorDiff(ctx context.Context, input SemanticDiffInput) (SemanticDiffOutput, error)
}
```

実行方針:

- `devos spec` / `devos plan` は `ArtifactGenerationAgent` を使う。
- `devos plan start` は `PlanningAgent` をbounded parallelで使う。
- `devos plan consolidate` は `ConsolidationAgent` の出力をOrchestrator serviceが検証してから、必要に応じてSerial Canonical Commitへ渡す。
- `devos review` は `ReviewAgent` を使うが、最終判定はPolicy EngineとOrchestrator state machineが行う。
- planning worker、review agent、coding agentはcanonical artifact、task、roadmap、merge queueを直接更新しない。
- agent出力はすべてJSON SchemaとGo validationを通し、source of truthへ反映するのはOrchestrator serviceだけとする。

## Request Queue and Worker

Orchestratorは、単一taskを手動実行するだけでなく、Feature RequestとWork Queueを通じて複数要望を非同期に処理できます。

初期実装ではlaneごとにconcurrencyを分けます。

| Lane | Concurrency | Writes |
| --- | --- | --- |
| planning | bounded parallel, default 3 | planning runs、planning artifacts、decision report drafts |
| consolidation | sequential | change requests、task group proposals、inbox items |
| execution | sequential | runs、verification、gate results |
| merge | sequential | merge queue、rebase/reverify/merge runs |

planning laneはread-only snapshotからproposal / report / draftを作ります。canonical artifact、task、roadmapへの反映はPlanning ConsolidatorとCanonical Commit Coordinatorがsingle writerで行います。

implementationとmerge queueの並列実行はmerge conflict、reverify、shared file riskが増えるためLater扱いにします。

## Verification / Gate

```text
Rule Engine
  -> 確実に検出できるものを判定する

Evidence Collector
  -> diff、テスト、ログ、artifact、task、PRDとの対応を集める

LLM Reviewer
  -> 意味的なリスク、仕様充足、曖昧さを判定する

Policy Engine
  -> プロジェクト方針に照らして次アクションを決める

Human
  -> policyでは解決できない判断だけ行う
```

Decision GateをLLMだけに寄せると誤判断します。逆にルールだけに寄せると止まりすぎます。ルール、証拠収集、LLMレビュー、Policy Engineを分けます。

## Human Inbox

最初に作るUIはTask BoardではなくHuman Inboxです。

```text
Project: Personal Dev OS

Autonomy Status:
- Running: 2 tasks
- Auto-repairing: 1 task
- Waiting for human: 2 decisions
- Blocked: 0
- Last successful merge: 18 minutes ago

Needs Your Judgment:
1. DEC-014: Add production dependency for form validation
   Recommendation: Approve
   Risk: Medium
   Why you need to decide: production dependency policy
   [Review Report]
```

UIの目的はログを見せることではなく、判断に必要な証拠だけを圧縮して見せることです。生ログと生diffは下層の詳細として扱います。

## Security Boundaries

Codex writable root、run artifact root、canonical artifact、secret storage、platform credential boundaryは分離します。Windows Codex adapterとWSL Codex adapterはauth、sandbox、`CODEX_HOME` を暗黙共有しません。Cross-environment path accessはPathMappingServiceだけが許可します。

## Git / Worktree Strategy

タスクごとにworktreeを作り、差分を分離します。

```text
main repository
  -> .devagent-worktrees/
       TASK-001/
       TASK-002/
       TASK-003/
```

基本コマンド:

```bash
git worktree add .devagent-worktrees/TASK-003 -b devos/TASK-003 main
```

Git / worktree / patch foundationはReal Codex接続前に独立実装します。Fake adapterでも実際のworktree上に固定diffを反映し、後続のdiff保存、patch export、merge queue、reverifyを現実のGit操作で検証します。

Worktree作成はcanonical Git environmentでだけ行います。Windows-primaryではGit for Windows、WSL-primaryではLinux Gitをcanonical Git providerとします。Hybridでsidecar verificationを行う場合も、同じworktreeを複数environmentが同時にwriteしてはいけません。same_filesystem mappingではwrite ownerをprimary environmentに固定します。sidecarがwriteを必要とする場合は `isolated_worktree` または `mirrored_clone` を使います。

`.devagent-worktrees/` はdefault worktree rootです。Windows-primary / WSL-primary / Hybridでは、`execution_environment.worktree_root` またはRunProfileで環境ごとのworktree rootを定義できます。

初期基盤に含めるもの:

- project root検出
- git repository check
- dirty tree policy
- `.devagent-worktrees/` gitignore check
- `orchestrator-data/` repo外配置またはgitignore check
- worktree作成
- branch naming
- base commit保存
- head commit取得
- diff保存
- patch export
- rebase / merge dry-run
- conflict検出
- merge前reverification
- cleanup dry-run

Dirty tree policy:

- target branchにtracked dirty changesがある場合、通常runは開始しない。
- `.devagent/` のgenerated artifactは、DBに保存済みのcontent hashと一致する場合だけ許可する。
- untracked filesはcontext builderへ含めない。
- worktree作成時のbase commitはtarget branchのclean HEADを保存する。
- approved artifact生成後はartifact version hashをDBへ保存し、以後のrunではhash一致を確認する。

Canonical artifact protection:

- Coding Agentのwritable rootはtask worktreeの実装対象に限定する。
- `.devagent/prd.md`、`.devagent/architecture.md`、`.devagent/roadmap.yaml`、`.devagent/schemas/*`、`.devagent/policies/*`、`AGENTS.md` はCodexが書く成果物ではない。
- Trusted artifactsは、可能な限りOrchestratorがprompt内またはread-only context bundleとして渡す。
- diffにcanonical artifact、schema、policy、AGENTS.mdの変更が含まれる場合、Decision Gateで検出する。

## Merge Queue

並列taskが同じbase commitから始まる場合、レビュー時点で通っていた差分でもmerge時点では古いmainを前提にしている可能性があります。そのため、人間承認後に直接mergeせず、merge queueで最新mainへのrebaseまたはmergeと再検証を必須にします。

```text
ready_for_human_review
  -> approved_for_merge
  -> queued_for_merge
  -> rebasing
  -> reverifying
  -> merged
```

Before merge:

1. 対象branch/worktreeを最新mainへrebaseまたはmergeする。
2. conflictがあればauto repair、解決不能ならHuman Inboxへ `merge_conflict` を出す。
3. verification commandを再実行する。
4. Decision Gateを再評価する。
5. 問題がなければmainへmergeする。

merge前の検証回数は増えますが、レビュー時には通っていた差分がmerge後に壊れるリスクを避けるため、このコストは初期完成スコープに含めます。

## Manual Patch Apply

Orchestratorが対象repoのmainへ直接mergeできない環境では、承認済み差分をpatchとしてexportし、人間が適用したcommitをOrchestratorへ登録します。この経路も完了条件に含める場合は、merge queueと同じく再検証とGate再評価を必須にします。

```text
ready_for_human_review
  -> approved_for_merge
  -> patch_exported
  -> manually_applied
  -> reverifying
  -> applied
```

手動適用では、人間の「適用した」という申告だけをsource of truthにしません。`patch_applications` にexported patch hash、適用commit、検証結果、GateResultを保存し、`applied` はOrchestrator verificationがPASSした後だけ許可します。

## Worktree Garbage Collector

worktreeは長時間運用で必ず増えるため、Orchestratorはcleanup planを作れます。ただし削除は危険操作なので、defaultはdry-runです。未merge diff、保存されていないdiff artifact、untracked filesがあるworktreeは削除対象にしません。
