#!/bin/sh
# ai-dev direnv integration.
#
# Install this file at ~/.config/direnv/lib/ai-dev.sh. direnv sources it
# while evaluating .envrc files, which makes use_ai_dev available:
#
#   # ~/code/.envrc
#   use_ai_dev
#
# A parent .envrc is not reevaluated by stock direnv when the shell moves
# between sibling directories beneath it. To update project overlays on
# those moves, source this same file from the interactive shell after the
# standard direnv hook and install the ai-dev companion hook:
#
#   # bash
#   eval "$(direnv hook bash)"
#   . "$HOME/.config/direnv/lib/ai-dev.sh"
#   use_ai_dev_hook bash
#
#   # zsh
#   eval "$(direnv hook zsh)"
#   . "$HOME/.config/direnv/lib/ai-dev.sh"
#   use_ai_dev_hook zsh

# use_ai_dev exports the environment resolved for the directory that
# caused direnv to evaluate the parent .envrc. When called outside direnv,
# it resolves the caller's current directory.
use_ai_dev() {
  ai_dev_bin="${AI_DEV_BIN:-ai-dev}"

  if ! command -v "$ai_dev_bin" >/dev/null 2>&1; then
    ai_dev_warn "command not found: $ai_dev_bin"
    unset ai_dev_bin
    return 0
  fi

  if [ -n "${DIRENV_IN_ENVRC:-}" ]; then
    ai_dev_project_dir="${OLDPWD:-$PWD}"
  else
    ai_dev_project_dir="$PWD"
  fi

  ai_dev_exports="$(cd "$ai_dev_project_dir" && "$ai_dev_bin" env --shell sh)"
  ai_dev_status=$?

  if [ "$ai_dev_status" -ne 0 ]; then
    ai_dev_error "failed to resolve environment"
    unset ai_dev_bin ai_dev_project_dir ai_dev_exports ai_dev_status
    return 1
  fi

  if [ -n "$ai_dev_exports" ]; then
    eval "$ai_dev_exports"
  fi

  unset ai_dev_bin ai_dev_project_dir ai_dev_exports ai_dev_status
}

# use_ai_dev_hook augments direnv's normal bash or zsh hook. Install it
# after `eval "$(direnv hook SHELL)"`. It unloads the current parent .envrc
# before direnv reevaluates that same file for a new nested directory.
use_ai_dev_hook() {
  case "${1:-}" in
  bash)
    AI_DEV_DIRENV_SHELL=bash

    if [ "${AI_DEV_DIRENV_BASH_HOOK:-}" = "1" ]; then
      return 0
    fi

    AI_DEV_DIRENV_BASH_HOOK=1
    AI_DEV_DIRENV_LAST_PWD="$PWD"

    # Use eval so this POSIX-compatible file remains parseable by shells
    # that do not support bash array syntax.
    eval '
      if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
        PROMPT_COMMAND=(_use_ai_dev_direnv_reload_if_needed "${PROMPT_COMMAND[@]}")
      else
        PROMPT_COMMAND="_use_ai_dev_direnv_reload_if_needed${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
      fi
    '
    ;;

  zsh)
    AI_DEV_DIRENV_SHELL=zsh

    if [ "${AI_DEV_DIRENV_ZSH_HOOK:-}" = "1" ]; then
      return 0
    fi

    AI_DEV_DIRENV_ZSH_HOOK=1
    AI_DEV_DIRENV_LAST_PWD="$PWD"

    eval '
      typeset -ga chpwd_functions
      chpwd_functions=(_use_ai_dev_direnv_reload_if_needed ${chpwd_functions:#_use_ai_dev_direnv_reload_if_needed})
    '
    ;;

  *)
    ai_dev_error "unsupported direnv hook shell: ${1:-<empty>}"
    return 1
    ;;
  esac
}

# _use_ai_dev_direnv_reload_if_needed runs in the interactive shell before
# direnv's standard hook. If the current directory is still beneath the
# loaded parent .envrc, it first asks direnv to unload from the directory
# above that parent. The standard hook can then load the same .envrc again
# using the new project directory.
_use_ai_dev_direnv_reload_if_needed() {
  if [ "${AI_DEV_DIRENV_LAST_PWD:-}" = "$PWD" ]; then
    return 0
  fi

  AI_DEV_DIRENV_LAST_PWD="$PWD"

  if [ -z "${DIRENV_DIR:-}" ]; then
    return 0
  fi

  ai_dev_loaded_dir="${DIRENV_DIR#-}"

  case "$PWD/" in
  "$ai_dev_loaded_dir"/*)
    if [ "$ai_dev_loaded_dir" = "/" ]; then
      ai_dev_unload_dir="/"
    else
      ai_dev_unload_dir="${ai_dev_loaded_dir%/*}"
      if [ -z "$ai_dev_unload_dir" ]; then
        ai_dev_unload_dir="/"
      fi
    fi

    ai_dev_direnv_bin="${DIRENV_BIN:-direnv}"
    if [ "$AI_DEV_DIRENV_SHELL" = "zsh" ]; then
      # zsh normally fires chpwd hooks even for a cd inside command
      # substitution. -q suppresses that recursion for this internal cd.
      ai_dev_unload_exports="$(
        builtin cd -q "$ai_dev_unload_dir" &&
          "$ai_dev_direnv_bin" export "$AI_DEV_DIRENV_SHELL"
      )"
    else
      ai_dev_unload_exports="$(
        cd "$ai_dev_unload_dir" &&
          "$ai_dev_direnv_bin" export "$AI_DEV_DIRENV_SHELL"
      )"
    fi
    ai_dev_status=$?

    if [ "$ai_dev_status" -ne 0 ]; then
      ai_dev_error "failed to unload the previous direnv environment"
      unset ai_dev_loaded_dir ai_dev_unload_dir ai_dev_direnv_bin
      unset ai_dev_unload_exports ai_dev_status
      return 1
    fi

    if [ -n "$ai_dev_unload_exports" ]; then
      eval "$ai_dev_unload_exports"
    fi

    unset ai_dev_unload_dir ai_dev_direnv_bin ai_dev_unload_exports ai_dev_status
    ;;
  esac

  unset ai_dev_loaded_dir
}

ai_dev_warn() {
  if command -v log_error >/dev/null 2>&1; then
    log_error "ai-dev: $1"
  else
    printf 'ai-dev: %s\n' "$1" >&2
  fi
}

ai_dev_error() {
  ai_dev_warn "$1"
}

# Backward-compatible aliases for early Checkpoint 3B prototypes.
ai_dev_activate() {
  use_ai_dev "$@"
}

ai_dev_direnv_hook() {
  use_ai_dev_hook "$@"
}
