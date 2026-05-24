# Toolchain Requirements

## Goal

Toolchain Requirementsは、Git、PowerShell、dotnet、MSBuild、Windows SDK、bash、bubblewrap、Codex CLI / Codex authなど、実行環境で必要なローカルツールを検出し、人間のセットアップ作業へつなぐための仕様です。

## Difference from Environment Variables

Environment Variables:

```text
secretやruntime設定。人間が値を入力する。
```

Toolchain Requirements:

```text
Git、PowerShell、dotnet、MSBuild、Windows SDK、bash、bubblewrap、Codex CLI / Codex authなど。
値入力ではなく、人間のセットアップやpreflight確認が必要。
```

Environment Input CardとToolchain Setup Cardを混ぜてはいけません。Visual Studio、MSBuild、Windows SDK、bubblewrap、GitなどはEnvironment InputではなくToolchainRequirementとして扱います。

## ToolchainRequirement Status

正規値:

```text
detected
missing
invalid
setup_required
waived
unsupported
revoked
```

`waived` はproject policyまたは明示的な人間判断でのみ設定できます。required verificationに必要なtoolchainをwaiveする場合はGate policyでmerge可否を明示します。

## ToolchainRequirement Required For

`toolchain_requirements.required_for` の正規値:

```text
implementation
verification
runtime
runtime_smoke
deployment
```

同じtoolchainでも、implementationに必要なものとoptional runtime smokeにだけ必要なものは別requirementとして記録できます。Gateでは `required_for` とverification commandの `required_for_merge` を組み合わせてmerge blockかreport-onlyかを決めます。

## Platform Doctor

Platform Doctorはexecution environmentごとにtoolchain、path、Git、sandbox、Codex adapter、case collision、line ending policyを検査します。

検査例:

- runner shellが起動できる
- Git providerが期待通り
- Codex adapterが利用可能
- Codex CLIがPATH上にある
- environment-specific `CODEX_HOME` のauthが存在する
- WSL adapterではWSL2である
- sandbox profileが利用可能
- required toolchain versionが満たされる
- `.gitattributes` が存在しline ending policyを持つ
- `core.autocrlf` / `core.filemode` がproject方針と矛盾しない
- case-sensitive filename collisionがない
- symlink supportが想定通り

missing/setup_requiredはHuman InboxのToolchain Setup CardまたはPlatform Setup Cardへ投影します。

## Windows Toolchains

初期候補:

- PowerShell
- Git for Windows
- Codex CLI Windows native
- Codex auth in Windows `CODEX_HOME`
- dotnet SDK
- MSBuild / Visual Studio Build Tools
- Windows SDK
- Windows App SDK / WinUI tooling if target requires it
- Developer Mode if target requires it

Windows-specific setupでadmin elevation、Visual Studio workload install、Developer Mode変更が必要な場合、通常runでは自動実行しません。

## WSL / Linux Toolchains

初期候補:

- WSL2
- bash
- Linux git
- Codex CLI in WSL
- Codex auth in WSL/Linux `CODEX_HOME`
- bubblewrap for Linux sandbox
- language-specific tools: Go, Node, Python, Rust, etc.

WSLでCodexを使う場合はWSL2を前提にします。WSL1を検出した場合、Codex WSL adapterをreadyにしてはいけません。

## Codex Auth Requirement

Codex CLI本体とCodex authは別requirementです。

- `codex`: `codex` executableがPATH上にあるかを検出する。
- `codex-auth`: environment-specific `CODEX_HOME` にauthが存在するかを検出する。

`codex-auth` はauthファイルの中身を読みません。存在確認だけを行い、証跡には `CODEX_HOME` sourceと検出状態だけを保存します。

default `CODEX_HOME`:

- Windows: `CODEX_HOME` が明示されていればそれを使う。なければ `%USERPROFILE%\.codex`、次に `%HOMEDRIVE%%HOMEPATH%\.codex` を候補にする。
- WSL / Linux: `CODEX_HOME` が明示されていればそれを使う。なければ `$HOME/.codex` を候補にする。

Windows Codex authとWSL Codex authは共有しません。片方で認証済みでも、もう片方の `codex-auth` は未検出なら `setup_required` としてToolchain Setup Cardへ投影します。

## Human Inbox Toolchain Setup Card

例:

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

Toolchain missingはHuman Inputではありません。値入力ではなく、外部セットアップまたは明示的waiveの判断です。

## Prohibited Automatic Actions

初期実装では自動実行しません。

- admin elevation
- Visual Studio workload install
- Developer Mode変更
- registry変更
- firewall / Defender設定変更
- certificate import
- code signing key操作
- installer実行
- winget install / apt install の自動実行

これらはHuman InboxのToolchain Setup CardまたはDecision Reportへ出します。実行が必要な場合も、明示的な手動手順または隔離されたmanual actionとして扱います。
