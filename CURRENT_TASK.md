# Current Task: Checkpoint 7

## Objective

Introduce a centralized AI client adapter framework so `ai-dev` can
translate the validated, resolved client-neutral MCP model into
client-specific configuration for supported tools.

This checkpoint adds adapter listing, validation, generation,
destination discovery, comparison, and safe optional output-file writing.
It must not install into real client configuration files.

## Adapter model

Supported adapters:

- `codex`
- `claude`
- `cursor`
- `vscode`

Adapters consume resolved MCP servers from existing config merge and
validation flows. They do not reparse global/project files.

Optional client overrides are supported via schema v1:

```toml
[clients.codex]
enabled = true

[clients.cursor]
enabled = false
```

## Commands

- `ai-dev client list [--json]`
- `ai-dev client show <client> [--json]`
- `ai-dev client path <client> [--scope <scope>] [--json]`
- `ai-dev client validate <client> [--scope <scope>] [--format <format>] [--strict] [--json]`
- `ai-dev client generate <client> [--json] [--format <format>] [--scope <scope>] [--include-disabled] [--resolve-secrets] [--with-metadata] [--strict] [--output <path>] [--force]`
- `ai-dev client compare [--json]`

## Safety and validation

- Adapters validate transport, field, scope, format, and secret compatibility.
- Unsupported field handling is explicit via warnings/errors.
- `--resolve-secrets` is opt-in and atomic.
- `--strict` upgrades warnings to generation/validation failures.
- Optional output-file writes are atomic and overwrite-protected.
- Doctor includes adapter registration, path ambiguity, executable detection, and compatibility diagnostics.

## Required stable codes

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

## CLI exit status

- `0`: success
- `1`: validation, lookup, or resolution failure
- `2`: invalid command usage

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
