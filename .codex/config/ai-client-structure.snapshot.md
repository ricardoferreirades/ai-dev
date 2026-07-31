# AI client structure snapshot

This file is the source of truth for the user-level GitHub Copilot CLI configuration directory and its closest equivalent structure in other AI clients.
When syncing, preserve the closest possible hierarchy and keep user-editable files separate from automatically managed files.

## Version

1

## Scope

This snapshot describes the user-level Copilot CLI configuration directory, normally `~/.copilot`.

Repository-level Copilot configuration is separate and normally lives under `.github/` in the repository.

## User-level Copilot CLI directory

### root

Folder: `~/.copilot`

Files and directories:

- `agents/` — personal custom agent definitions
- `command-history-state/` — CLI command history
- `config.json` — automatically managed application state such as authentication, installed plugins, and internal data
- `copilot-instructions.md` — personal custom instructions applied to all sessions
- `extensions/` — personal extensions loaded by the CLI
- `hooks/` — user-level hook scripts
- `ide/` — IDE integration state
- `installed-plugins/` — installed plugin files
- `instructions/` — additional `*.instructions.md` files
- `logs/` — session and extension logs
- `lsp-config.json` — LSP server definitions
- `mcp-config.json` — MCP server definitions
- `mcp-oauth-config/` — MCP OAuth fallback data
- `mcp-secrets/` — MCP secret fallback storage
- `permissions-config.json` — saved project permissions
- `plugin-data/` — persistent plugin data
- `session-state/` — session histories and artifacts
- `session-store.db` — cross-session SQLite database
- `settings.json` — user-editable CLI settings
- `skills/` — personal skill definitions

### skills subtree

Folder: `~/.copilot/skills`

Files and directories:

- `<skill-name>/SKILL.md` — personal skill definition

## User-editable areas

The main user-editable areas are:

- `settings.json`
- `copilot-instructions.md`
- `instructions/`
- `agents/`
- `skills/`
- `hooks/`
- `extensions/`
- `mcp-config.json`
- `lsp-config.json`

## Automatically managed areas

These are generally managed by the CLI:

- `config.json`
- `session-state/`
- `session-store.db`
- `installed-plugins/`

## Repository-level Copilot configuration

Repository-level Copilot configuration is different and normally lives under `.github/`, such as:

- `.github/copilot-instructions.md`
- `.github/agents/`
- `.github/copilot/settings.json`

## Sync rules

- Preserve hierarchy before transforming content.
- Keep single-file-to-single-file mappings when the source and target both support them.
- Keep user-level and repository-level Copilot configuration separate.
- Treat automatically managed files as generated state, not source-of-truth content.
- Update this snapshot first when the structure changes, then sync.
