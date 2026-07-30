# Current Task: Checkpoint 11

## Objective

Implement deterministic configuration bundle export/import/verification
and local synchronization workflows for `ai-dev`.

## Scope

- Add portable bundle schema `bundle-v1` using `.aidev` archives.
- Add manifest metadata and per-resource checksums.
- Add commands:
  - `export`
  - `import`
  - `bundle verify/show/list/metadata/diff`
  - `sync preview` and `sync`
- Add conflict policy handling:
  - `--overwrite`
  - `--skip-existing`
  - `--fail-on-conflict`
- Add atomic import application and rollback behavior.
- Add dry-run import planning with create/update/conflict/skip reporting.
- Integrate bundle validation through `validate --bundle`.
- Add doctor bundle capability and last-status reporting.
- Preserve security constraints:
  - no resolved secrets in bundles
  - no plugin executables bundled
  - machine overlays opt-in

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
