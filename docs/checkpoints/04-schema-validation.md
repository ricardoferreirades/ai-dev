# Checkpoint 4: configuration schema and validation

## Overview

Checkpoint 4 adds a stable, versioned contract for ai-dev configuration.
Every existing global and project source is validated independently, then
the merged configuration is validated again before it is consumed.

The current version is `v1`. Schema dispatch is explicit so later
versions can add migrations, deprecations, and rules without rewriting
command handlers.

## Schema v1 reference

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `schema` | string | New files | Must be `"v1"` |
| `name` | string | No | Human-readable owner or configuration name |
| `profile` | string | No | Existing profile label |
| `environment` | table | No | Environment variable names and scalar values |
| `mcp` | table | No | MCP configuration namespace |
| `mcp.servers` | array of strings | No | Declared MCP server identifiers |
| `prompts` | table | No | Prompt configuration namespace |
| `prompts.default` | string | No | Default prompt reference |
| `prompts.project` | string | No | Project prompt reference |
| `rules` | table | No | Rules configuration namespace |
| `rules.enabled` | array of strings | No | Enabled rule identifiers |

Environment values retain the Checkpoint 3A contract: strings, booleans,
integers, and floats are accepted. Variable names must match
`[A-Za-z_][A-Za-z0-9_]*`. Arrays and nested tables are rejected as
environment values.

Example:

```toml
schema = "v1"
name = "Ricardo"
profile = "default"

[environment]
EDITOR = "vim"
AI_FEATURE = true

[mcp]
servers = ["filesystem", "github"]

[prompts]
default = "prompts/default.md"
project = "prompts/project.md"

[rules]
enabled = ["safe-shell"]
```

Unknown top-level keys are errors. The defined `mcp`, `prompts`, and
`rules` tables also reject unknown keys. The `environment` table is open
to valid environment variable names.

## Validation

Validate the configuration for the current project:

```sh
ai-dev validate
```

The command reports participating sources in merge-precedence order,
followed by warnings and errors. It exits `0` when no errors exist.

Strict mode treats deprecation warnings as failures:

```sh
ai-dev validate --strict
```

Machine-readable output is available as deterministic JSON:

```sh
ai-dev validate --json
```

Example:

```json
{
  "valid": false,
  "errors": [
    {
      "source": "/home/user/.config/ai-dev/global.toml",
      "path": "mcp.servers",
      "code": "invalid_type",
      "severity": "error",
      "message": "mcp.servers must be an array of strings"
    }
  ],
  "warnings": [],
  "sources": [
    "/home/user/.config/ai-dev/global.toml"
  ]
}
```

Exit statuses are:

- `0` for valid configuration;
- `1` for validation errors or strict-mode warnings;
- `2` for invalid command usage.

## Stable finding codes

- `missing_schema`
- `unsupported_schema`
- `unknown_field`
- `invalid_type`
- `invalid_value`
- `deprecated_field`
- `conflicting_value`
- `invalid_environment_name`
- `invalid_environment_value`

Messages identify source paths, field paths, and expected shapes. They do
not include environment values, credentials, tokens, or secret material.

## Doctor, config, env, and direnv

`ai-dev doctor` distinguishes:

- missing optional source files;
- valid configuration;
- deprecated legacy configuration;
- unsupported schema versions;
- conflicting merged shapes;
- other invalid configuration.

`ai-dev config` and `ai-dev env` validate before producing resolved
output. Warnings are written to standard error and do not block normal
operation. Errors prevent all resolved or shell output.

The direnv helper already evaluates exports only after `ai-dev env`
returns successfully. A validation failure therefore produces no export
script and cannot partially apply values from invalid configuration.

## Legacy migration

Existing files without `schema` remain compatible in normal mode but
produce a `missing_schema` warning. Migrate by adding this top-level line
before any TOML table:

```toml
schema = "v1"
```

Then run:

```sh
ai-dev validate --strict
```

Strict validation should succeed before relying on the file in automated
workflows.

An explicit unsupported version such as `schema = "v2"` is never treated
as legacy `v1`. Change it only after confirming that the file uses the
documented `v1` fields and types.

## Resolving common errors

Unknown field:

```text
path=mcp.servres code=unknown_field
```

Rename the field to `mcp.servers`.

Invalid array:

```text
path=rules.enabled code=invalid_type
```

Use an array containing only strings.

Conflicting value:

```text
path=schema code=conflicting_value
```

Ensure global and project files declare the same schema version.

Invalid TOML is reported against the source document without echoing its
contents. Correct the TOML syntax, then rerun `ai-dev validate`.

## Testing

Run:

```sh
gofmt -w main.go validation.go validation_test.go direnv_shell_test.go
go test ./...
go vet ./...

temporary_binary="$(mktemp "${TMPDIR:-/tmp}/ai-dev.XXXXXX")"
CGO_ENABLED=0 go build -trimpath -o "$temporary_binary" .
"$temporary_binary" version
rm -f "$temporary_binary"
```

Tests use temporary configuration, data, state, repository, and worktree
directories. They do not modify the user's ai-dev configuration or shell
startup files.

## Rollback

To roll back the application change, restore the previous ai-dev binary.
No automatic migration or user-file mutation is performed by this
checkpoint.

If `schema = "v1"` was added to a legacy file, it is safe to leave in
place. Removing it restores legacy compatibility mode and its
deprecation warning. Remove newly added schema-only fields only when the
older binary does not recognize them.

After rollback, rerun:

```sh
ai-dev doctor
ai-dev env --shell sh
```

to confirm the previous behavior.
