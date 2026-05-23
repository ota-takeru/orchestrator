# Tech Stack

## Decision

このプロジェクトは、バックエンドをGo、UIをReactで実装します。

## Backend

| Area | Choice |
| --- | --- |
| Language | Go |
| CLI | Go `cmd/devos` |
| API server | Go `cmd/orchestrator` |
| Internal modules | Go `internal/*` |
| DB | SQLite |
| SQLite access | `database/sql` + SQLite driver |
| Validation | Go structs and explicit validation functions |
| Codex execution | `os/exec` で `codex exec` を実行 |
| Platform adapters | Windows / WSL / Linux local runner first |
| Process execution | `os/exec` via Runner interface |
| Windows runner | PowerShell / cmd |
| WSL runner | bash via local WSL/Linux process or `wsl.exe` bridge |
| Path handling | environment-aware PathMappingService |
| Logs | JSONL + Markdown summary + Orchestrator-owned run artifacts |

初期版では、Go標準ライブラリを優先します。HTTPルーティング、CLIフレームワーク、SQLite driverなどは、標準ライブラリだけで複雑になる箇所に絞って導入します。

## UI

| Area | Choice |
| --- | --- |
| Framework | React |
| Language | TypeScript |
| Build tool | Vite |
| Styling | Tailwind |
| Components | shadcn/ui style components |
| API client | Go API over local HTTP |

UIは状態の表示と人間の判断操作に集中します。ワークフローの正規状態、Decision Gate、Codex実行、diff保存はGoバックエンド側で管理します。

## Repository Shape

```text
cmd/
  devos/
  orchestrator/
internal/
  api/
  app/
  artifacts/
  codex/
  decisions/
  gitworktree/
  platforms/
  runners/
  pathmap/
  gitproviders/
  toolchains/
  runprofiles/
  memory/
  storage/
  tasks/
  verifier/
ui/
  src/
data/
projects/
```

## Verification

```text
go test ./...
pnpm --dir ui test
pnpm --dir ui lint
pnpm --dir ui build
```

## Boundary

- Go owns orchestration, persistence, execution, verification, and merge decisions.
- Go Core owns workflow and state.
- Platform adapters own OS-specific execution details.
- React owns screens, local UI state, forms, and user actions.
- TypeScript should not become the source of truth for workflow state.
- React must not implement path mapping, runner policy, or platform state transitions.
- Codex CLI is the first execution backend; Codex SDK remains a future option.
