# Checkpoint 8: Prompt and rule registry

Checkpoint 8 adds centralized, client-neutral registries for reusable prompts
and rules. Resources are discovered, validated, searched, and composed without
any dependency on Codex, Claude, Cursor, VS Code, or other clients.

## Registry paths

Default locations:

- Prompts: `~/.config/ai-dev/prompts`
- Rules: `~/.config/ai-dev/rules`

Override paths in config:

```toml
schema = "v1"

[prompts]
registry = "/absolute/or/relative/path/to/prompts"
enabled = ["backend/reviewer", "security"]

[rules]
registry = "/absolute/or/relative/path/to/rules"
enabled = ["go/reviewer", "security"]
```

## Supported resource formats

- Markdown (`.md`)
- Plain text (`.txt`)

Other file extensions are rejected.

## Identifier and namespace rules

Identifiers are derived from relative file paths (without extension), with `/`
used as namespace separator.

Examples:

- `prompts/backend/reviewer.md` -> `backend/reviewer`
- `rules/go/reviewer.txt` -> `go/reviewer`

Identifier constraints:

- case-sensitive
- allowed characters: letters, digits, `-`, `_`, `/`
- must start with a letter or digit
- must not end with `/`
- must not contain `//`

## Metadata front matter

Resources may include YAML front matter.

Supported metadata fields:

- `title` (string)
- `description` (string)
- `version` (string)
- `author` (string)
- `tags` (array of strings)

Unknown metadata fields produce validation warnings.
Invalid metadata types produce validation errors.

## Discovery and validation

Registry discovery is recursive and deterministic.

Validation detects:

- duplicate identifiers
- unsupported file extensions
- unreadable files
- malformed front matter
- invalid metadata types
- empty content
- missing enabled references

Validation is integrated into `ai-dev validate`.

## Commands

Prompt commands:

```sh
ai-dev prompt list [--json]
ai-dev prompt show <identifier> [--json]
ai-dev prompt search <query> [--json]
ai-dev prompt resolve [--json]
ai-dev prompt info [--json]
```

Rule commands:

```sh
ai-dev rule list [--json]
ai-dev rule show <identifier> [--json]
ai-dev rule search <query> [--json]
ai-dev rule resolve [--json]
ai-dev rule info [--json]
```

## Composition behavior

Enabled identifiers are resolved in this order:

1. global config
2. profile config (when defined)
3. project config

Duplicate enabled identifiers are deduplicated with first occurrence winning.

Composed output is deterministic and client-neutral.

## Content preservation

- Markdown and text content are preserved exactly.
- No reformatting is applied.
- Composition may normalize a missing trailing newline.

## Search behavior

Search matches are case-insensitive against:

- identifier
- title
- description
- tags

## Doctor integration

`ai-dev doctor` now reports prompt/rule registry diagnostics including:

- configured registry paths
- resource counts
- missing references
- duplicate identifiers
- invalid metadata

## Stable checkpoint codes

- `prompt_not_found`
- `rule_not_found`
- `duplicate_prompt`
- `duplicate_rule`
- `invalid_prompt_metadata`
- `invalid_rule_metadata`
- `invalid_prompt_identifier`
- `invalid_rule_identifier`
- `unsupported_prompt_format`
- `unsupported_rule_format`
- `empty_prompt`
- `empty_rule`

## Backward compatibility

When prompt/rule resources are not referenced, missing default registry
folders do not break existing command behavior from earlier checkpoints.

## Rollback

To rollback Checkpoint 8:

1. Revert `registry.go`, `registry_test.go`, and command wiring changes.
2. Revert prompt/rule schema and validation integration updates.
3. Rebuild:

```sh
CGO_ENABLED=0 go build -trimpath -o ./bin/ai-dev .
```

4. Re-run validation suite:

```sh
go test ./...
go vet ./...
```
