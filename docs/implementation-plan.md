# Implementation Plan

実装順序は、Codex実行を早くつなぐことよりも、状態、証拠、判断レポート、platform contractを先に固めることを優先します。中核は「最初のタスクを実装する」ことではなく、「失敗、修復、判断待ち、変更要求まで含む最短の自走ループ」です。

Initial Complete Scope は品質境界であり、最初の実装タスクではありません。Coding Agent は Initial Complete Scope 全体を一括実装してはいけません。実装はImplementation Sliceごとに行い、各sliceは状態遷移、DB制約、証拠保存、テストを伴って完了します。

Real Codex接続前に、Windows-primary / WSL-primary / Hybrid fake runnerを通します。

## Slice 0: Canonical Docs and Authority

- `docs/index.md` のCanonical Implementation Docsだけを実装仕様として扱う
- `docs/archive/*` とobsolete/non-canonical docsをcontext builderから除外、またはuntrusted background reference扱いにする
- [state-machine.md](state-machine.md)、[storage-schema.md](storage-schema.md)、[state-invariants.md](state-invariants.md) を正規仕様として維持する
- OpenAI / Codex仕様に依存する記述は公式確認URLと確認日を残す

## Slice 0.25: Platform Model Docs

- `platform-model.md`
- `runner-protocol.md`
- `path-mapping.md`
- `toolchain-requirements.md`
- `index.md` canonical docs更新
- Windows-primary / WSL-primary / Hybrid の定義
- primary_environment rule
- canonical_operations rule

## Slice 0.5: Project Trust / Platform-aware Preflight

- project root検出
- primary_environment selection
- execution_environment registry validation
- path mapping validation
- `.gitattributes` existence / policy check
- `core.autocrlf` / `core.filemode` check
- case-sensitive filename collision check
- symlink support check
- dirty tree policy
- `.env.local` gitignore check
- `.devagent-worktrees/` gitignore check
- `orchestrator-data/` がrepo外、またはgitignore済みであること
- approved artifacts / `.devagent/schemas/` がCodex writable rootに入らないこと
- fake bootstrapではtarget app toolchainを要求しない
- real Codex adapterだけCodex CLI / auth / output-schemaをhard preflightにする

## Slice 1: Core Storage, Platform Tables, State Machines

Contract Fix Sliceをここで完了します。

- SQLite migration system
- `projects`
- `execution_environments`
- `project_run_profiles`
- `path_mappings`
- `target_platforms`
- `toolchain_requirements`
- `artifacts` / `artifact_versions` with `approved_version_id` and `latest_version_id`
- `tasks`
- `runs` with one verification run supporting multiple environment results
- `run_artifacts` with `command_event_id`, `artifact_key`, and multiple command stdout/stderr artifacts
- `command_events`
- `verification_results.environment_id`
- `gate_results`
- `human_approvals`
- `inbox_items`
- `environment_requirements` / `environment_bindings` scoped by environment
- `environment_audit_events` with environment / binding / requirement / run / command event references
- `merge_queue_entries`
- `patch_applications`
- `trace_links`
- platform-related CHECK / FK / UNIQUE / index
- CHECK Matrix and Go enum validation
- state transition service for Task, Run, CommandEvent, MergeQueue, PatchApplication, Platform entities
- pending timestamp validation for runs and command_events
- JSON ID array validation for RunProfile required/optional environment ids

## Slice 1.5: Schema Registry and Validation

- Orchestrator-owned JSON Schema registry
- `.devagent/schemas/` へのhash付きcopy
- schema checksum保存
- Go struct validation
- JSON Schema validation
- schema version mismatch検出
- Codex output schemaとOrchestrator artifact schemaの分離

## Slice 2: Artifact Lifecycle + Approval

- `.devagent/` artifact生成
- artifact versioning
- `approved_version_id` をtrusted contextとして使う
- `latest_version_id` をdraft/proposedを含む作業versionとして使う
- artifact approval status
- `devos init` / `devos spec` / `devos plan` / `devos tasks materialize` の責務分離
- approved artifactだけからTask YAMLとcanonical taskをready化
- proposed versionがcurrent approved contextを置き換えないことの検証

## Slice 2.25: Runner and Platform Foundation

- Runner interface
- fake platform runner
- fake Windows runner
- fake WSL runner
- WindowsLocalRunner skeleton
- WSLLocalRunner skeleton
- RunnerCapabilities
- RunCommandRequest / RunCommandResult
- command_events保存
- PathMappingService
- platform doctor skeleton

