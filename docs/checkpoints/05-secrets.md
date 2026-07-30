# Checkpoint 5: secret references and provider abstraction

Checkpoint 5 adds runtime secret resolution without storing plaintext
secrets in TOML configuration.

## Reference syntax

Secret-backed values use:

```toml
[environment]
OPENAI_API_KEY = "secret://env/OPENAI_API_KEY"
DATABASE_URL = "secret://command/database-url"
```

Only values beginning exactly with `secret://` are treated as secret
references.

## Supported providers

- `env` reads the reference from the current process environment.
- `command` executes a configured command definition directly, without a
  shell.

Command-backed secrets are defined under `[secrets.commands]`:

```toml
[secrets.commands.database-url]
command = "op"
args = ["read", "op://Development/database/url"]
```

## Commands

- `ai-dev env --shell sh` resolves secret-backed environment values
  before emitting shell exports.
- `ai-dev secret resolve <reference>` resolves one secret reference and
  prints only the value.
- `ai-dev secret check` verifies all configured secret references.
- `ai-dev secret check --json` emits machine-readable results without
  secret contents.
- `ai-dev doctor` reports secret-provider status without exposing
  values.

## Validation and safety

Validation checks reference syntax, provider names, command definitions,
and command shapes before resolution runs. Resolution failures stop
output atomically: `ai-dev env` and direnv produce no partial exports.

Resolved values are cached only in memory for the current process and
are never written to disk.

## Test and rollback

Run:

```sh
gofmt -w .
go vet ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -o ./bin/ai-dev .
```

Rollback is the same as the earlier checkpoints:

1. restore the previous binary;
2. remove or revert `secret://` references from configuration;
3. remove `[secrets.commands.*]` definitions if they are no longer
   needed.
