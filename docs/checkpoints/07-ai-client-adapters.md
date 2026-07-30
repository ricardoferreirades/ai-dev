# Checkpoint 7: AI client adapter framework

Checkpoint 7 adds a centralized client adapter layer that translates the
resolved, client-neutral MCP model into client-specific configuration.

Supported clients in this checkpoint:

- `codex`
- `claude`
- `cursor`
- `vscode`

Adapters consume the same resolved source model used by `ai-dev mcp resolve`.
They do not reparse global/project TOML directly, and they do not duplicate
project detection, merge, schema validation, secret-reference parsing, or MCP
registry parsing logic.

## Adapter architecture

A shared adapter contract is defined in `client.go`:

- identity (`Name`)
- capability declaration (`Capabilities`)
- format/scope support (`SupportedFormats`, `SupportedScopes`)
- transport/field compatibility (`SupportedTransports`, `SupportedFields`)
- destination discovery (`Destinations`)
- generation (`Generate`)
- compatibility validation (`Validate`)

All client adapters are isolated behind this interface. Adding new adapters
(eg. `gemini`, `windsurf`, `continue`, `amp`, `zed`) requires only a new
adapter registration entry.

## Client commands

### List adapters

```sh
ai-dev client list
ai-dev client list --json
```

Reports:

- client name
- adapter availability
- supported feature set
- default output format
- destination-discovery support

### Show adapter details

```sh
ai-dev client show <client>
ai-dev client show <client> --json
```

Reports:

- supported formats
- supported scopes
- supported transports
- supported fields
- known limitations
- destination candidates
- unresolved secret-reference compatibility

### Discover default destination candidates

```sh
ai-dev client path <client>
ai-dev client path <client> --scope user
ai-dev client path <client> --scope project
ai-dev client path <client> --json
```

This command is read-only and never creates files or directories.

### Validate adapter compatibility

```sh
ai-dev client validate <client>
ai-dev client validate <client> --strict
ai-dev client validate <client> --scope user
ai-dev client validate <client> --format json
ai-dev client validate <client> --json
```

Validation detects incompatibilities including unsupported transports,
unsupported fields, unresolved required secrets, and scope/format mismatches.

### Generate client configuration

```sh
ai-dev client generate <client>
ai-dev client generate <client> --json
ai-dev client generate <client> --format <format>
ai-dev client generate <client> --scope user
ai-dev client generate <client> --scope project
ai-dev client generate <client> --include-disabled
ai-dev client generate <client> --resolve-secrets
ai-dev client generate <client> --with-metadata
ai-dev client generate <client> --strict
```

Default output is standard output. Adapters never install into real client
configuration files in this checkpoint.

### Optional output file

```sh
ai-dev client generate <client> --output generated/<name>.json
ai-dev client generate <client> --output generated/<name>.json --force
```

Output file safeguards:

- write occurs only when `--output` is explicitly provided;
- file generation and serialization complete before commit;
- writing uses a same-directory temporary file;
- final write uses atomic rename;
- existing files are protected unless `--force` is set;
- parent directory must already exist;
- output path must stay repository-local;
- failures leave existing files unchanged;
- temporary files are cleaned up;
- files containing resolved secrets are written with restrictive permissions
  (`0600` on POSIX systems).

### Compare clients

```sh
ai-dev client compare
ai-dev client compare --json
```

Summarizes representability across all registered adapters:

- fully supported features
- partially supported features
- unsupported features
- generation blockers

## Scope behavior

Supported scopes:

- `user`
- `project`

Scope selection influences:

- destination discovery metadata
- generated payload scope metadata
- validation compatibility checks

Unsupported scope selections fail with a stable usage/validation error.

## Capability model

Adapters declare support status for:

- `mcp`
- `environment`
- `prompts`
- `rules`

Checkpoint 7 only generates MCP configuration. Non-MCP capabilities are
reported explicitly as partial/unsupported by adapter capability declarations.

## Disabled MCP servers

- Disabled servers are excluded from generated output by default.
- `--include-disabled` includes disabled entries where representable.
- If disabled-state semantics are not representable for a client, the adapter
  emits a compatibility warning (or fails in strict mode).

## Secret handling

Default generation preserves `secret://` references.

`--resolve-secrets` performs resolution through the existing provider abstraction.

Safety guarantees:

- resolution is atomic;
- no partial generation output is emitted on failure;
- diagnostics never include resolved secret values;
- metadata never includes secret values.

If a client cannot consume unresolved references, generation fails with a clear
error unless `--resolve-secrets` is provided.

## Determinism

Generation is deterministic for a given resolved configuration and option set:

- stable adapter ordering;
- stable MCP server ordering;
- stable environment/header ordering;
- stable diagnostic ordering;
- stable serialized fields (except explicitly requested timestamp metadata).

## Stable client error codes

- `unknown_client`
- `client_disabled`
- `unsupported_client_format`
- `unsupported_client_scope`
- `unsupported_client_transport`
- `unsupported_client_field`
- `client_generation_failed`
- `client_validation_failed`
- `client_path_ambiguous`
- `client_path_unavailable`
- `client_output_exists`
- `client_output_write_failed`
- `client_secret_resolution_failed`
- `client_configuration_incompatible`

## Doctor integration

`ai-dev doctor` now includes a client adapter section:

- registered adapters
- adapter initialization failures
- detected client executables (best-effort)
- destination ambiguity notices
- compatibility status against the current resolved MCP model

Doctor does not require client binaries to be installed.

## Schema integration

Schema `v1` now accepts optional client overrides:

```toml
[clients.codex]
enabled = true

[clients.claude]
enabled = true

[clients.cursor]
enabled = false

[clients.vscode]
enabled = true
```

Rules:

- unknown client names fail validation;
- unknown fields under `clients.<name>` fail validation;
- currently supported override field is `enabled`.

## Known limitations in Checkpoint 7

- Adapters generate MCP configuration only.
- Prompt/rule/environment client payload generation is intentionally deferred.
- No direct installation into real client configuration files.
- No automatic client process restart or live reload.

## Rollback

To rollback Checkpoint 7 behavior:

1. Revert the source changes introducing `client.go` and CLI wiring.
2. Restore `validation.go` without the `clients` namespace checks.
3. Rebuild:

```sh
CGO_ENABLED=0 go build -trimpath -o ./bin/ai-dev .
```

4. Re-run verification:

```sh
go test ./...
go vet ./...
```
