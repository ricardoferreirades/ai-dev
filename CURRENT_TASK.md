# Current Task: Checkpoint 5

## Objective

Introduce secret references and a provider abstraction so `ai-dev` can
resolve sensitive values at runtime without storing plaintext secrets in
project or global TOML configuration.

The checkpoint must not change project detection, configuration merge
semantics, valid environment export, or direnv behavior.

## Secret model

Secret references use the syntax `secret://<provider>/<reference>` and
are resolved at runtime only.

Supported providers:

- `env`
- `command`

Command-backed secrets are defined under `[secrets.commands.<name>]`.

## Resolution flow

Secret validation applies before environment export or inspection. The
centralized secret resolver is reused by:

- `ai-dev env [--shell sh]`
- `ai-dev secret resolve <reference>`
- `ai-dev secret check [--json]`
- `ai-dev doctor`
- future secret-aware consumers

Resolved values are cached only for the current process and never
written to disk.

## Required stable codes

- `invalid_secret_reference`
- `unknown_secret_provider`
- `missing_secret_value`
- `empty_secret_value`
- `missing_secret_command`
- `secret_command_failed`
- `secret_command_empty_output`
- `invalid_secret_command`
- `secret_resolution_failed`

## CLI exit status

- `0`: success
- `1`: validation or resolution failure
- `2`: invalid command usage

## Acceptance criteria

- `secret://env/<name>` references are recognized.
- Environment-provider references resolve when the variable exists.
- Missing and empty environment variables fail safely.
- `secret://command/<name>` references are recognized.
- Command providers are validated and executed without a shell.
- Nonzero command exit status and empty output fail safely.
- Plaintext environment values continue to work.
- `ai-dev env` resolves secret-backed environment values atomically.
- `ai-dev secret resolve <reference>` prints only the resolved value.
- `ai-dev secret check` and `--json` inspect without exposing values.
- `ai-dev doctor` reports secret-provider status safely.
- Duplicate references resolve only once per command execution.
- No resolved values are written to disk.
- Existing Checkpoint 1–4 commands remain compatible.
- Automated tests use isolated fixtures only.
- `go test ./...`, `go vet ./...`, and a static build pass.
- Documentation covers usage, security, failure modes, and rollback.

## Out of scope

- Persistent secret caching
- Additional secret providers
- Secret creation or rotation
- MCP execution or client generation
- Prompt or rule file loading