## Slice 2.5: Environment-aware Git / Worktree / Patch Foundation

- canonical Git environment resolver
- Git for Windows provider skeleton
- Linux Git provider skeleton
- worktree path validation per environment
- same_filesystem write owner enforcement
- isolated_worktree support skeleton
- project root検出
- git repository check
- dirty tree policy
- worktree作成
- branch naming
- base/head commit保存
- diff保存
- patch export
- rebase / merge dry-run
- conflict検出
- cleanup dry-run plan

## Slice 3: Fake Run Workflow with Fake Platform Runners

- Fake Coding Agent Adapter
- Windows-primary fake workflow
- WSL-primary fake workflow
- Hybrid fake workflow
- 1つのverification runに複数environmentのcommand_events / verification_resultsを保存
- fixed diff生成
- fixed JSONL保存
- run artifact保存
- invalid state transition rejection

## Slice 4: Environment-aware Verification / Baseline / Gate

- environment-aware verification_commands
- required_for_merge flag
- Windows PowerShell verification runner
- WSL bash verification runner
- optional verification result handling
- baseline verification
- current diff / baseline / environment / spec_gap / unknown classification
- optional sidecar failureはデフォルトREPORT_ONLY
- required_for_merge failureはmerge block
- platform/toolchain failure classification
- GateResult保存

## Slice 5: Human Inbox + Approval Sources + Toolchain Setup

- `human_approvals`
- `inbox_items` projection
- `devos inbox`
- `devos decisions`
- `devos approve`
- `devos review approve`
- `devos review reject`
- `devos merge approve`
- final review approval
- merge approval
- Environment Input Card
- Platform Setup Card
- Toolchain Setup Card
- Path Mapping Issue Card
- Runner Capability Issue Card
- missing toolchainをEnvironment Inputへ混ぜない

## Slice 6: Merge Queue + Reverify

- explicit MergeQueueStatus state machine
- `approved_for_merge`
- `queued_for_merge`
- fakeまたはdry-run merge path
- 最新mainへのrebaseまたはmerge
- merge前reverification
- Decision Gate再評価
- merge conflict handling
- `devos patch export`
- `devos patch status`
- `devos patch mark-applied`
- `devos patch verify-applied`
- PatchApplication `needs_decision` から復帰する遷移
- `patch_exported -> manually_applied -> reverifying -> applied`

## Slice 7: Real Codex Windows / WSL Execution

- Codex Windows adapter
- Codex WSL adapter
- WSL2 preflight
- environment-specific `CODEX_HOME`
- Windows path / WSL path support
- sandbox_profile stored in run record
- prompt.mdをimmutable artifactとして保存
- `codex exec -` へprompt.mdの内容をstdinで渡す
- Windows Codex authとWSL Codex authを共有しない

## Slice 8+: Auto Repair, Semantic Diff, Change Request, Planning Queue, UI

- auto repair
- semantic behavior diff
- dependency risk ledger
- policy memory
- Change Request flow
- Request Queue
- bounded parallel planning lane
- Planning Consolidation
- Serial Canonical Commit
- Sequential Execution Worker
- Rolling Planning Checkpoint
- Human Inbox UI
- Full Dashboard

## Acceptance Test Names

実装時は少なくとも次のE2E / integration testを用意します。slice内の単体テストに加えて、これらの名前を完了条件の目印にします。

