# Checkpoint 10: Plugin Architecture

## Objective

Introduce a controlled plugin architecture for `ai-dev` that extends
providers, registries, adapters, transforms, and validation without
loading untrusted code in-process.

Plugins are external executables discovered from local filesystem paths,
validated through manifest metadata, and invoked through a versioned
newline-delimited JSON protocol.

## Capability types

Supported plugin capabilities:

- `secret-provider`
- `client-adapter`
- `mcp-transform`
- `prompt-provider`
- `rule-provider`
- `validator`

## Discovery model

Plugin discovery is deterministic and path-ordered.

Search path precedence:

1. `--plugin-path <path>` (repeatable)
2. `AI_DEV_PLUGIN_PATH` (path-list separated)
3. configured paths from `[plugins].paths`
4. default path: `~/.local/share/ai-dev/plugins`

Each plugin is a directory containing `plugin.toml`.

## Manifest

`plugin.toml` schema fields:

Required:

- `schema = "v1"`
- `id`
- `name`
- `version`
- `protocol = "ai-dev-plugin-v1"`
- `executable`
- `capabilities`

Optional:

- `description`
- `author`
- `homepage`
- `license`
- `minimum_ai_dev_version`
- `maximum_ai_dev_version`
- `platforms`
- `architectures`

Identifier validation:

- non-empty
- starts with ASCII letter or digit
- contains only ASCII letters, digits, `-`, `_`, `.`
- unique across discovered plugins

## Protocol

Protocol version:

- `ai-dev-plugin-v1`

Transport:

- request/response via NDJSON over stdin/stdout
- stdout reserved for protocol messages
- stderr reserved for diagnostics

### Request lifecycle

1. handshake
2. capability discovery
3. operation execution

### Request shape

```json
{"protocol":"ai-dev-plugin-v1","type":"handshake","plugin_id":"onepassword","ai_dev_version":"0.10.0"}
{"protocol":"ai-dev-plugin-v1","type":"capabilities","plugin_id":"onepassword"}
{"protocol":"ai-dev-plugin-v1","type":"run","plugin_id":"onepassword","capability":"secret-provider","operation":"resolve","input":{"provider":"onepassword","reference":"vault/item"}}
```

### Response shape

```json
{"protocol":"ai-dev-plugin-v1","ok":true,"plugin_id":"onepassword","plugin_version":"1.0.0"}
{"protocol":"ai-dev-plugin-v1","ok":true,"capabilities":[{"name":"secret-provider","operations":["resolve"],"input_schema_version":"v1","output_schema_version":"v1","metadata":{"providers":["onepassword"]}}]}
{"protocol":"ai-dev-plugin-v1","ok":true,"output":{"value":"..."}}
```

Failures are represented with `ok=false`, stable `code`, and safe `message`.

## Process model and isolation

- Plugins run as separate processes.
- No shared-library loading.
- No shell wrapper (`sh -c` / `bash -c`).
- Finite timeout enforced (default 10 seconds).
- Output limits enforced:
  - stdout bytes
  - stderr bytes
  - response count
  - message size

## Plugin execution environment

Default plugin process environment is minimal:

- `PATH`
- `HOME`
- `TMPDIR`
- `AI_DEV_PLUGIN_PROTOCOL`
- `AI_DEV_PLUGIN_ID`

Optional environment extensions are configured per plugin under
`[plugins.<id>.environment]`.

Default working directory is the plugin directory unless explicitly
configured with `working_directory`.

## Configuration namespace

Top-level plugin configuration:

```toml
[plugins]
paths = ["~/custom/ai-dev/plugins"]

[plugins.onepassword]
enabled = true
timeout_seconds = 10
working_directory = "./"
inherit_environment = false

[plugins.onepassword.environment]
OP_ACCOUNT = "my.1password.com"

[plugins.onepassword.config]
account = "my.1password.com"
```

`config` is treated as opaque by the core.

## Commands

- `ai-dev plugin list [--json]`
- `ai-dev plugin show <plugin-id> [--json] [--handshake]`
- `ai-dev plugin validate [<plugin-id>] [--json]`
- `ai-dev plugin status [--json]`
- `ai-dev plugin refresh [--json]`
- `ai-dev plugin run <plugin-id> <operation> [--capability <name>] [--input <path>] [--json]`

## Integrations

- Secret-provider plugins register provider names consumed by
  `secret://<provider>/...` references.
- Prompt/rule provider plugins contribute resources into the existing
  registry model with plugin provenance in `source`.
- Validator plugins append findings to core validation output without
  modifying resolved configuration.
- `config sources` includes plugin manifest provenance entries.
- `doctor` includes plugin discovery/compatibility/handshake reporting.

## Stable error codes

- `plugin_not_found`
- `invalid_plugin_manifest`
- `unsupported_plugin_manifest_schema`
- `invalid_plugin_identifier`
- `duplicate_plugin_identifier`
- `plugin_executable_not_found`
- `plugin_executable_not_executable`
- `unsupported_plugin_protocol`
- `plugin_version_incompatible`
- `plugin_platform_incompatible`
- `plugin_architecture_incompatible`
- `plugin_capability_mismatch`
- `plugin_capability_conflict`
- `plugin_handshake_failed`
- `plugin_timeout`
- `plugin_output_invalid`
- `plugin_output_too_large`
- `plugin_execution_failed`
- `plugin_operation_unsupported`
- `plugin_disabled`
- `plugin_configuration_invalid`

## Safety notes

- Discovery and manifest parsing do not execute plugin binaries.
- Plugins run only when explicitly invoked or when a command requires an
  enabled plugin capability.
- Diagnostics are sanitized to avoid secret leakage.
- Core commands continue to work when no plugins are installed.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
