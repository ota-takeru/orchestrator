# Environment Variables

## Goal

Human Inboxや設定UIから人間が環境変数を入力すると、対象プロジェクトの実行環境へ安全に反映されるようにします。AIやCodexに `.env` 本体を読ませたり、secret値をprompt、ログ、summary、diffへ出したりしません。

## Principle

- 環境変数の値は人間だけが入力する。
- Codexは `.env` 本体を読まない、書かない、表示しない。
- Orchestrator APIだけがsecret write pathを持つ。
- UI、DB、ログにはsecret値を平文保存しない。
- `.env.example`、必要キー一覧、検証結果、redacted metadataは表示してよい。
- 値の反映は監査可能にするが、監査ログに値は残さない。

## Credential Classes

Codex実行用credentialと対象アプリ用secretを混ぜてはいけません。初期実装から次の3分類を分けます。

| Class | Examples | Owner | Notes |
| --- | --- | --- | --- |
| `orchestrator_credentials` | `CODEX_API_KEY`, `CODEX_HOME`, Codex auth state | 人間 / Orchestrator runtime | Codex CLIを呼ぶための資格情報。通常のEnvironment Inputには出さない |
| `target_project_environment` | `OPENAI_API_KEY`, `DATABASE_URL`, app-specific secrets | 人間 / Target project | 対象アプリのruntime / verification用secret |
| `verification_environment` | smoke test用一時env、runtime test用env | Orchestrator verification runner | verification laneのchild processへ必要最小限だけ注入 |

`CODEX_API_KEY` はOrchestratorがCodex CLIを実行するためのcredentialです。対象アプリがOpenAI APIを使う場合の `OPENAI_API_KEY` とは別物として扱います。`CODEX_API_KEY`、`CODEX_HOME`、`~/.codex/auth.json` はHuman Inboxの通常Environment Inputに混ぜず、Codex auth preflight / settingsで扱います。

environment別credential例:

```text
orchestrator_credentials:
  windows-main:
    CODEX_HOME: %USERPROFILE%\.codex
    CODEX_API_KEY: redacted
  wsl-main:
    CODEX_HOME: ~/.codex
    CODEX_API_KEY: redacted
```

Windows Codex authとWSL Codex authは別credential boundaryです。`%USERPROFILE%\.codex` と `~/.codex` を同一視しません。

対象アプリ用secretをCodex implementation laneへ渡さない方針を維持します。必要な場合でも、promptに埋めずOrchestrator-managed process envとしてverification laneまたはruntime smoke test laneへだけ注入します。

## Human Inbox Flow

```text
Verification / Run
  -> missing_environment_variable を検出
  -> Human Inbox に入力カードを作成
  -> 人間がUIで値を入力
  -> Orchestrator APIが形式検証
  -> Secret Store または .env.local へ書き込み
  -> environment_bindings を更新
  -> 該当runを再開または再実行
```

Human Inbox card例:

```text
Action Required: Missing environment variables

Task:
TASK-008 外部API接続の疎通確認

Why:
OPENAI_API_KEY が未設定のため、verification commandを実行できません。

Input:
[OPENAI_API_KEY] [••••••••••••••••]

Apply to:
(*) Recommended: This project only
( ) This task run only
( ) User-level default for future projects

Reason:
このAPIキーは現在のプロジェクト検証にだけ必要です。

After Apply:
- .env.local に値を書き込む
- environment_bindings にredacted metadataを保存する
- RUN-20260521-008 を再実行する

Actions:
[Save and Rerun] [Skip verification that needs this key] [Mark this requirement as not needed]
```

## Storage Options

初期完成スコープではOS keychain連携を必須にしません。まずはproject-local secret fileへ書きます。ただしsecret値をSQLiteに保存しません。

