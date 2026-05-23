# Platform Model

## Goal

Windows / WSL / Hybrid 対応は、製品を分けるためのものではありません。採用する方針は次です。

```text
One product
One core workflow
One DB schema
One CLI UX
Multiple platform adapters / runners
```

Core workflow、状態機械、DB、Human Inbox、Decision Gate、artifact lifecycle は共通にします。OS差はPlatform Adapterへ閉じ込め、projectごとに1つのprimary environmentをsource of truthとして固定します。

## Core and Platform Adapter Split

Core owns:

- project model
- artifact lifecycle
- task / run state machine
- verification result model
- decision gate
- human inbox
- human approvals
- merge queue
- manual apply
- trace links
- planning queue
- policy / memory / dependency ledger
- evidence store

Platform adapters own:

- shell execution
- path format
- Git provider
- worktree creation
- Codex process invocation
- verification command execution
- toolchain detection
- sandbox profile
- protected path expansion

CoreはOS固有のpath、shell、Git実装、sandbox差を直接扱いません。Coreが扱うのは `execution_environment_id`、RunProfile、正規化済みresult、証拠artifactです。

## Execution Environments

Execution Environmentは、Orchestratorがcommand、Git、Codex、verificationを実行できる具体的な環境です。

```yaml
execution_environment:
  id: windows-main
  os_family: windows
  role: primary
  shell: powershell
  project_root: C:\dev\my-app
  git_provider: git-for-windows
  codex_adapter: codex-windows
  sandbox_profile: windows-native
  status: configured
```

```yaml
execution_environment:
  id: wsl-main
  os_family: wsl
  role: primary
  shell: bash
  project_root: /home/user/my-app
  git_provider: linux-git
  codex_adapter: codex-wsl
  sandbox_profile: linux-bubblewrap
  status: configured
```

正規値:

```text
os_family:
  windows | wsl | linux | macos | remote_windows | remote_linux

role:
  primary | sidecar | remote | disabled

shell:
  powershell | cmd | bash | sh | none

git_provider:
  git-for-windows | linux-git | none

codex_adapter:
  codex-windows | codex-wsl | codex-linux | none

sandbox_profile:
  windows-native | linux-bubblewrap | external-isolated | none
```

## Project Modes

正規モード:

```text
windows-primary:
  devos host: Windows
  canonical repo root: Windows filesystem
  canonical Git: Git for Windows
  canonical verification: Windows runner
  Codex: Windows native
  WSL: optional sidecar

wsl-primary:
  devos host: WSL/Linux
  canonical repo root: WSL/Linux filesystem
  canonical Git: Linux Git
  canonical verification: WSL/Linux runner
  Codex: WSL/Linux
  Windows: optional sidecar

hybrid:
  one primary environment
  one or more sidecar environments
  canonical Git/merge/artifact write remain assigned to one environment
  secondary environments may run optional or required verification
```

`single_environment` はplatform未分化の旧概念ではなく、primary environmentだけを持つ単一環境profileとして扱います。

DB上の `project_run_profiles.mode` は `single_environment`、`windows_primary`、`wsl_primary`、`hybrid` のいずれかです。表示名では `windows-primary` / `wsl-primary` を使ってよいですが、保存値はunderscore表記に統一します。

## Primary Environment Rule

必須ルール:

- projectは `primary_environment` を必ず1つだけ持つ。
- `primary_environment` なしのprojectでrunを開始してはいけない。
- canonical Git operationを複数環境へ分散してはいけない。
- canonical mergeを複数環境へ分散してはいけない。
- canonical artifact writeはOrchestrator Coreだけが行う。
- WindowsとWSLが同じworktreeを同時にwriteしてはいけない。

例:

```yaml
project:
  primary_environment: windows-main
  optional_environments:
    - wsl-sidecar
```

```yaml
project:
  primary_environment: wsl-main
  optional_environments:
    - windows-sidecar
```

Hybrid projectでも、canonical Git / canonical merge / canonical artifact write のsource of truthは1環境に固定します。

## Canonical Operations

project profileはcanonical operationの割当を持ちます。

```yaml
canonical_operations:
  git_status: windows-main
  worktree_create: windows-main
  implementation: windows-main
  verification:
    required:
      - windows-main
    optional:
      - wsl-sidecar
  merge: windows-main
  artifact_write: core
```

WSL-primary例:

```yaml
canonical_operations:
  git_status: wsl-main
  worktree_create: wsl-main
  implementation: wsl-main
  verification:
    required:
      - wsl-main
    optional: []
  merge: wsl-main
  artifact_write: core
```

`artifact_write: core` は、run log、verification result、gate result、Decision Report、summaryなどの証拠をCoding Agentやrunnerが直接編集しないことを意味します。

## Target Platforms

Target Platformは、作るアプリや検証対象のplatformです。Execution Environmentとは別概念です。

例:

```yaml
target_platform:
  id: windows-desktop
  os_family: windows
  app_type: desktop
  framework: winui
  packaging: msix
  required_environment_id: windows-main
  canonical_verification_environment_id: windows-main
```

初期完成スコープでは、Target Platformはbuild/test/optional package artifact captureの要件解決に使います。code signing、Store submission、installer実行はLaterです。

## Hybrid Rules

- Hybridはprimary environmentを複数持つことではない。
- implementation environment、canonical Git environment、canonical merge environmentはRunProfileで1つに解決される。
- sidecar environmentはoptionalまたはrequired verificationを実行できる。
- sidecarがcanonical worktreeへwriteする場合はDecision Gate対象にする。
- same filesystem mappingで同じ物理treeを共有する場合、write ownerは1つに固定する。
- sidecarがwriteを必要とする場合は `isolated_worktree` または `mirrored_clone` を使う。

## Repository Location Policy

Windows-primary project:

```text
repoはWindows filesystemを推奨する。
例: C:\dev\project
WSLから触る場合は /mnt/c/dev/project として扱う。
```

WSL-primary project:

```text
repoはWSL/Linux filesystemを推奨する。
例: /home/user/project
Windowsから直接canonical operationを行わない。
```

Hybrid project:

```text
same_filesystem mappingを使う場合はread/write ownerを明示する。
初期標準では isolated_worktree を推奨する。
```

`.devagent-worktrees/` はdefault worktree rootであり、固定仕様ではありません。environmentごとに `execution_environment.worktree_root` またはRunProfileでworktree rootを定義できます。

## Distribution Model

初期完成スコープのWindows対応は、build/testとoptional package artifact captureまでを対象にします。

Initial:

- Windows build
- Windows test
- optional package artifact capture without signing

Later:

- code signing automation
- certificate import/export
- Store submission
- installer execution

Packaging / signing runnerはPlatform Adapter境界として設計しますが、code signing key操作やinstaller実行は通常runでは自動化しません。

## Initial Scope

初期完成スコープに含めます。

- platform model schema
- Windows local runner
- WSL/Linux local runner
- primary_environment selection
- path mapping validation
- environment-aware verification_commands
- platform doctor
- toolchain setup Human Inbox card
- Windows-primary run
- WSL-primary run
- Hybrid verification with explicit sidecar mapping

## Later

- remote runners
- concurrent implementation across environments
- automatic toolchain installation
- administrator elevation automation
- production deployment
- code signing automation without human approval
