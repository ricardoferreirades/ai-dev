# Checkpoint 6: MCP registry and resolved MCP configuration

Checkpoint 6 introduces a client-neutral MCP registry model in ai-dev.
The registry is validated, merged, inspected, and resolved centrally,
without generating client-specific files.

## Schema v1 MCP reference

Top-level shape:

```toml
schema = "v1"

[environment]
GLOBAL_TIMEOUT = "30"

[mcp.servers.github]
transport = "stdio"
command = "github-mcp-server"
args = ["stdio"]
enabled = true
timeout_seconds = 15
inherit_environment = false

[mcp.servers.github.environment]
LOG_LEVEL = "debug"
GITHUB_TOKEN = "secret://env/GITHUB_TOKEN"

[mcp.servers.remote]
transport = "http"
url = "https://mcp.example.com"
enabled = true
timeout_seconds = 20

[mcp.servers.remote.headers]
Authorization = "secret://env/MCP_AUTH_TOKEN"
```

### Server name rules

Server names must:

- be non-empty;
- contain only ASCII letters, digits, underscores, and hyphens;
- begin with a letter or digit;
- remain case-sensitive.

### Supported transports

- `stdio`
- `http`

### `stdio` fields

Required:

- `command` (non-empty string)

Optional:

- `args` (array of strings)
- `cwd` (string)
- `environment` (table of scalar environment values)
- `enabled` (boolean, defaults to `true`)
- `timeout_seconds` (positive integer)
- `inherit_environment` (boolean, defaults to `false`)

### `http` fields

Required:

- `url` (absolute `http` or `https` URL)

Optional:

- `headers` (table of string values)
- `enabled` (boolean, defaults to `true`)
- `timeout_seconds` (positive integer)

### Conflict rules

Invalid combinations include:

- `transport = "stdio"` with `url` or `headers`
- `transport = "http"` with `command`, `args`, `cwd`, `environment`, or `inherit_environment`

### Unknown field behavior

Unknown MCP fields are rejected by validation.

## Merge and override behavior

Global and project files are merged using existing recursive merge rules.
Project overlays can override only the fields they need.

Example overlay:

```toml
schema = "v1"

[mcp.servers.github]
enabled = false

[mcp.servers.github.environment]
LOG_LEVEL = "trace"

[mcp.servers.postgres]
transport = "stdio"
command = "postgres-mcp"
args = ["--read-only"]
```

This can:

- disable a global server;
- override one environment value;
- add a new server;
- merge arrays using the existing ordered de-duplication rules.

## Environment inheritance and precedence

For `stdio` servers, resolved environment values are composed in order:

1. process environment, only when `inherit_environment = true`;
2. resolved top-level `[environment]` values;
3. server-level `[mcp.servers.<name>.environment]` values.

Server-level values have the highest precedence.

When `inherit_environment` is omitted or `false`, the process
environment is not serialized into resolved output.

## Secret behavior

Default MCP inspection commands keep references unchanged, for example:

- `secret://env/MCP_AUTH_TOKEN`

By default, these commands do not resolve secrets:

- `ai-dev mcp list`
- `ai-dev mcp show <name>`
- `ai-dev mcp resolve`

Secret resolution is opt-in only:

```sh
ai-dev mcp resolve --resolve-secrets
```

Resolution uses the Checkpoint 5 provider abstraction and is atomic.
If any secret fails, no partial JSON registry is printed.

## Commands

### `ai-dev mcp list`

Lists resolved MCP servers with:

- `name`
- `transport`
- `enabled`
- `scope` (`global`, `project`, `merged`, or `resolved`)

Filter to enabled servers only:

```sh
ai-dev mcp list --enabled
```

JSON output:

```sh
ai-dev mcp list --json
```

### `ai-dev mcp show <server-name>`

Shows one resolved server definition.

```sh
ai-dev mcp show github
ai-dev mcp show github --json
```

Missing servers fail with:

- `mcp_server_not_found`

### `ai-dev mcp resolve`

Emits the active client-neutral MCP registry JSON contract:

```json
{
  "servers": {
    "github": {
      "transport": "stdio",
      "command": "github-mcp-server",
      "args": [
        "stdio"
      ],
      "environment": {},
      "enabled": true
    }
  }
}
```

Include disabled servers:

```sh
ai-dev mcp resolve --include-disabled
```

Resolve secret-backed environment/header values:

```sh
ai-dev mcp resolve --resolve-secrets
```

### `ai-dev mcp check`

Validates enabled server readiness without launching MCP servers.

Checks include:

- `stdio`: command shape, executable lookup, `cwd` existence, secret resolvability
- `http`: URL validity, secret-backed header resolvability

No HTTP connection attempts are performed.

JSON mode:

```sh
ai-dev mcp check --json
```

Example result item:

```json
{
  "name": "github",
  "transport": "stdio",
  "valid": true,
  "checks": [
    "command_field_valid",
    "command_available",
    "secret_references_resolvable"
  ],
  "errors": []
}
```

## Doctor integration

`ai-dev doctor` now reports an MCP summary with:

- configured server count;
- enabled server count;
- invalid definitions;
- unavailable executables;
- invalid working directories;
- unresolved secret references;
- unsupported transports.

## Stable MCP codes

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

## Legacy MCP array compatibility

Legacy schema syntax is still accepted with a warning:

```toml
[mcp]
servers = ["github", "postgres"]
```

Validation emits a deprecation warning. The deterministic temporary
interpretation maps each server name to a `stdio` entry with:

- `transport = "stdio"`
- `command = <server-name>`
- `args = []`

## Testing and verification

```sh
gofmt -w .
go vet ./...
go test ./...

temporary_binary="$(mktemp "${TMPDIR:-/tmp}/ai-dev.XXXXXX")"
CGO_ENABLED=0 go build -trimpath -o "$temporary_binary" .
"$temporary_binary" mcp list --json
rm -f "$temporary_binary"
```

Automated tests are isolated and do not:

- launch real MCP servers;
- contact remote HTTP endpoints;
- use real secrets;
- write user configuration.

## Rollback

1. Restore the previous ai-dev binary.
2. Remove or disable `[mcp.servers.*]` sections that require Checkpoint 6.
3. Keep or revert legacy `[mcp] servers = [...]` syntax as needed.
4. Re-run:

```sh
ai-dev doctor
ai-dev validate
```

to confirm the rolled-back behavior.
