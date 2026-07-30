# Current Task: Checkpoint 8

## Objective

Introduce a centralized prompt and rule registry so `ai-dev` can
discover, validate, resolve, compose, and expose reusable prompt and
rule resources independently of any AI client.

This checkpoint adds client-neutral prompt/rule discovery and
composition only. It must not install or format prompt/rule content for
specific AI clients.

## Registry model

Supported resource kinds:

- `prompts`
- `rules`

Registry files are discovered recursively from configurable directories.
Default directories:

- `~/.config/ai-dev/prompts`
- `~/.config/ai-dev/rules`

Registry file formats:

- `.md`
- `.txt`

Identifiers are derived from relative paths and namespaces:

`backend/reviewer.md` -> `backend/reviewer`

Optional YAML front matter metadata fields:

- `title`
- `description`
- `version`
- `author`
- `tags`

## Commands

- `ai-dev prompt list [--json]`
- `ai-dev prompt show <identifier> [--json]`
- `ai-dev prompt search <query> [--json]`
- `ai-dev prompt resolve [--json]`
- `ai-dev prompt info [--json]`
- `ai-dev rule list [--json]`
- `ai-dev rule show <identifier> [--json]`
- `ai-dev rule search <query> [--json]`
- `ai-dev rule resolve [--json]`
- `ai-dev rule info [--json]`

## Safety and validation

- Validation covers duplicate IDs, unsupported formats, malformed/invalid metadata, empty resources, and missing enabled references.
- Prompt/rule resolution is deterministic and client-neutral.
- Duplicate enabled identifiers are deduplicated (first occurrence wins).
- Content is preserved exactly except optional trailing newline normalization in composition.
- Doctor reports prompt/rule registry paths, counts, duplicate identifiers, invalid metadata, and missing references.

## Required stable codes

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

## CLI exit status

- `0`: success
- `1`: validation, lookup, or resolution failure
- `2`: invalid command usage

## Verification

- `go test ./...`
- `go vet ./...`
- `CGO_ENABLED=0 go build -trimpath -o <temporary-binary> .`
