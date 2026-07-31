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

## Installing

As a Go module, the CLI can also be installed directly with Go:

```sh
go install github.com/ricardoferreirades/ai-dev@latest
```

Go installs the executable into `GOBIN` or `GOPATH/bin`; make sure that
directory is on `PATH`.

macOS and Linux users can install the latest published binary with:

```sh
curl -fsSL https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main/install.sh | sh
```

The installer detects the operating system and CPU architecture, installs
`ai-dev` into the first writable directory already on `PATH`, and otherwise
uses `~/.local/bin` and adds it to the current shell's startup file. Open a
new shell, or run the printed `export PATH=...` command, then verify it with:

```sh
ai-dev version
```

To install a specific release or into a specific directory:

```sh
curl -fsSL https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main/install.sh | AI_DEV_VERSION=0.14.4 sh
curl -fsSL https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main/install.sh | AI_DEV_INSTALL_DIR="$HOME/.local/bin" sh
```

For a source checkout, use `AI_DEV_FROM_SOURCE=1 ./install.sh`; this requires
Go. Release archives are built automatically for macOS and Linux on `amd64`
and `arm64` version tags.

To uninstall the binary while preserving configuration:

```sh
curl -fsSL https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main/uninstall.sh | sh
```

The uninstall script checks the standard Go install locations (`GOBIN` and
`GOPATH/bin`) so older Go-installed copies are removed too. To also remove
ai-dev configuration, data, and state, use the explicit purge option:

```sh
curl -fsSL https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main/uninstall.sh | sh -s -- --purge
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
ai-dev policy list [--json]
ai-dev policy show <policy-id> [--json]
ai-dev policy explain <policy-id>
ai-dev policy evaluate [policy-id] [--json] [--policy-mode disabled|advisory|enforced]
ai-dev policy report [--json]
ai-dev export [--output <path>] [--project] [--global] [--include-machine] [--include-plugins] [--profiles] [--prompts] [--rules] [--config] [--plugins] [--sign <key-id>] [--encrypt-for <key-id>]...
ai-dev import <bundle|repository|directory> [--dry-run] [--force] [--name <name>] [--overwrite | --skip-existing | --fail-on-conflict] [--require-signed | --require-trusted] [--require-signer <key-id>]... [--key <key-id>] [--json]
ai-dev bundle verify <bundle> [--require-trusted-signature] [--require-signer <key-id>]... [--json]
ai-dev bundle show <bundle> [--json] [--decrypt] [--key <key-id>]
ai-dev bundle list [directory] [--json]
ai-dev bundle metadata <bundle> [--json]
ai-dev bundle diff <bundle> [--json]
ai-dev bundle sign <bundle> --key <key-id> [--output <path>]
ai-dev bundle verify-signature <bundle> [--json]
ai-dev bundle signatures <bundle> [--json]
ai-dev bundle recipients <bundle> [--json]
ai-dev bundle decrypt <bundle> [--output <path>] [--key <key-id>] [--json]
ai-dev bundle reencrypt <bundle> [--add-recipient <key-id>]... [--remove-recipient <key-id>]... [--output <path>] [--key <key-id>]
ai-dev sync preview <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev sync <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
ai-dev key generate --purpose <signing|encryption> --id <key-id> [--passphrase-ref <secret://...>]
ai-dev key import <path> [--purpose <signing|encryption>] [--private] [--id <key-id>]
ai-dev key export <key-id> [--private --yes] [--json]
ai-dev key list [--json]
ai-dev key show <key-id> [--json]
ai-dev key remove <key-id> (--public | --private) [--yes]
ai-dev trust set <key-id> <trusted|untrusted|revoked|unknown> [--scope global|project]
ai-dev trust show <key-id> [--json]
ai-dev trust list [--scope global|project|effective] [--json]
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
- `policy` — discover, evaluate, and report compliance policy outcomes.
- `key` — generate, import, export, inspect, and remove local signing/encryption keys.
- `trust` — manage explicit signer trust state (global and project-scoped).
- `export` — package configuration into a portable `.aidev` bundle, optionally signed and encrypted.
- `import` — validate and atomically import `.aidev` bundles, or import rules, prompts,
  instructions, agents, skills, MCP files, and client configuration from a local
  directory or Git repository.
- `bundle` — verify structure/security, inspect, sign, decrypt, re-encrypt, and list security metadata.
- `sync` — preview or apply bundle synchronization using import policies.
- `config-path` — print the expected project configuration path.
- `doctor` — check commands, directories, and configuration files.
- `version` — print the ai-dev version.

### Importing AI development files

Import a local directory or Git repository containing AI development files:

```sh
ai-dev import /path/to/ai-dev-config --name team
ai-dev import https://github.com/example/team-ai-config.git --name team
```

Use `--dry-run` to inspect the files first and `--force` to replace an existing
import. Rules and prompts are copied into the normal ai-dev registries and
enabled automatically. Instructions, agents, skills, MCP definitions, and
client-specific files are preserved under:

```text
~/.config/ai-dev/imports/<name>/
```

Existing `.aidev` bundle imports continue to use the same `ai-dev import`
command and security/conflict options.

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

See [`docs/checkpoints/12-bundle-signing-encryption.md`](docs/checkpoints/12-bundle-signing-encryption.md)
for key lifecycle, trust model, security envelope format, signing and encryption workflows,
verification policy behavior, rotation/revocation guidance, and recovery/rollback procedures.

See [`docs/checkpoints/13-policy-engine-compliance-framework.md`](docs/checkpoints/13-policy-engine-compliance-framework.md)
for policy schema, discovery and precedence, evaluation operators, enforcement modes,
compliance reporting, integration points, and rollback guidance.

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
