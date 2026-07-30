# Current Task: Checkpoint 13

## Objective

Implement a deterministic policy and compliance engine that evaluates
resolved configuration across core ai-dev operations.

## Scope

- Add policy registry under `~/.config/ai-dev/policies` with recursive discovery.
- Add policy schema `policy-v1` validation and duplicate identifier checks.
- Add policy commands:
  - `policy list/show/explain/evaluate/report`
- Add condition engine with deterministic logical and comparison operators.
- Add policy outcomes (`pass/warn/fail/skip`) and severity metadata.
- Add policy modes (`disabled/advisory/enforced`) from CLI and configuration.
- Add override support through `[policies.<id>]` for `enabled` and `enforcement`.
- Integrate policy checks into:
  - `validate`, `doctor`, `export`, `import`,
  - `bundle verify`, `bundle decrypt`,
  - `client generate`, `client validate`,
  - `env`, and `mcp resolve`.
- Add compliance summaries and persisted policy reports.
- Keep policy evaluation read-only (no resolved configuration mutation).

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
