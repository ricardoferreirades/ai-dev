# Current Task: Checkpoint 4

## Objective

Introduce a versioned configuration schema and reusable validation layer
before environment activation or future MCP, secret, and client-specific
resolution.

The checkpoint must not change project detection, configuration merge
semantics, valid environment export, or direnv behavior.

## Schema

The current supported schema version is:

```toml
schema = "v1"
```

Schema `v1` recognizes:

- `schema`: string;
- `name`: string;
- `profile`: string;
- `environment`: table of existing supported scalar values;
- `mcp.servers`: array of strings;
- `prompts.default`: string;
- `prompts.project`: string;
- `rules.enabled`: array of strings.

Unknown fields are errors where a table has a defined schema.

Legacy files without `schema` are interpreted as `v1` with a
`missing_schema` deprecation warning. Strict validation treats warnings
as failures. Explicit unsupported schema versions are always errors.

## Validation flow

Validation applies independently to:

1. `global.toml`;
2. the current project overlay;
3. the resolved post-merge configuration.

The centralized validation layer is reused by:

- `ai-dev validate [--strict] [--json]`;
- `ai-dev doctor`;
- `ai-dev config`;
- `ai-dev env`;
- future configuration consumers.

Validation findings contain source, field path, stable code, severity,
and a value-safe message. Findings are sorted by source, path, code, and
message. Source order records global configuration before the project
overlay.

## Required stable codes

- `missing_schema`
- `unsupported_schema`
- `unknown_field`
- `invalid_type`
- `invalid_value`
- `deprecated_field`
- `conflicting_value`
- `invalid_environment_name`
- `invalid_environment_value`

## CLI exit status

- `0`: valid configuration;
- `1`: validation errors, or warnings in strict mode;
- `2`: invalid command usage.

## Acceptance criteria

- `schema = "v1"` is recognized.
- Unsupported schema versions are rejected.
- Missing schema warns normally and fails in strict mode.
- Unknown fields and invalid field types are detected.
- Invalid array element types are detected.
- Global and project sources are validated independently.
- The merged configuration is validated.
- Human, strict, and deterministic JSON output work.
- Findings include source, path, code, severity, and message.
- `doctor` classifies missing, valid, deprecated, unsupported,
  conflicting, and invalid configuration.
- `config` and `env` validate before producing resolved output.
- `env` and direnv emit no partial exports after validation failure.
- Existing commands remain compatible for valid configurations.
- Tests use isolated temporary directories.
- `go test ./...` and `go vet ./...` pass.
- A static temporary binary builds successfully.
- Documentation covers schema use, migration, validation, examples, and
  rollback.

## Out of scope

- Automatic migration
- Secret providers or secret resolution
- MCP execution or client generation
- Prompt or rule file loading
- Skills, profile resolution beyond scalar validation, machine overlays,
  plugins, synchronization, packaging
- Dynamic or network-based schemas
