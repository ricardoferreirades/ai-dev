# Checkpoint 9: Profiles and Machine Overlays

## Objective

Add profile-based overlays and machine-specific overlays with deterministic
resolution order and provenance reporting, while preserving compatibility
for existing global/project configuration usage.

## Runtime options

Global runtime overrides (must appear before the command):

- `--machine <identifier>`
- `--profile <identifier>` (repeatable)
- `--profile-only <identifier>` (repeatable; disables configured profile refs)

## Resolution order

When resolving command context, overlays are applied in this order:

1. global (`~/.config/ai-dev/global.toml`)
2. profiles referenced by global config
3. machine overlay (`~/.config/ai-dev/machines/<normalized-machine>.toml`)
4. command-line profile overrides (`--profile` / `--profile-only`)
5. project overlay (`~/.config/ai-dev/projects/<project-id>.toml`)
6. profiles referenced by project config

`--profile-only` disables profile references declared in global/project files,
but still applies the global and project base documents.

## Profile and machine model

### Profiles

- Profile files live in `~/.config/ai-dev/profiles/*.toml`.
- Identifier validation: `^[A-Za-z0-9][A-Za-z0-9_-]*$`.
- Profiles cannot recursively reference other profiles.
- Profiles cannot define runtime metadata fields such as `machine`,
  `project_id`, `project_root`, `repository`, `config_home`, `data_home`,
  `state_home`.

### Machine overlays

- Machine id sources (highest precedence first):
  1. `--machine`
  2. `AI_DEV_MACHINE`
  3. `[machine].id` from resolved config
  4. hostname fallback
- Normalization: lowercase, non-alphanumeric collapsed to `-`,
  repeated dashes collapsed, edge dashes trimmed.
- Machine overlays live in `~/.config/ai-dev/machines/<id>.toml`.
- Machine overlays cannot define `machine`, `profiles`, or runtime metadata
  fields listed above.

## New commands

- `ai-dev profile list [--json]`
- `ai-dev profile show <identifier> [--json]`
- `ai-dev profile active [--json]`
- `ai-dev profile resolve [--with-project] [--json]`
- `ai-dev machine show [--json]`
- `ai-dev context [--json]`
- `ai-dev config sources [--json]`
- `ai-dev config origin <field.path> [--json]`

## Validation and diagnostics

Added/used stable error codes:

- `invalid_profile_identifier`
- `profile_not_found`
- `duplicate_profile_reference`
- `invalid_profile`
- `forbidden_profile_field`
- `recursive_profile_reference`
- `invalid_machine_identifier`
- `invalid_machine_overlay`
- `forbidden_machine_field`
- `context_resolution_failed`
- `configuration_origin_not_found`

Doctor output now includes profile and machine diagnostics in addition to
previous MCP/secret/registry checks.

## Compatibility

- Existing configurations without profile/machine usage continue to resolve.
- Legacy singular `profile` is accepted for compatibility and treated as a
  deprecated alias of `profiles`.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
