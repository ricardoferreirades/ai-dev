# ai-dev

`ai-dev` is a user-level AI development environment manager for macOS and
Linux.

It keeps configuration outside individual Git repositories and worktrees
so that developers can consistently access environment variables, MCP
servers, prompts, rules, skills, and agent configuration from any project.

## Runtime locations

- Configuration: `~/.config/ai-dev`
- Installed binary: `~/.local/bin/ai-dev`
- Data: `~/.local/share/ai-dev`
- State: `~/.local/state/ai-dev`

This Git repository contains only the source code; none of the locations
above are tracked here.

## Building

```sh
CGO_ENABLED=0 go build -trimpath -o ./bin/ai-dev .
```

## Commands

```text
ai-dev info
ai-dev project-id
ai-dev root
ai-dev [--machine <id>] [--profile <id>]... [--profile-only <id>]... config [--json | --compact]
ai-dev [--machine <id>] [--profile <id>]... [--profile-only <id>]... config sources [--json]
ai-dev [--machine <id>] [--profile <id>]... [--profile-only <id>]... config origin <field.path> [--json]
ai-dev [--plugin-path <path>]... plugin list [--json]
ai-dev [--plugin-path <path>]... plugin show <plugin-id> [--json] [--handshake]
ai-dev [--plugin-path <path>]... plugin validate [<plugin-id>] [--json]
ai-dev [--plugin-path <path>]... plugin status [--json]
ai-dev [--plugin-path <path>]... plugin refresh [--json]
ai-dev [--plugin-path <path>]... plugin run <plugin-id> <operation> [--capability <name>] [--input <path>] [--json]
ai-dev export [--output <path>] [--project] [--global] [--include-machine] [--include-plugins] [--profiles] [--prompts] [--rules] [--config] [--plugins]
ai-dev import <bundle> [--dry-run] [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev bundle verify <bundle>
ai-dev bundle show <bundle> [--json]
ai-dev bundle list [directory] [--json]
ai-dev bundle metadata <bundle> [--json]
ai-dev bundle diff <bundle> [--json]
ai-dev sync preview <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev sync <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev env [--shell sh]
ai-dev validate [--strict] [--json] [--bundle <path>]
ai-dev secret resolve <reference>
ai-dev secret check [--json]
ai-dev mcp list [--enabled] [--json]
ai-dev mcp show <server-name> [--json]
ai-dev mcp resolve [--include-disabled] [--resolve-secrets]
ai-dev mcp check [--json]
ai-dev client list [--json]
ai-dev client show <client> [--json]
ai-dev client path <client> [--scope <scope>] [--json]
ai-dev client validate <client> [--scope <scope>] [--format <format>] [--strict] [--json]
ai-dev client generate <client> [--json] [--format <format>] [--scope <scope>] [--include-disabled] [--resolve-secrets] [--with-metadata] [--strict] [--output <path>] [--force]
ai-dev client compare [--json]
ai-dev prompt list [--json]
ai-dev prompt show <identifier> [--json]
ai-dev prompt search <query> [--json]
ai-dev prompt resolve [--json]
ai-dev prompt info [--json]
ai-dev rule list [--json]
ai-dev rule show <identifier> [--json]
ai-dev rule search <query> [--json]
ai-dev rule resolve [--json]
ai-dev rule info [--json]
ai-dev profile list [--json]
ai-dev profile show <identifier> [--json]
ai-dev profile active [--json]
ai-dev profile resolve [--with-project] [--json]
ai-dev machine show [--json]
ai-dev context [--json]
ai-dev config-path
ai-dev doctor
ai-dev version
```

- `info` — print resolved project and Git information.
- `project-id` — print the stable project identifier.
- `root` — print the current project root.
- `config` — print the resolved global and project configuration.
- `config sources` — print ordered configuration sources and precedence.
- `config origin` — show source contributions for a specific field path.
- `env` — print shell-safe `export` statements for the resolved
  `[environment]` table, resolving `secret://` references first.
- `validate` — validate the current global, project, and resolved
  configuration context.
- `secret` — resolve and inspect secret references safely.
- `mcp` — inspect, resolve, and validate the MCP server registry.
- `client` — inspect adapter capabilities and generate client-specific MCP configuration.
- `prompt` — discover, inspect, search, and resolve prompt registry resources.
- `rule` — discover, inspect, search, and resolve rule registry resources.
- `profile` — inspect available, active, and resolved profile overlays.
- `machine` — inspect machine identity normalization and overlay path.
- `context` — print runtime context, active profiles, and merge order.
- `plugin` — discover, validate, inspect, and invoke external plugins.
- `export` — package configuration into a portable `.aidev` bundle.
- `import` — validate and import a bundle with conflict policies.
- `bundle` — verify, inspect, list, and diff bundles.
- `sync` — preview or apply bundle synchronization using import policies.
- `config-path` — print the expected project configuration path.
- `doctor` — check commands, directories, and configuration files.
- `version` — print the ai-dev version.

## Configuration

Configuration is TOML-based and layered:

1. `~/.config/ai-dev/global.toml` — applies to every project.
2. `~/.config/ai-dev/profiles/<profile>.toml` — optional profile overlays.
3. `~/.config/ai-dev/machines/<machine-id>.toml` — optional machine overlay.
4. `~/.config/ai-dev/projects/<project-id>.toml` — project overlay.

