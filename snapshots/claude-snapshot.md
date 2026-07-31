For Claude Code, personal configuration lives in `~/.claude`, while shared project configuration lives in the repository’s `.claude/` directory.

User-level structure:

```text
~/.claude/
├── agents/
│   └── <agent-name>.md          # Personal subagent definitions
├── agent-memory/
│   └── <agent-name>/            # Persistent subagent memory
├── commands/
│   └── <command>.md             # Legacy-style custom slash commands
├── output-styles/
│   └── <style>.md               # Custom response styles
├── plugins/                     # Installed plugins and marketplaces
├── projects/
│   └── <project>/
│       ├── memory/              # Automatic project memory
│       └── <session>.jsonl      # Conversation transcripts
├── rules/
│   └── <topic>.md               # Personal topic/path-specific rules
├── skills/
│   └── <skill-name>/
│       ├── SKILL.md             # Reusable prompt or capability
│       ├── scripts/             # Optional helper programs
│       ├── references/          # Optional supporting documentation
│       └── assets/              # Optional resources
├── themes/
│   └── <theme>.json             # Custom terminal themes
├── workflows/
│   └── <workflow>.js            # Dynamic multi-agent workflows
├── CLAUDE.md                    # Global personal instructions
├── keybindings.json             # Custom keyboard shortcuts
├── settings.json                # Global settings, hooks and permissions
├── history.jsonl                # Prompt history
├── stats-cache.json             # Usage and cost aggregates
├── remote-settings.json         # Cached organization settings, if used
└── policy-limits.json           # Cached organization policies, if used
```

Claude Code also creates runtime directories such as:

```text
~/.claude/
├── backups/
├── cache/
├── debug/
├── file-history/                # Pre-edit snapshots/checkpoints
├── image-cache/
├── paste-cache/
├── plans/
├── session-env/
├── sessions/
├── shell-snapshots/
└── tasks/
```

One important file sits beside the directory rather than inside it:

```text
~/.claude.json                   # OAuth, application state, UI state,
                                 # trust records and personal MCP servers
```

Repository-level structure:

```text
project/
├── CLAUDE.md                    # Shared project instructions
├── CLAUDE.local.md              # Private project instructions; gitignore
├── .mcp.json                    # Shared project MCP servers
├── .worktreeinclude             # Gitignored files copied into worktrees
└── .claude/
    ├── agents/
    │   └── <agent-name>.md      # Project subagents
    ├── agent-memory/
    │   └── <agent-name>/        # Persistent agent memory
    ├── commands/
    │   └── <command>.md         # Custom slash commands
    ├── output-styles/
    │   └── <style>.md
    ├── rules/
    │   └── <topic>.md           # Modular project instructions
    ├── skills/
    │   └── <skill-name>/
    │       └── SKILL.md
    ├── workflows/
    │   └── <workflow>.js
    ├── settings.json            # Shared project settings
    └── settings.local.json      # Private overrides; gitignore
```

Most users only need `CLAUDE.md` and `settings.json`; the remaining directories are optional or generated on demand. `CLAUDE_CONFIG_DIR` can replace the default `~/.claude` location.

Transcripts, prompt history, command output, and file contents are stored as plaintext, so `~/.claude` should be treated as sensitive.

Source: [Explore the `.claude` directory — official Claude Code documentation](https://code.claude.com/docs/en/claude-directory).
