# Current Task: Checkpoint 6

## Objective

Introduce a centralized Model Context Protocol registry so `ai-dev` can
define, validate, merge, list, inspect, and resolve MCP server
configuration independently of client adapters.

This checkpoint defines MCP configuration and readiness checks only. It
must not launch, supervise, or communicate with MCP servers.

## MCP model

Supported transports:

- `stdio`
- `http`

Registry entries are defined as named tables under
`[mcp.servers.<server-name>]` and merged using existing global/project
overlay semantics.

Legacy syntax remains temporarily compatible with a deterministic
interpretation:

```toml
[mcp]
servers = ["github", "postgres"]
```

## Commands

- `ai-dev mcp list [--enabled] [--json]`
- `ai-dev mcp show <server-name> [--json]`
- `ai-dev mcp resolve [--include-disabled] [--resolve-secrets]`
- `ai-dev mcp check [--json]`

## Safety and validation

- Unknown MCP fields are rejected.
- Secret values are never emitted by default inspection commands.
- `--resolve-secrets` is opt-in and atomic.
- `mcp check` validates readiness without network calls or server launch.
- Doctor includes MCP summary diagnostics.

## Required stable codes

- `invalid_mcp_server_name`
- `duplicate_mcp_server`
- `unsupported_mcp_transport`
- `missing_mcp_command`
- `invalid_mcp_command`
- `missing_mcp_url`
- `invalid_mcp_url`
- `conflicting_mcp_fields`
- `invalid_mcp_args`
- `invalid_mcp_environment`
- `invalid_mcp_headers`
- `invalid_mcp_timeout`
- `mcp_command_not_found`
- `mcp_working_directory_not_found`
- `mcp_secret_resolution_failed`
- `mcp_server_not_found`

## CLI exit status

- `0`: success
- `1`: validation, lookup, or resolution failure
- `2`: invalid command usage

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
