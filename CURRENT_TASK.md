# Current Task: Checkpoint 12

## Objective

Implement local key management, trust policy enforcement, and secure
bundle signing/encryption workflows for `ai-dev`.

## Scope

- Add local key registry under `~/.config/ai-dev/keys` with:
  - signing keys (`ed25519`)
  - recipient encryption keys (`x25519-aes256gcm-v1`)
  - encrypted private-key storage and restrictive permissions
- Add key lifecycle commands:
  - `key generate/import/export/list/show/remove`
- Add explicit trust registry and scope-aware trust commands:
  - `trust set/show/list`
- Extend bundle commands for security operations:
  - `export --sign <key-id>`
  - `export --encrypt-for <key-id>` (repeatable)
  - `bundle sign`
  - `bundle verify-signature`
  - `bundle signatures`
  - `bundle recipients`
  - `bundle decrypt`
  - `bundle reencrypt`
- Add security envelope support:
  - `security-v1`
  - signature verification separate from trust evaluation
  - encrypted payload + recipient metadata handling
- Enforce import trust policy with configuration and CLI overrides.
- Integrate key/trust checks into validation and doctor diagnostics.
- Preserve compatibility with unsigned, unencrypted `bundle-v1` artifacts.

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
