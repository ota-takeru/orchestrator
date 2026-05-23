# Runner Protocol

## Goal

Runner Protocolは、Windows runner、WSL/Linux runner、将来のremote runnerをCoreから同じ形で呼び出すための仕様です。Coreはcommandの意味、証拠、状態遷移を管理し、OS固有のshell、path、sandbox、process起動はrunnerに委譲します。

## Runner Interface

Go interface例:

```go
type Runner interface {
	EnvironmentID() string
	Capabilities(ctx context.Context) (RunnerCapabilities, error)
	Preflight(ctx context.Context) (PreflightReport, error)
	RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error)
	CollectArtifacts(ctx context.Context, req ArtifactCollectionRequest) ([]RunArtifact, error)
}
```

Runnerはstdout/stderrをDBへ直接保存しません。Orchestrator-owned artifactとして保存し、`command_events` と `run_artifacts` で参照します。

## Capabilities

RunnerCapabilitiesは少なくとも次を持ちます。

```yaml
runner_capabilities:
  environment_id: windows-main
  shells:
    - powershell
    - cmd
  direct_exec: true
  shell_exec: true
  supports_timeout: true
  supports_process_group_cancel: true
  supports_redaction: true
  supports_network_policy: true
  path_style: windows
  sandbox_profiles:
    - windows-native
  git_providers:
    - git-for-windows
```

Capabilities不足は通常のHuman Inputではありません。Runner Capability Issue CardまたはEnvironment Issue Reportとして扱います。

## Command Request

Windows例:

```json
{
  "environment_id": "windows-main",
  "runner": "powershell",
  "cwd": "C:\\dev\\my-app\\.devagent-worktrees\\TASK-001",
  "argv": ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", ".\\.devagent\\scripts\\verify-windows.ps1"],
  "timeout_seconds": 600,
  "network_policy": "off",
  "env_binding_ids": ["ENVBIND-001"],
  "capture_stdout": true,
  "capture_stderr": true,
  "redaction_required": true
}
```

WSL/Linux例:

```json
{
  "environment_id": "wsl-main",
  "runner": "bash",
  "cwd": "/home/user/project/.devagent-worktrees/TASK-001",
  "argv": ["bash", "-lc", "./.devagent/scripts/verify-linux.sh"],
  "timeout_seconds": 600,
  "network_policy": "off",
  "env_binding_ids": [],
  "capture_stdout": true,
  "capture_stderr": true,
  "redaction_required": true
}
```

`runner` は `auto` を受けてもよいですが、実行前にenvironmentのdefault shellへ解決してから保存します。`cwd` は対象environmentのpath validatorを通過している必要があります。

## Command Result

```json
{
  "environment_id": "windows-main",
  "exit_code": 0,
  "status": "succeeded",
  "started_at": "2026-05-23T00:00:00Z",
  "completed_at": "2026-05-23T00:02:00Z",
  "stdout_artifact_id": "RUNART-001",
  "stderr_artifact_id": "RUNART-002",
  "command_event_ids": ["CMDEVT-001"],
  "detected_failures": []
}
```

`status` は `pending | running | succeeded | failed | timed_out | blocked | cancelled` のいずれかです。command eventは再開せず、人間判断後は新しいcommand eventを作ります。

## Command Events

command eventはGateでqueryできる必要があるため、初期実装ではtableとして保存します。`run_artifacts(artifact_type='command_events')` だけに保存する代替案は、Gate queryが難しくなるため標準にしません。

```sql
CREATE TABLE command_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  command_kind TEXT NOT NULL,
  runner TEXT NOT NULL,
  cwd TEXT NOT NULL,
  argv_json TEXT NOT NULL,
  shell_invocation INTEGER NOT NULL,
  network_policy TEXT NOT NULL,
  exit_code INTEGER,
  status TEXT NOT NULL,
  stdout_artifact_id TEXT,
  stderr_artifact_id TEXT,
  detected_risks_json TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

保存する最小証拠:

- `environment_id`
- `runner`
- `cwd`
- `argv_json`
- shell経由かdirect execか
- `network_policy`
- `exit_code`
- stdout/stderr artifact
- detected risks

`pending` command eventでは `started_at` はNULLです。record作成時刻は `created_at` に保存し、`running` で `started_at` を設定し、terminal statusで `completed_at` を設定します。状態やartifact参照を更新するたびに `updated_at` を更新します。

Command stdout/stderrは `run_artifacts` に複数保存できます。`run_artifacts` は `command_event_id` と `artifact_key` を持ち、uniqueは `UNIQUE(run_id, artifact_type, artifact_key)` を標準にします。command outputのartifact_typeは `command_stdout`、`command_stderr`、`command_result` です。

## Environment-Aware Verification

Task YAMLのverification commandはenvironment-awareな構造体にします。文字列配列は後方互換として読み込んでもよいですが、正規schemaではありません。

```yaml
verification_commands:
  - id: go-test
    environment: primary
    runner: auto
    required_for_merge: true
    working_dir: task_worktree
    command:
      argv: ["go", "test", "./..."]
    timeout: 10m
    network: false
    required_toolchains:
      - go
```

`environment: primary` はRunProfileでprimary environmentへ解決します。sidecar verificationは `required_for_merge` によりmerge block対象かreport onlyかを明示します。

## Runner Safety

- Running `powershell.exe` from WSL or `wsl.exe` from Windows is a bridge operation and must be represented by a runner/environment, not an ad-hoc shell string.
- command requestのpathは対象environmentのvalidatorで検査する。
- cross-environment path変換はPathMappingServiceだけが行う。
- protected path expansionはenvironment-specificに行う。
- network policyはcommand eventへ必ず保存する。
- dangerous command検出はargv正規化、shell分類、protected path、network lane、worktree外writeを組み合わせる。
- admin elevation、installer、registry、certificate、firewall/Defender変更は通常runnerでは実行しない。

## Failure Classification

Runner由来の失敗分類:

| Failure | Handling |
| --- | --- |
| unknown environment | `platform_mismatch_detector` |
| path mapping missing | `path_mapping_detector` |
| cwd outside allowed root | `path_mapping_detector` |
| required toolchain missing | `toolchain_detector` |
| runner capability missing | Runner Capability Issue Card |
| command timeout | verification failureまたはenvironment failure |
| protected path access | `HARD_BLOCK` |
| unauthorized cross-environment write | `HARD_BLOCK` |

required verificationのtoolchain missingはmerge blockです。optional verificationのtoolchain missingはデフォルトでREPORT_ONLYに分類し、Gate policyで必要に応じて昇格します。
