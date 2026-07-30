# Checkpoint 13: Policy Engine and Compliance Framework

Checkpoint 13 introduces a centralized, read-only policy engine for `ai-dev`.

## Registry and schema

- Registry root: `~/.config/ai-dev/policies`
- Recursive discovery: nested directories are supported
- Schema: `policy-v1`
- Policy files: TOML (`.toml`)

Unsupported schemas and duplicate policy identifiers fail policy validation.

## Policy model

Each policy contains metadata and logic:

- `id`, `title`, `description`, `version`, `author`, `tags`
- `severity`: `info|warning|error|critical`
- `enabled`: boolean (default `true`)
- `enforcement`: `disabled|advisory|enforced`
- `scopes`: one or more of `global|project|profile|machine|bundle|client`
- `condition`: declarative expression tree

## Operators

Supported condition operators:

- `equals`
- `not_equals`
- `exists`
- `missing`
- `contains`
- `not_contains`
- `greater_than`
- `less_than`
- `regex_match`
- logical `and`, `or`, `not`

## Outcomes and reporting

Each policy evaluation yields exactly one outcome:

- `pass`
- `warn`
- `fail`
- `skip`

Each finding includes:

- policy identifier
- severity
- outcome
- configuration path
- provenance
- stable code
- readable message

Compliance summary includes:

- passed
- warned
- failed
- skipped
- compliance percentage

## Commands

```sh
ai-dev policy list [--json]
ai-dev policy show <policy-id> [--json]
ai-dev policy explain <policy-id>
ai-dev policy evaluate [policy-id] [--json] [--policy-mode disabled|advisory|enforced]
ai-dev policy report [--json]
```

## Policy mode

Supported modes:

- `disabled`
- `advisory`
- `enforced`

Configuration default:

```toml
[policy]
mode = "enforced"
```

Per-command override:

```sh
--policy-mode disabled|advisory|enforced
```

## Overrides and precedence

Per-policy enablement/enforcement overrides:

```toml
[policies.require-mcp]
enabled = true
enforcement = "advisory"
```

Override precedence:

1. global registry defaults
2. profile overrides
3. machine overrides
4. project overrides

Only `enabled` and `enforcement` are overridable. Policy logic is not overridden.

## Integrated operations

Policy checks are integrated before output for:

- `validate`
- `doctor`
- `export`
- `import`
- `bundle verify`
- `bundle decrypt`
- `client generate`
- `client validate`
- `env`
- `mcp resolve`

## Bundle integration

Policy files under `~/.config/ai-dev/policies` are included in bundle exports and
follow normal bundle conflict semantics on import.

Checkpoint 13 remains compatible with signed/encrypted bundles from Checkpoint 12.

## Security behavior

Policy evaluation is read-only and deterministic for identical input.

Policy diagnostics do not expose:

- resolved secret values
- private keys
- decrypted bundle contents
- secret-provider outputs

## Stable policy error codes

- `policy_not_found`
- `duplicate_policy`
- `invalid_policy`
- `unsupported_policy_schema`
- `invalid_policy_identifier`
- `policy_evaluation_failed`
- `policy_condition_invalid`
- `policy_scope_invalid`
- `policy_enforcement_failed`
- `policy_override_invalid`
- `policy_conflict`
- `policy_operation_blocked`
- `policy_plugin_failed`

## Rollback

To disable policy enforcement while preserving diagnostics:

```sh
ai-dev <command> --policy-mode advisory
```

To bypass policy evaluation temporarily:

```sh
ai-dev <command> --policy-mode disabled
```

To remove all custom policies, move files out of:

- `~/.config/ai-dev/policies`

No configuration mutation is performed by policy evaluation itself.