- `TestBootstrapFakeTaskMerges`
- `TestStorageCheckValuesCoverAllStateMachines`
- `TestChangeRequestsTableExistsBeforeChangeFlow`
- `TestDraftArtifactsDoNotMakeTaskReady`
- `TestApprovedArtifactsMakeTaskReady`
- `TestArtifactApprovedWithNotesStoresApprovalNotes`
- `TestRejectedArtifactCannotMaterializeReadyTask`
- `TestInvalidTransitionRejectedWithWorkflowEvent`
- `TestVerificationCurrentDiffTriggersAutoRepair`
- `TestBaselineFailureDoesNotFailCurrentTask`
- `TestBaselineFailureAllowsMergeOnlyWhenNoRegressionAndReported`
- `TestUnclassifiedVerificationFailureBlocksMerge`
- `TestBaselineFailureWorsenedByDiffBlocksMerge`
- `TestHumanDecisionBlocksMerge`
- `TestFinalReviewApprovalQueuesMerge`
- `TestMergeQueueReverifiesBeforeMerge`
- `TestHardBlockCannotBeApprovedThrough`
- `TestInboxIsProjectionNotSourceOfTruth`
- `TestFakeBootstrapDoesNotRequireCodexAuth`
- `TestCodexAdapterRequiresCodexAuthPreflight`
- `TestWorkerLeaseExpiredItemIsRecovered`
- `TestLeasedWorkQueueItemCanRecoverFromExpiredLease`
- `TestWorkQueueAttemptCountDoesNotDoubleIncrement`
- `TestWorkQueueIdempotencyPreventsDuplicateOpenItems`
- `TestPlanningWorkerCannotWriteCanonicalArtifacts`
- `TestStalePlanningSnapshotCannotCommit`
- `TestChangeRequestUsesTraceLinks`
- `TestCanonicalArtifactsCannotBeModifiedByCodingAgent`
- `TestSchemaFilesCannotBeModifiedByCodingAgent`
- `TestWorktreesDirectoryIsGitignoredBeforeRun`
- `TestRunArtifactHashChangesInvalidateHumanApproval`
- `TestSecretNeverPersistedToSQLiteOrArtifacts`
- `TestEnvSetUsesInteractiveOrStdinSecretInputOnly`
- `TestManualPatchExportAndVerifyIfManualApplyIsInScope`
- `TestProjectRequiresExactlyOnePrimaryEnvironment`
- `TestWindowsPrimaryFakeBootstrapPasses`
- `TestWSLPrimaryFakeBootstrapPasses`
- `TestHybridOptionalVerificationRecordsEnvironment`
- `TestVerificationCommandRequiresKnownEnvironment`
- `TestRunStoresImplementationEnvironmentID`
- `TestVerificationResultStoresEnvironmentID`
- `TestCommandEventStoresRunnerAndEnvironment`
- `TestPathMappingRejectsPathOutsideAllowedRoot`
- `TestSameFilesystemMappingRejectsConcurrentWriteOwners`
- `TestCaseSensitiveFilenameCollisionBlocksWindowsPrimary`
- `TestGitProviderMatchesCanonicalEnvironment`
- `TestWindowsPrimaryUsesGitForWindows`
- `TestWSLPrimaryUsesLinuxGit`
- `TestPlatformDoctorCreatesToolchainSetupCard`
- `TestMissingRequiredToolchainBlocksRequiredVerification`
- `TestMissingOptionalToolchainDoesNotBlockMergeByDefault`
- `TestWindowsCodexAdapterDoesNotUseWSLCodexHome`
- `TestWSLCodexAdapterRejectsWSL1`
- `TestWindowsProtectedPathsAreExpandedAndBlocked`
- `TestWSLProtectedPathsAreExpandedAndBlocked`
- `TestRunnerBridgeCannotBypassPathMappingService`
- `TestOptionalSidecarVerificationFailureIsReportOnlyByDefault`
- `TestRequiredSidecarVerificationFailureBlocksMerge`
- `TestRunProfileCanonicalMergeEnvironmentIsSingleWriter`
- `TestHybridVerificationCanRecordMultipleEnvironmentResults`
- `TestRunArtifactAllowsMultipleCommandStdoutStderr`
- `TestPendingRunDoesNotRequireStartedAt`
- `TestPendingCommandEventDoesNotRequireStartedAt`
- `TestEnvironmentRequirementsAreScopedPerEnvironment`
- `TestEnvironmentBindingScopeAllowsRun`
- `TestPatchApplicationNeedsDecisionCanResumeAfterDecision`
- `TestMergeQueueStatusTransitionsAreExplicit`
- `TestArtifactProposedVersionDoesNotReplaceCurrentApprovedContext`
- `TestCodexPromptStdinUsesPromptContentNotPath`
- `TestAgentsMdContainsPlatformSafetyRules`
- `TestRunProfileEnvironmentIdsAreValidated`
- `TestSameFilesystemPathMappingRequiresWriteOwner`
- `TestSidecarVerificationDoesNotBlockMergeUnlessRequired`
- `TestWindowsAndWSLCannotWriteSameWorktreeConcurrently`

## Later

- remote Windows runner
- remote Linux runner
- automatic toolchain installation
- code signing automation
- concurrent implementation across environments
- Codex SDK移行条件の再評価
