# Checkpoint 12: Bundle Signing and Encryption

Checkpoint 12 adds local key lifecycle management, explicit trust policy,
and secure bundle workflows to `ai-dev`.

## Threat model

This checkpoint addresses these risks:

- bundle tampering between export and import;
- unauthorized bundle origin claims;
- bundle disclosure at rest or in transit;
- accidental trust escalation from mere key presence;
- unsafe handling of private key material.

This checkpoint does not add cloud KMS, CA trust chains, or remote identity
services. Trust remains explicit and local.

## Security envelope

The first envelope version is `security-v1`.

Supported algorithms:

- signing: `ed25519`
- encryption: `x25519-aes256gcm-v1`

`x25519-aes256gcm-v1` uses recipient public keys to wrap a random content key,
then encrypts bundle payload with AES-256-GCM (authenticated encryption).

Bundle states supported:

- unsigned + unencrypted
- signed + unencrypted
- unsigned + encrypted
- signed + encrypted

## Signing model

Signing uses deterministic canonical input derived from:

- bundle schema and manifest metadata;
- resource checksums;
- bundled resource bytes;
- security metadata (excluding signature bytes).

Any covered modification invalidates verification.

Combined signed+encrypted export is deterministic in operation order:

1. Build plaintext bundle payload.
2. Encrypt payload for selected recipients (if enabled).
3. Sign final bundle representation (ciphertext when encrypted, plaintext when not encrypted).

## Key registry

Default registry root:

- `~/.config/ai-dev/keys`

Registry layout separates:

- signing public/private keys
- encryption public/private keys

Private keys are stored as encrypted records and require passphrase material.
Private key files are written with restrictive permissions.

## Trust registry

Trust records are explicit and local. States:

- `trusted`
- `untrusted`
- `revoked`
- `unknown`

Scopes:

- `global`
- `project`

Project scope overrides global scope for the active project.

## Commands

### Key lifecycle

```sh
ai-dev key generate --purpose signing --id release-key --passphrase-ref secret://env/AI_DEV_KEY_PASS
ai-dev key generate --purpose encryption --id work-laptop --passphrase-ref secret://env/AI_DEV_KEY_PASS
ai-dev key import <path> [--purpose signing|encryption] [--private] [--id <key-id>]
ai-dev key export <key-id> [--private --yes] [--json]
ai-dev key list [--json]
ai-dev key show <key-id> [--json]
ai-dev key remove <key-id> (--public | --private) --yes
```

### Trust management

```sh
ai-dev trust set <key-id> <trusted|untrusted|revoked|unknown> [--scope global|project]
ai-dev trust show <key-id> [--json]
ai-dev trust list [--scope global|project|effective] [--json]
```

### Secure bundle export/verification

```sh
ai-dev export --output release.aidev --sign release-key --key-passphrase-ref secret://env/AI_DEV_KEY_PASS
ai-dev export --output release.aidev --encrypt-for alice --encrypt-for work-laptop
ai-dev export --output release.aidev --sign release-key --encrypt-for work-laptop --key-passphrase-ref secret://env/AI_DEV_KEY_PASS

ai-dev bundle verify release.aidev
ai-dev bundle verify release.aidev --require-trusted-signature
ai-dev bundle verify release.aidev --require-signer release-key

ai-dev bundle verify-signature release.aidev [--json]
ai-dev bundle signatures release.aidev [--json]
ai-dev bundle recipients release.aidev [--json]
```

### Signing/decryption/re-encryption

```sh
ai-dev bundle sign release.aidev --key release-key --passphrase-ref secret://env/AI_DEV_KEY_PASS
ai-dev bundle decrypt release.aidev --key work-laptop --passphrase-ref secret://env/AI_DEV_KEY_PASS --output release-plain.aidev
ai-dev bundle reencrypt release.aidev --remove-recipient old-laptop --add-recipient new-laptop --key work-laptop --passphrase-ref secret://env/AI_DEV_KEY_PASS
```

### Secure import policy

Configuration:

```toml
[bundles.security]
import_policy = "require-trusted"
required_signers = ["release-key"]
```

Invocation overrides can strengthen policy:

```sh
ai-dev import release.aidev --require-signed
ai-dev import release.aidev --require-trusted
ai-dev import release.aidev --require-signer release-key
```

## Revocation and rotation

- Revoked signers never satisfy trusted-signature requirements.
- Old signatures remain cryptographically verifiable but are reported as revoked.
- Trust metadata supports `supersedes` relationships for key-rotation tracking.
- Trust transfer is always explicit; it is never automatic.

## Security diagnostics

`ai-dev doctor` now reports key and trust safety findings, including:

- key registry availability;
- unsafe private-key permissions;
- configured policy and signer revocation findings.

`ai-dev validate` checks `bundles.security` policy schema and signer identifier format.

## Recovery and rollback

If a secure bundle operation fails:

1. Keep the original bundle and key files.
2. Inspect error code and message.
3. Correct key availability, passphrase reference, trust policy, or signer constraints.
4. Re-run export/import/verify.

Atomic file writes are used for key and bundle operations so partial outputs do
not replace existing artifacts.
