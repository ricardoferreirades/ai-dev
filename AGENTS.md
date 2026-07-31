# ai-dev Agent Instructions

## Project purpose

`ai-dev` is a user-level AI development environment manager for macOS and Linux.

It keeps configuration outside individual Git repositories and worktrees so that
developers can consistently access environment variables, MCP servers, prompts,
rules, skills, and agent configuration from any project.

The runtime configuration lives outside this repository:

- Configuration: `~/.config/ai-dev`
- Installed binary: `~/.local/bin/ai-dev`
- Data: `~/.local/share/ai-dev`
- State: `~/.local/state/ai-dev`

This Git repository contains the source code only.

## Current implementation

The CLI is written in Go.

Completed checkpoints:

### Checkpoint 1

- Stable project identification.
- Git repository detection.
- Remote URL normalization.
- Git worktree compatibility.
- Commands:
  - `ai-dev info`
  - `ai-dev project-id`
  - `ai-dev root`
  - `ai-dev doctor`
  - `ai-dev version`

### Checkpoint 2

- TOML configuration support.
- Global configuration from `~/.config/ai-dev/global.toml`.
- Project overlays from `~/.config/ai-dev/projects/<project-id>.toml`.
- Recursive table merging.
- Ordered, de-duplicated array merging.
- Commands:
  - `ai-dev config`
  - `ai-dev config --json`
  - `ai-dev config --compact`
  - `ai-dev config-path`

### Checkpoint 3A

- Environment export from the resolved `[environment]` table.
- POSIX-shell-safe quoting.
- Support for string, boolean, integer, and float values.
- Rejection of invalid environment variable names.
- Rejection of arrays and nested tables as environment values.
- Commands:
  - `ai-dev env`
  - `ai-dev env --shell sh`
- Shell helper:
  - `ai-activate`

### Checkpoint 3B

- Automatic activation and unloading through `direnv`.
- Shared parent-directory `.envrc` support for repositories and worktrees.
- Reusable helper:
  - `shell/direnv/ai-dev.sh`

### Checkpoint 4

- Versioned configuration schema with explicit `v1` dispatch.
- Independent global and project source validation.
- Resolved post-merge validation.
- Stable, deterministic validation findings.
- Normal, strict, and JSON validation modes.
- Validation integration with `doctor`, `config`, `env`, and direnv.
- Command:
  - `ai-dev validate`
  - `ai-dev validate --strict`
  - `ai-dev validate --json`

### Checkpoint 5

- Secret references with runtime provider abstraction.
- Supported providers:
  - `env`
  - `command`
- Secret inspection and direct resolution commands.
- Validation and doctor integration for secret references.
- Commands:
  - `ai-dev secret resolve <reference>`
  - `ai-dev secret check`
  - `ai-dev secret check --json`

Current version:

- `0.14.9`

## Current task

Checkpoint 6 is complete. Read `CURRENT_TASK.md`,
`docs/checkpoints/05-secrets.md`, and
`docs/checkpoints/06-mcp-registry.md` before changing MCP registry or
secret-resolution behavior.

## Development rules

- Make one small, testable change at a time.
- Do not implement future checkpoints unless explicitly requested.
- Preserve compatibility with macOS and Linux.
- Prefer standard-library Go where practical.
- Format Go code with `gofmt`.
- Run `go test ./...`.
- Run `go vet ./...`.
- Build with `CGO_ENABLED=0`.
- Never modify the user's real files under `~/.config/ai-dev` during automated
  tests.
- Tests must use temporary directories and environment-variable overrides.
- Do not store credentials, API keys, tokens, or secrets in this repository.
- Do not silently change established configuration merge semantics.
- Do not remove an existing command without explicit approval.
- Keep shell output deterministic.
- Treat generated shell code as security-sensitive.

## Build command

```bash
CGO_ENABLED=0 go build -trimpath -o ./bin/ai-dev .
```