| Storage | Initial scope | Notes |
| --- | --- | --- |
| `.env.local` | yes | Git ignore必須。Codexからはdeny-readにする。 |
| OS keychain | later | macOS Keychain、Windows Credential Manager、libsecretなど。 |
| external secret manager | later | ローカルファースト初期完成スコープでは対象外。 |

推奨ファイル:

```text
my-app/
  .env.local        # secret values, gitignored, Codex deny-read
  .env.example      # non-secret keys and comments, reviewable
  .devagent/
    environment/
      required.yaml
      bindings.yaml # redacted metadata only
```

`.env.local` は人間入力の反映先であり、AI成果物ではありません。差分レビューにも含めません。

## Required Environment YAML

`.devagent/environment/required.yaml`:

```yaml
required_environment:
  - key: OPENAI_API_KEY
    required_for:
      - verification
      - runtime
    status: missing
    source_hint: user_input
    validation:
      type: secret
      min_length: 20
    scope_options:
      - project
      - run
      - user_default
    description: OpenAI APIを使う機能の疎通確認に必要。

  - key: DATABASE_URL
    required_for:
      - runtime
    status: missing
    source_hint: user_input
    validation:
      type: url
      allowed_schemes:
        - postgres
        - sqlite
```

`.devagent/environment/bindings.yaml`:

```yaml
bindings:
  - key: OPENAI_API_KEY
    scope: project
    storage: env_file
    path: .env.local
    status: configured
    value_fingerprint: sha256:...
    redacted_preview: sk-...abcd
    created_by: human
    created_at: 2026-05-21T10:00:00+09:00
    last_used_by_run_id: RUN-20260521-008
```

`value_fingerprint` は同一値かどうかの比較用です。値の復元に使えないhashにし、saltはOrchestrator管理にします。

## Data Model

SQLiteにはsecret値を保存しません。状態、scope、保存先、redacted preview、fingerprintだけを保存します。

```sql
CREATE TABLE environment_requirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  key TEXT NOT NULL,
  required_for TEXT NOT NULL, -- implementation | verification | runtime | runtime_smoke | deployment
  status TEXT NOT NULL, -- missing | requested | configured | invalid | waived | cancelled | revoked
  source_hint TEXT NOT NULL, -- user_input | generated_example | external_secret
  validation_json TEXT NOT NULL,
  description TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE environment_bindings (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  key TEXT NOT NULL,
  scope TEXT NOT NULL, -- project | task | run | user_default
  scope_id TEXT,
  storage TEXT NOT NULL, -- env_file | os_keychain | external_secret
  storage_ref TEXT NOT NULL,
  status TEXT NOT NULL, -- configured | missing | invalid | revoked
  redacted_preview TEXT,
  value_fingerprint TEXT,
  created_by TEXT NOT NULL, -- human | policy
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_by_run_id TEXT
);

CREATE TABLE environment_audit_events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  environment_id TEXT,
  binding_id TEXT,
  requirement_id TEXT,
  key TEXT NOT NULL,
  action TEXT NOT NULL, -- requested | configured | updated | revoked | used | validation_failed
  actor TEXT NOT NULL,
  scope TEXT NOT NULL,
  scope_id TEXT,
  run_id TEXT,
  command_event_id TEXT,
  redacted_preview TEXT,
  created_at TEXT NOT NULL
);
```

- project共通のsecretは `environment_id NULL` を許可する。
- 実行時注入では必ず `environment_id` へ解決する。
- WindowsとWSLで保存先pathやsecret storeが異なる場合、別bindingとして扱う。
- audit eventには、どのenvironment / binding / requirement / run / command_eventで使われたかを追えるIDを保存する。
- uniquenessはenvironment-awareにする。`environment_id IS NULL` のglobal requirement / bindingと、environment別requirement / bindingはpartial unique indexで分ける。
- Visual Studio、MSBuild、Windows SDK、bubblewrap、GitなどはEnvironment InputではなくToolchainRequirementとして扱う。
- Environment Input Cardでtoolchain installを促してはいけない。