Run `ai-dev config-path` from inside a project to find its overlay path,
and `ai-dev config` to see the fully resolved configuration.

### Schema and validation

Schema `v1` is the current configuration contract:

```toml
schema = "v1"
name = "Ricardo"
profiles = ["default", "team-shared"]

[machine]
id = "dev-workstation"

[environment]
EDITOR = "vim"

[mcp.servers.github]
transport = "stdio"
command = "github-mcp-server"
args = ["stdio"]

[mcp.servers.github.environment]
GITHUB_TOKEN = "secret://env/GITHUB_TOKEN"

[mcp.servers.remote]
transport = "http"
url = "https://mcp.example.com"

[mcp.servers.remote.headers]
Authorization = "secret://env/MCP_AUTH_TOKEN"

[prompts]
default = "prompts/default.md"
project = "prompts/project.md"

[rules]
enabled = ["safe-shell"]

[plugins]
paths = ["~/custom/ai-dev/plugins"]

[plugins.onepassword]
enabled = true
timeout_seconds = 10

[plugins.onepassword.config]
account = "my.1password.com"
```

Validate the current global file, project overlay, and merged result:

```sh
ai-dev validate
ai-dev validate --strict
ai-dev validate --json
```

Legacy files without `schema` continue to work in normal mode with a
deprecation warning. Strict mode treats that warning as a validation
failure. See
[`docs/checkpoints/04-schema-validation.md`](docs/checkpoints/04-schema-validation.md)
for the complete schema, diagnostics, migration, and rollback reference.

### Secret references

`ai-dev` supports secret references in `[environment]` values. The
syntax is `secret://<provider>/<reference>`.

Supported providers in this checkpoint:

- `env` — read the secret from the current process environment.
- `command` — execute a configured command definition and read its
  standard output.

See [`docs/checkpoints/05-secrets.md`](docs/checkpoints/05-secrets.md)
for syntax, command definitions, inspection commands, security
guarantees, and rollback steps.

See [`docs/checkpoints/06-mcp-registry.md`](docs/checkpoints/06-mcp-registry.md)
for full MCP schema, merge behavior, validation rules, JSON contracts,
and rollback guidance.

See [`docs/checkpoints/07-ai-client-adapters.md`](docs/checkpoints/07-ai-client-adapters.md)
for the client adapter architecture, capability matrix, format/scope behavior,
validation and comparison commands, secret handling, output file safety, and rollback guidance.

See [`docs/checkpoints/08-prompt-rule-registry.md`](docs/checkpoints/08-prompt-rule-registry.md)
for prompt/rule registry layout, metadata validation, namespacing, composition,
search and resolve behavior, deterministic ordering, and rollback guidance.

See [`docs/checkpoints/09-profiles-machine-overlays.md`](docs/checkpoints/09-profiles-machine-overlays.md)
for profile and machine overlay precedence, runtime override flags, source
provenance commands, and compatibility guidance.

See [`docs/checkpoints/10-plugin-architecture.md`](docs/checkpoints/10-plugin-architecture.md)
for plugin manifest schema, discovery precedence, protocol lifecycle,
capability integrations, diagnostics, and rollback guidance.

See [`docs/checkpoints/11-configuration-distribution-sync.md`](docs/checkpoints/11-configuration-distribution-sync.md)
for bundle format schema, export/import commands, conflict policies,
checksum verification, synchronization preview, and atomic import behavior.

## Environment activation

`ai-dev env --shell sh` prints `export KEY='value'` statements resolved
from the project's validated `[environment]` configuration table. Plain
values and secret references are resolved before output is emitted. The
command emits no exports when validation or secret resolution fails.
Valid exports can be activated manually:

```sh
eval "$(ai-dev env --shell sh)"
```

### Automatic activation with direnv

Checkpoint 3B adds automatic activation and unloading through
[direnv](https://direnv.net/), without requiring a `.envrc` file inside
every individual repository. See
[`docs/checkpoints/03b-direnv.md`](docs/checkpoints/03b-direnv.md) for the
full design, and `shell/direnv/ai-dev.sh` for the reusable helper.

Quick start:

```sh
mkdir -p ~/.config/direnv/lib
cp shell/direnv/ai-dev.sh ~/.config/direnv/lib/ai-dev.sh
```

Then configure the shell after direnv's standard hook:

```sh
# ~/.bashrc
eval "$(direnv hook bash)"
. "$HOME/.config/direnv/lib/ai-dev.sh"
use_ai_dev_hook bash

# Or, in ~/.zshrc:
eval "$(direnv hook zsh)"
. "$HOME/.config/direnv/lib/ai-dev.sh"
use_ai_dev_hook zsh
```

Finally, create and allow one `.envrc` in the common parent:

```sh
mkdir -p ~/code
printf '%s\n' 'use_ai_dev' >~/code/.envrc
direnv allow ~/code
```

The companion shell hook ensures moving between sibling projects beneath
the same parent `.envrc` updates the resolved project overlay.

## Development

```sh
gofmt -l .
go vet ./...
go test ./...
```

See `AGENTS.md` for detailed contribution rules and `CURRENT_TASK.md` for
the checkpoint currently in progress.
