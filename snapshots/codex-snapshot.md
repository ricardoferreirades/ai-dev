For Codex, the user-level directory is `~/.codex` by default. Its location can be changed with `CODEX_HOME`.

A practical documented snapshot is:

```text
~/.codex/
├── agents/
│   └── <agent-name>.toml       # Personal custom agent definitions
├── prompts/
│   └── <prompt-name>.md        # Reusable custom prompts
├── sessions/                   # Local session transcripts and state
├── logs/                       # Runtime logs
├── state/                      # Feature/plugin state
├── AGENTS.md                   # Personal instructions for every project
├── auth.json                   # Credentials when file storage is used
├── config.toml                 # Main user-level configuration
├── hooks.json                  # User-level lifecycle hooks
├── history.jsonl               # Local conversation history, when enabled
└── <profile>.config.toml       # Optional named configuration profiles
```

Some entries are created only when their corresponding feature is used. Files like `auth.json`, session data, and history may contain sensitive information and generally shouldn’t be committed or shared.

Unlike GitHub Copilot, current Codex skills are normally stored outside `~/.codex`:

```text
~/.agents/
├── skills/
│   └── <skill-name>/
│       ├── SKILL.md
│       ├── agents/
│       │   └── openai.yaml     # Optional UI/tool metadata
│       ├── references/         # Optional supporting documentation
│       ├── scripts/            # Optional helper programs
│       └── assets/             # Optional reusable resources
└── plugins/
    └── marketplace.json        # Optional personal plugin marketplace
```

Repository-level Codex structure:

```text
project/
├── AGENTS.md                   # Repository instructions
├── .codex/
│   ├── config.toml             # Trusted-project configuration
│   ├── hooks.json              # Project hooks
│   └── agents/
│       └── <agent-name>.toml   # Project-scoped custom agents
└── .agents/
    ├── skills/
    │   └── <skill-name>/
    │       └── SKILL.md
    └── plugins/
        └── marketplace.json
```

Nested `AGENTS.md`, `.codex/config.toml`, and `.agents/skills/` locations can provide more specific configuration for subdirectories. Project `.codex` configuration is ignored unless the repository is trusted.

Official references: [Codex configuration](https://developers.openai.com/codex/config-basic), [advanced configuration](https://developers.openai.com/codex/config-advanced), [AGENTS.md](https://developers.openai.com/codex/guides/agents-md), and [Codex skills](https://developers.openai.com/codex/skills).
