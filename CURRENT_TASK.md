# Current Task: Checkpoint 3B (fresh implementation)

## Status policy

Checkpoint 3B is **not complete**. Treat any file claiming completion as stale
until all acceptance criteria in this document pass.

## Objective

Implement automatic environment activation and unloading through `direnv`,
while keeping project configuration outside individual Git repositories and
worktrees.

## Installation scope (for this checkpoint)

Deliver only:

- reusable helper source in this repository;
- automated tests where practical;
- documented manual installation commands;
- documented rollback commands.

Do **not** implement an automated installer and do **not** modify files under
the user's home directory during implementation.

## CLI change policy

Preferred architecture is shell-only integration using existing:

```text
ai-dev env --shell sh
```

Do not change the Go CLI unless shell-only integration is proven insufficient.
If CLI changes become necessary, stop and document:

1. Why shell integration is insufficient.
2. Proposed CLI behavior.
3. Compatibility implications.
4. Tests that would be added.

No CLI change may be implemented without explicit approval.

## Nested-directory behavior constraints

The helper must use existing `ai-dev` project resolution behavior as-is:

- Inside a Git repository, nested directories resolve to that repository's
  project ID/overlay.
- Outside a Git repository, nested directories resolve as filesystem projects
  per current behavior.

The helper must not implement custom project-detection logic.

## Compatibility targets

- direnv >= 2.30
- Bash >= 4.0
- zsh >= 5.8
- current Go version supported by this repository

Use POSIX-sh-compatible generated environment syntax.

## Error/diagnostics requirements

- Activation failures must return nonzero status.
- Errors must clearly identify ai-dev environment resolution failure.
- Errors must not print resolved environment values.
- Errors must not print secrets.
- Messages should provide diagnostic context for:
  - missing `ai-dev`;
  - invalid TOML;
  - command failures.
- Tests should assert stable identifying fragments, not full exact strings.

## Desired architecture

Implement direnv stdlib helper:

```text
use_ai_dev
```

Repository source:

```text
shell/direnv/ai-dev.sh
```

Expected installed location:

```text
~/.config/direnv/lib/ai-dev.sh
```

A parent-directory `.envrc` must be able to contain only:

```sh
use_ai_dev
```

The helper must evaluate `ai-dev env --shell sh` output using direnv-supported
environment loading behavior, and must not generate files in child repositories
or worktrees.

## Checkpoint boundary

Implement, test, and document Checkpoint 3B only.

Do not start:

- secret handling;
- MCP integration;
- IDE adapters;
- prompt/rules/skills registries;
- profiles;
- synchronization features.

## Acceptance criteria

- direnv activation works under bash.
- direnv activation works under zsh.
- Global environment values are activated.
- Project environment values are activated.
- Project values override global values.
- Moving to another configured project updates the environment.
- Leaving the managed directory unloads variables.
- A worktree resolves the same project overlay as its main checkout.
- Invalid TOML causes activation to fail clearly.
- Existing `ai-dev env` behavior remains unchanged.
- `go test ./...` passes.
- `go vet ./...` passes.
- A temporary binary builds successfully.
- Documentation includes install, test, and rollback steps.
