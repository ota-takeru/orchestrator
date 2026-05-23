# Implementation Start Contract

この文書は、最初の実装sliceで守る短い契約です。Initial Complete Scopeは最終的な初期完成品質境界であり、最初の実装タスクではありません。

## First Target

Fake Coding Agent Adapterで1タスクの縦断workflowを通します。

```text
devos init
  -> devos spec
  -> devos plan
  -> artifact approve
  -> devos tasks materialize
  -> fake implementation run
  -> verification
  -> gate result
  -> inbox projection
  -> human final review approval
  -> merge approval
  -> merge queue
  -> reverify
  -> merged
```

`devos init` 単体はproject record、`.devagent/concept.md`、policy skeleton、schema registry copy、preflight reportだけを作ります。Bootstrap用の縦断commandは `devos bootstrap --adapter fake` とし、initにartifact生成、承認、ready化、mergeまで背負わせません。

## Must Implement First

- SQLite migration 001 core
- SQLite migration 002 mergeの最小部分
- execution_environments
- project_run_profiles
- path_mappings
- target_platforms
- toolchain_requirements
- command_events
- environment-aware verification_commands
- Go enumとstate machine
- repository tests
- fake Windows runner
- fake WSL runner
- fake hybrid verification flow
- Git / worktree / patch foundation
- artifact draft生成とapproval
- approved artifactによるtask materialize / ready化制御
- fake implementation run
- run artifact保存
- Orchestrator-owned verification runner
- gate result保存
- Human Inbox projectionと `human_approvals`
- `devos inbox`
- `devos approve`
- `devos review approve`
- `devos merge approve`
- merge queueのfakeまたはdry-run path
- merge前reverification

Real Codex前にFake runnerで通すworkflow:

- Windows-primary fake workflow
- WSL-primary fake workflow
- Hybrid fake workflow

Hybrid fake workflowでは、1つのverification runに複数environmentの `command_events` / `verification_results` を保存できることを確認します。

## Must Not Implement Yet

- real Codex execution
- Web UI
- parallel planningの本実装
- Change Request flow
- Environment Input UI
- Policy / Preference Editor
- policy memoryの期限・失効条件処理
- dependency risk ledgerの詳細処理
- semantic behavior diffの高度なline range confidence
- Full Dashboard

## Completion Rule

このcontractの完了は、1つのタスクがFake adapterで `merged` まで到達し、DB制約、状態遷移、run artifact、verification result、gate result、inbox item、merge前reverificationがテストで確認された状態です。

Real Codex Adapterは、このcontractが通った後に接続します。
