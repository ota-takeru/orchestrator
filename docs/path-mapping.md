# Path Mapping

## Goal

Path Mappingは、Windows path、WSL/Linux path、Orchestrator管理pathを安全に対応付けるための仕様です。場当たり的な文字列置換は禁止し、root単位のmappingとenvironmentごとのvalidatorを使います。

## Canonical Path Rules

- path validationは `execution_environment_id` ごとに行う。
- Windows path、WSL path、Linux pathを同じvalidatorで扱わない。
- cross-environment path変換はPathMappingServiceだけが行う。
- path columnsは `execution_environment_id` を持つか、RunProfileやExecutionEnvironmentから解決できなければならない。
- `projects.root_path` はbootstrap/default用に残してよいが、canonical rootは `execution_environments.project_root` を正とする。

## Mapping Modes

正規値:

```text
same_filesystem:
  Windows pathとWSL pathが同じ物理treeを指す。
  例: C:\dev\app <-> /mnt/c/dev/app
  初期標準では同時write禁止。

isolated_worktree:
  primaryとsidecarが別worktreeを持つ。
  patch、branch、commit、rsync-like copyのいずれかで同期する。
  初期推奨。

mirrored_clone:
  別cloneとして扱う。
  同期はGit commit/branch経由。

unsupported:
  path変換不可。runnerはread/write不可。
```

## Windows / WSL Examples

Windows-primary same filesystem:

```yaml
path_mapping:
  from_environment_id: windows-main
  to_environment_id: wsl-sidecar
  from_root: C:\dev\app
  to_root: /mnt/c/dev/app
  mapping_mode: same_filesystem
  write_owner_environment_id: windows-main
```

WSL-primary isolated worktree:

```yaml
path_mapping:
  from_environment_id: wsl-main
  to_environment_id: windows-sidecar
  from_root: /home/user/app
  to_root: C:\dev\app-sidecar
  mapping_mode: isolated_worktree
  write_owner_environment_id: wsl-main
```

## Case Sensitivity

Windows-primaryではcase-sensitive filename collisionをpreflightで検出します。例: `Readme.md` と `README.md` が同じtreeに存在する場合、Windows側で安全に扱えないためblockします。

preflight:

- case-sensitive filename collision check
- symlink support check
- `core.ignorecase` / `core.filemode` / `core.autocrlf` check
- `.gitattributes` existence / policy check

## Line Endings

line ending policyは `.gitattributes` で固定します。Windows / WSL両対応では、runnerのOS差で不要なdiffが出ることをGate対象にします。

推奨:

```gitattributes
* text=auto
*.sh text eol=lf
*.ps1 text eol=crlf
*.go text eol=lf
*.ts text eol=lf
*.tsx text eol=lf
```

project方針により詳細は変えてよいですが、policyが存在しない場合はplatform doctorで警告またはsetup cardを作ります。

## Same Worktree Write Policy

- same filesystem mappingでは同じ物理treeへの同時writeを禁止する。
- write ownerは1つのenvironmentに固定する。
- primary environment以外からcanonical worktreeへwriteする場合はDecision Gate対象にする。
- sidecarがwriteを必要とする場合は `isolated_worktree` または `mirrored_clone` を使う。
- canonical Git providerとcanonical merge providerはRunProfileで1つに固定する。

## PathMappingService

場当たり的な文字列置換は禁止します。

```go
type PathMappingService interface {
	ToEnvironmentPath(ctx context.Context, projectID string, fromEnvID string, toEnvID string, path string) (string, error)
	ValidatePathInEnvironment(ctx context.Context, envID string, path string, purpose PathPurpose) error
	ResolveCanonicalPath(ctx context.Context, projectID string, path string) (CanonicalPath, error)
}
```

`ResolveCanonicalPath` は、入力pathがどのenvironmentのallowed root配下にあるかを解決し、ambiguousな場合はerrorにします。

## Validation Rules

- path mappingはroot単位で登録する。
- partial string replaceは禁止。
- mapping後のpathは対象environmentのallowed root配下でなければならない。
- symlink escapeは禁止。
- Windows pathではdrive letter、UNC、reserved name、NUL、control characterを検査する。
- WSL/Linux pathではabsolute path、NUL、symlink escapeを検査する。
- same_filesystem mappingでは同時write ownerを1つに固定する。
- case-sensitive collisionをpreflightで検出する。
- `.gitattributes` でline endingsを固定する。
