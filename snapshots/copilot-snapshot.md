The documentation clarifies that `.copilot` is normally a user-level GitHub Copilot CLI directory at `~/.copilot`, not inside the project workspace. That explains why the earlier workspace search found nothing.

Expected structure:

```text
~/.copilot/
├── agents/                    # Personal *.agent.md definitions
├── command-history-state/     # CLI command history
├── extensions/                # Personal CLI extensions
├── hooks/                     # User-level hook scripts
├── ide/                       # IDE integration state
├── installed-plugins/         # Installed plugin files
├── instructions/              # Additional *.instructions.md files
├── logs/                      # Session and extension logs
├── mcp-oauth-config/          # MCP OAuth fallback data
├── mcp-secrets/               # MCP secret fallback storage
├── plugin-data/               # Persistent plugin data
├── session-state/             # Session histories and artifacts
├── skills/
│   └── <skill-name>/
│       └── SKILL.md           # Personal skill definition
├── config.json                # Managed app/auth/plugin state
├── copilot-instructions.md    # Global personal instructions
├── lsp-config.json            # LSP server definitions
├── mcp-config.json            # MCP server definitions
├── permissions-config.json    # Saved project permissions
├── session-store.db           # Cross-session SQLite database
└── settings.json              # User-editable CLI settings
```

Not every item exists immediately; Copilot creates many of them only when the corresponding feature is used.

The main user-editable areas are:

* `settings.json`
* `copilot-instructions.md`
* `instructions/`
* `agents/`
* `skills/`
* `hooks/`
* `extensions/`
* `mcp-config.json`
* `lsp-config.json`

Files such as `config.json`, `session-state/`, `session-store.db`, and `installed-plugins/` are generally managed by the CLI.

Repository-level Copilot configuration is different and normally lives under `.github/`, such as `.github/copilot-instructions.md`, `.github/agents/`, and `.github/copilot/settings.json`.

Source: [GitHub Copilot CLI configuration directory](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-config-dir-reference).
