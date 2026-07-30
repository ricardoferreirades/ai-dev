# Checkpoint 3B: automatic activation with direnv

## Goal

Activate, update, and unload the environment resolved by `ai-dev env`
automatically as a shell moves between projects, without adding `.envrc`
files to individual repositories or worktrees.

## Design

The reusable helper at `shell/direnv/ai-dev.sh` has two responsibilities:

1. `use_ai_dev` runs `ai-dev env --shell sh` for the active project
   and evaluates only its standard output.
2. `use_ai_dev_hook` augments direnv's bash or zsh hook so moving
   between sibling projects beneath the same parent `.envrc` unloads the
   old environment and reevaluates the parent `.envrc` for the new
   directory.

The companion hook is necessary because standard direnv keeps an already
loaded parent `.envrc` active when moving between its child directories;
it does not reevaluate that unchanged file solely because the working
directory changed.

The helper distinguishes two error cases:

- If `ai-dev` is not installed, activation prints a warning and returns
  successfully.
- If `ai-dev env` fails, including because configuration contains invalid
  TOML, activation prints a clear error, returns nonzero, and evaluates no
  command output.

Once an `.envrc` evaluates successfully, direnv's normal environment diff
restores the previous values when the shell leaves the managed directory.

## Installation

Install direnv and ensure `ai-dev` is on `PATH`. Copy the reusable helper
to direnv's user library:

```sh
mkdir -p ~/.config/direnv/lib
cp shell/direnv/ai-dev.sh ~/.config/direnv/lib/ai-dev.sh
```

Configure the interactive shell after direnv's standard hook.

For bash, add this to `~/.bashrc`:

```sh
eval "$(direnv hook bash)"
. "$HOME/.config/direnv/lib/ai-dev.sh"
use_ai_dev_hook bash
```

For zsh, add this to `~/.zshrc`:

```sh
eval "$(direnv hook zsh)"
. "$HOME/.config/direnv/lib/ai-dev.sh"
use_ai_dev_hook zsh
```

Create one `.envrc` in the common parent of the managed projects:

```sh
mkdir -p ~/code
printf '%s\n' 'use_ai_dev' >~/code/.envrc
direnv allow ~/code
```

Repositories and worktrees beneath `~/code` now share that `.envrc`.
No file is added to an individual repository.

`AI_DEV_BIN` may be set to an absolute binary path when `ai-dev` is not on
`PATH`. `DIRENV_BIN` provides the equivalent override for direnv.

## Testing

Run the complete repository checks:

```sh
gofmt -w main.go direnv_shell_test.go
go test ./...
go vet ./...

temporary_binary="$(mktemp "${TMPDIR:-/tmp}/ai-dev.XXXXXX")"
CGO_ENABLED=0 go build -trimpath -o "$temporary_binary" .
"$temporary_binary" version
rm -f "$temporary_binary"
```

`go test ./...` builds its own temporary `ai-dev` binary and runs
`shell/direnv/ai-dev_test.sh`. The suite always checks the helper under
bash and also checks zsh when it is installed. When direnv is installed,
it runs end-to-end scenarios covering:

- global and project environment activation;
- project values overriding global values;
- movement between two configured projects;
- automatic unloading outside the managed directory;
- main-checkout and worktree identity;
- invalid TOML failure behavior;
- deterministic compatibility of `ai-dev env --shell sh`.

To require both optional tools rather than skip their scenarios:

```sh
AI_DEV_TEST_REQUIRE_DIRENV=1 AI_DEV_TEST_REQUIRE_ZSH=1 go test ./...
```

The test suite uses temporary directories and `AI_DEV_CONFIG_HOME`; it
does not read or modify the user's real ai-dev configuration.

## Rollback

1. Remove `use_ai_dev_hook bash` or `use_ai_dev_hook zsh` and the
   adjacent helper source line from the shell startup file. Keep the
   standard `eval "$(direnv hook ...)"` line if direnv is still wanted for
   other projects.
2. Remove `use_ai_dev` from the common parent `.envrc`. Delete the
   `.envrc` only if it contains no other configuration.
3. Remove `~/.config/direnv/lib/ai-dev.sh`.
4. Start a new shell or leave the managed directory so direnv unloads the
   previously exported values.

## Security notes

- Only successful standard output from the trusted
  `ai-dev env --shell sh` command is evaluated.
- Standard error is never evaluated.
- `ai-dev env` emits deterministic, POSIX-shell-quoted exports.
- Failed configuration resolution cannot partially activate an
  environment.

## Out of scope

Checkpoint 3B does not add secret resolution, MCP support, IDE adapters,
prompt registries, or automatic file generation inside repositories.
