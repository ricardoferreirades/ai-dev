# Checkpoint 11: Configuration Distribution and Synchronization

## Objective

Add deterministic local bundle export/import/sync for `ai-dev`
configuration without introducing cloud synchronization or centralized
infrastructure.

## Bundle format

- Extension: `.aidev`
- Container: ZIP archive with transparent compression
- Schema: `bundle-v1`

Archive layout:

- `manifest.json`
- `resources/<resource-path>`

## Manifest fields

`manifest.json` includes:

- `schema`
- `bundle_version`
- `created_at`
- `creator_version`
- `origin_platform`
- `project_identifier` (when available)
- `resources` (type/path/size/checksum/provenance)
- `checksums` (path -> SHA-256)

## Included resources

Bundled resources may include:

- global config
- project config
- profiles
- prompts
- rules
- machine overlays (opt-in)
- plugin manifests (opt-in)

Never bundled:

- resolved secrets
- provider outputs
- caches
- logs
- temporary files
- plugin executables

## Commands

Export:

```bash
ai-dev export [--output <path>] [--project] [--global] [--include-machine] [--include-plugins] [--profiles] [--prompts] [--rules] [--config] [--plugins]
```

Import:

```bash
ai-dev import <bundle> [--dry-run] [--overwrite | --skip-existing | --fail-on-conflict] [--json]
```

Bundle inspection and verification:

```bash
ai-dev bundle verify <bundle>
ai-dev bundle show <bundle> [--json]
ai-dev bundle metadata <bundle> [--json]
ai-dev bundle list [directory] [--json]
ai-dev bundle diff <bundle> [--json]
```

Synchronization:

```bash
ai-dev sync preview <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev sync <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
```

Validation integration:

```bash
ai-dev validate --bundle <bundle> [--json]
```

## Conflict policies

Exactly one policy is active:

- default: `--fail-on-conflict`
- `--overwrite`
- `--skip-existing`

## Atomic import

Import writes are transactional at command scope:

- all selected resources are validated before changes
- failed imports restore previous file content
- dry-run never modifies files

## Provenance

Successful imports write provenance metadata to state storage:

- `bundle-provenance.json`
- `bundle-last-status.json`

Doctor reports bundle support and last bundle validation/import status.

## Stable error codes

- `bundle_not_found`
- `invalid_bundle`
- `unsupported_bundle_schema`
- `bundle_checksum_failed`
- `bundle_manifest_invalid`
- `bundle_import_failed`
- `bundle_export_failed`
- `bundle_conflict`
- `bundle_resource_invalid`
- `bundle_resource_duplicate`
- `bundle_sync_failed`

## Security and compatibility

- Secret references are preserved as references only.
- Existing non-bundle workflows (checkpoints 1-10) remain unchanged.
- Plugin manifests may be included, executables are excluded.
- Machine overlays are excluded unless `--include-machine` is set.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
