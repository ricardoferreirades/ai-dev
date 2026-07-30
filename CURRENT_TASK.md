# Current Task: Checkpoint 9

## Objective

Implement profile and machine overlay resolution with deterministic
precedence, runtime override flags, provenance introspection commands,
and backward compatibility for existing configuration usage.

## Scope

- Add runtime options parsing before command dispatch:
  - `--machine <identifier>`
  - `--profile <identifier>` (repeatable)
  - `--profile-only <identifier>` (repeatable)
- Introduce centralized context resolver for:
  - source layering
  - profile activation and de-duplication
  - machine normalization and overlay selection
  - source provenance metadata
- Add command surfaces:
  - `profile list/show/active/resolve`
  - `machine show`
  - `context`
  - `config sources`
  - `config origin <field.path>`
- Update doctor diagnostics with profile/machine checks.
- Preserve compatibility for legacy singular `profile` key.

## Required behavior

- Deterministic merge/source ordering across global, profile, machine,
  CLI profile, and project overlays.
- Stable error codes for invalid identifiers, missing profile references,
  invalid profile/machine overlays, and missing origin paths.
- Redaction of sensitive values in provenance/origin outputs.
- Non-profile workflows continue to function unchanged.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
