# Current Task: Checkpoint 10

## Objective

Implement a controlled plugin architecture for `ai-dev` with external
process execution, manifest-driven discovery, versioned NDJSON protocol,
capability registration, validation, diagnostics, and command surfaces.

## Scope

- Add plugin discovery and manifest validation from ordered search paths.
- Add plugin command surface:
  - `plugin list`
  - `plugin show`
  - `plugin validate`
  - `plugin status`
  - `plugin refresh`
  - `plugin run`
- Add handshake and runtime capability discovery protocol (`ai-dev-plugin-v1`).
- Enforce process controls:
  - finite timeout
  - bounded stdout/stderr
  - bounded message count and size
  - direct executable invocation (no shell wrapper)
  - minimal default plugin environment
- Integrate plugin capability surfaces:
  - secret-provider registration for `secret://<provider>/...`
  - prompt-provider and rule-provider registry resources
  - validator findings in core validation output
- Add plugin provenance in `config sources` and plugin checks in `doctor`.

## Required behavior

- Invalid, incompatible, or conflicting plugins are visible but fail closed.
- Plugin identifier, manifest schema, protocol, executable, and capability
  checks emit stable diagnostics.
- Discovery is deterministic by search path then filesystem ordering.
- Existing non-plugin workflows remain compatible.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
