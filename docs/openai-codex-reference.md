# OpenAI Codex Reference

確認日: 2026-05-23

このメモは、Personal Dev OSのCodex実行層に関係する公式ドキュメント確認結果です。実装時にCLIフラグやSDK仕様が変わる可能性があるため、該当箇所は再確認してください。

## Checked Sources

- Codex non-interactive mode: <https://developers.openai.com/codex/noninteractive>
- Codex CLI reference: <https://developers.openai.com/codex/cli/reference>
- Agent approvals & security: <https://developers.openai.com/codex/agent-approvals-security>
- Configuration reference: <https://developers.openai.com/codex/config-reference>
- Config basics / configuration precedence: <https://developers.openai.com/codex/config-basic#configuration-precedence>
- Codex IDE extension settings: <https://developers.openai.com/codex/ide/settings#settings-reference>

## Notes

- `codex exec` はスクリプトやCI風の非対話実行に使う。
- `codex exec` は標準ではread-only sandboxで動くため、実装委任では必要最小権限として `--sandbox workspace-write` を明示する。
- `--json` を付けると newline-delimited JSON events を受け取れる。
- `--output-last-message` / `-o` で最終メッセージを書き出せる。
- `--output-schema` で最終応答をJSON Schemaへ適合させる構造化出力を要求できる。
- `PROMPT` に `-` を指定するとstdinからpromptを読める。
- `--ephemeral` でsession rollout filesをdiskへ永続化しない実行にできる。
- `--ignore-user-config` は `$CODEX_HOME/config.toml` を読み込まない。認証は引き続き `CODEX_HOME` を使う。
- `--ignore-rules` はuser/project execpolicy `.rules` を読み込まない。
- `--color never` でstdoutのANSI colorを無効化できる。
- `--ask-for-approval` は `untrusted` / `on-request` / `never` を指定できる。
- `--full-auto` は互換用として残っているが、新しい自動化では `--sandbox workspace-write` を優先する。
- `danger-full-access` は隔離済みCI runnerやコンテナなど、外側で制御された環境だけで検討する。
- sandboxとapprovalは別の制御であり、workspace-writeでもワークスペース外編集やネットワークアクセスには承認が必要になる構成がある。
- `--ask-for-approval untrusted` は、安全と見なされるread操作以外の、状態変更や外部実行につながるコマンドで承認を求める用途に使える。
- Auto-review modeは承認判断者を人間からreviewer agentへ差し替えるもので、sandbox境界や権限を広げるものではない。高リスク判断の最終代替にはしない。
- CLI flags / `--config`、profile、project config、user config、system config、built-in defaults の順で設定が解決される。
- workspace-writeのnetwork accessはデフォルトoffで、必要な場合は `sandbox_workspace_write.network_access` とnetwork proxy policyを明示する。
- CLI `-c key=value` はそのrunのconfig overrideとして使えるため、`-c 'sandbox_workspace_write.network_access=false'` をrunごとに明示できる。
- MCP serverを `required = true` にすると、初期化失敗時に `codex exec` はそのまま続行せずエラー終了する。
- Codex CLI referenceは `codex sandbox` にmacOS、Linux、Windowsのsandbox helperを持つ。
- Configuration referenceはnative Windows sandbox用の `windows.sandbox` と `windows.sandbox_private_desktop` を持つ。
- Codex IDE settingsは、WindowsでWSLを使う設定を持ち、repositories/toolingがWSL2にある場合やLinux-native toolingが必要な場合に使うと説明している。
- Codex IDE settingsは、そうでない場合はWindows sandboxでnative Windows実行できると説明している。
- Codex CLI is available on Windows, macOS, and Linux.
- On Windows, Codex can run natively in PowerShell with Windows sandbox.
- On Windows, Codex can also run in WSL2 when Linux-native tooling is needed.
- WSL CodexはWindows native Codexとは別execution environmentとして扱う。
- WSL1はCodex WSL adapterで受け付けない。WSL2 / Linux sandbox前提のpreflightを通す。
- Windows-native projects should prefer Windows filesystem when Windows-native agent/tooling is primary.
- WSL-primary projects should prefer WSL/Linux filesystem when Linux tooling is primary.

## Design Implications

- run単位でstdout JSONL、stderr、最終メッセージ、diff、review、summaryをOrchestrator-owned artifactとして保存する。
- Decision GateはCodexの最終メッセージだけに依存しない。
- プロセス終了コード、JSONLイベント、diff、verification結果を別々に評価する。
- sandbox、approval policy、network、output schemaはrunごとにCLI flagまたは安全profileで明示する。
- ユーザー環境差を減らす標準profileでは `--ignore-user-config`、`--ignore-rules`、`--ephemeral`、`--color never` を使う。
- promptはstdinで渡し、保存済みprompt artifactと実行内容を一致させる。
- raw approval promptはユーザーへ直接出さず、Orchestrator eventとして捕捉してHuman Inboxへ正規化する。
- approval promptの削減にはAuto-reviewを検討できるが、Auto-reviewは権限境界を広げない。production dependency、auth、DB schema、外部API、個人情報などの高リスク判断はHuman Inboxへ出す。
- `danger-full-access` をアプリの通常実行パスに入れない。
- 実装委任プロンプトには、タスクファイル、AGENTS.md、検証コマンド、禁止事項を明示する。

## Implementation Checks

- `codex exec --help` on Windows
- `codex exec --help` in WSL
- sandbox availability
- output-schema support
- CODEX_HOME behavior
- WSL version