## API Boundary

Secret値を扱うAPIは専用endpointに分けます。通常のproject、task、run、decision APIのresponseにsecret値を含めません。

```text
POST /api/projects/{project_id}/environment/requirements
GET  /api/projects/{project_id}/environment/status
POST /api/projects/{project_id}/environment/bindings
POST /api/projects/{project_id}/environment/bindings/{binding_id}/revoke
```

`POST /bindings` のrequest bodyにはsecret値が含まれますが、responseには含めません。

CLIでsecret値を扱う場合も、値をコマンドライン引数にしてはいけません。

```text
devos env set OPENAI_API_KEY --scope project
  -> TTY password promptで入力

devos env set OPENAI_API_KEY --scope project --value-stdin
  -> automation用にstdinから読む
```

`--value sk-...` のようなflagは初期実装では提供しません。shell history、process list、logsへsecretが残るためです。

```json
{
  "key": "OPENAI_API_KEY",
  "scope": "project",
  "value": "secret-value-from-human",
  "rerun_after_save": "RUN-20260521-008"
}
```

response例:

```json
{
  "key": "OPENAI_API_KEY",
  "scope": "project",
  "status": "configured",
  "redacted_preview": "sk-...abcd",
  "next_action": "rerun",
  "run_id": "RUN-20260521-008"
}
```

## Execution Injection

Codex実行時にsecret値をpromptへ入れません。verification commandやruntime commandへ必要な場合だけ、Orchestratorがchild process environmentとして注入します。

```text
Orchestrator
  -> load .env.local / secret store
  -> construct command env
  -> run verification command
  -> redact stdout/stderr/events before saving
```

Codex implementation laneには原則secretを渡しません。secretが必要なのはverification laneやruntime smoke testです。やむを得ず実装中のコマンドに必要な場合も、promptではなくprocess envで渡し、ログ保存前にredactionします。

## Decision Gate

環境変数関連のGate action:

| Detector | Action |
| --- | --- |
| `missing_environment_variable` | `HUMAN_INPUT` |
| `invalid_environment_variable` | `HUMAN_INPUT` or `HUMAN_DECISION` |
| `secret_value_in_diff` | `HARD_BLOCK` |
| `secret_value_in_logs` | `HARD_BLOCK` |
| `env_example_changed` | `HUMAN_DECISION` |
| `env_local_changed_by_codex` | `HARD_BLOCK` |

不足環境変数は、通常のDecision Reportではなく `HUMAN_INPUT` のEnvironment Input CardとしてHuman Inboxへ出します。人間は値を入力するだけでよく、実装方針判断を求めません。

## UI Requirements

- 入力欄はpassword表示を標準にする。
- show/hide toggleを置くが、表示中もcopyやscreen captureの注意を出す。
- 値はsubmit後にUI stateから即破棄する。
- 保存後はredacted previewだけ表示する。
- scopeを選べるようにする: project / task / run / user_default。初期選択は `project` を推奨として表示し、理由を1文で添える。
- `Save and Rerun` で該当runを自動再開できる。
- secretが必要な検証だけをskipする操作と、要件自体を不要扱いにする操作は、別のボタン文言として明確に分ける。
- `.env.example` にないkeyを追加する場合は説明入力を必須にし、Decision Gate対象にする。

## Security Requirements

- `.env.local` はgitignore確認を必須にする。
- `.env.local` がGit trackedなら `HARD_BLOCK`。
- Codex permission profileで `.env`、`.env.local`、`.env.example` 以外の `.env.*` をdeny-readにする。`.env.example` は非secretの必要キー一覧として扱い、変更時はDecision Gate対象にする。
- secret値はSQLite、prompt、events.jsonl、stdout、stderr、summary、decision reportへ保存しない。
- redactionは保存前に行う。
- redaction漏れを検出したらrunを `blocked_on_policy` にする。
