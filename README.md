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
ai-dev config [--json | --compact]
ai-dev env [--shell sh]
ai-dev validate [--strict] [--json]
ai-dev secret resolve <reference>
ai-dev secret check [--json]
ai-dev mcp list [--enabled] [--json]
ai-dev mcp show <server-name> [--json]
ai-dev mcp resolve [--include-disabled] [--resolve-secrets]
ai-dev mcp check [--json]
ai-dev config-path
ai-dev doctor
ai-dev version
```

- `info` — print resolved project and Git information.
- `project-id` — print the stable project identifier.
- `root` — print the current project root.
- `config` — print the resolved global and project configuration.
- `env` — print shell-safe `export` statements for the resolved
  `[environment]` table, resolving `secret://` references first.
- `validate` — validate the current global, project, and resolved
  configuration context.
- `secret` — resolve and inspect secret references safely.
- `mcp` — inspect, resolve, and validate the MCP server registry.
- `config-path` — print the expected project configuration path.
- `doctor` — check commands, directories, and configuration files.
- `version` — print the ai-dev version.

## Configuration

Configuration is TOML-based and layered:

1. `~/.config/ai-dev/global.toml` — applies to every project.
2. `~/.config/ai-dev/projects/<project-id>.toml` — overlays specific to
   one project, merged recursively over the global configuration (arrays
   are merged in order and de-duplicated).

Run `ai-dev config-path` from inside a project to find its overlay path,
and `ai-dev config` to see the fully resolved configuration.

### Schema and validation

Schema `v1` is the current configuration contract:

```toml
schema = "v1"
name = "Ricardo"
profile = "default"

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
