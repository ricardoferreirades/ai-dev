#!/usr/bin/env bash
# Tests for shell/direnv/ai-dev.sh.

set -u

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
helper="$script_dir/ai-dev.sh"
failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

pass() {
  echo "ok - $1"
}

skip() {
  echo "ok - $1 # SKIP $2"
}

make_temp_dir() {
  mktemp -d "${TMPDIR:-/tmp}/ai-dev-direnv-test.XXXXXX"
}

assert_equal() {
  local description="$1"
  local expected="$2"
  local actual="$3"

  if [[ "$actual" == "$expected" ]]; then
    pass "$description"
  else
    fail "$description: expected '$expected', got '$actual'"
  fi
}

test_activation_in_shell() {
  local shell_name="$1"
  local shell_bin="$2"
  local temp_dir
  temp_dir="$(make_temp_dir)"

  cat >"$temp_dir/ai-dev" <<'EOF'
#!/bin/sh
if [ "$1" = "env" ] && [ "$2" = "--shell" ] && [ "$3" = "sh" ]; then
  printf '%s\n' "export AI_DEV_TEST_VAR='from-ai-dev'"
  exit 0
fi
exit 1
EOF
  chmod +x "$temp_dir/ai-dev"

  local output
  output="$(
    AI_DEV_BIN="$temp_dir/ai-dev" \
      "$shell_bin" -f -c '
        . "$1"
        use_ai_dev
        printf "%s" "$AI_DEV_TEST_VAR"
      ' _ "$helper"
  )"
  local status=$?

  rm -rf "$temp_dir"

  if [[ "$status" -eq 0 && "$output" == "from-ai-dev" ]]; then
    pass "activation works under $shell_name"
  else
    fail "activation under $shell_name returned status=$status output='$output'"
  fi
}

test_missing_binary_warns_and_succeeds() {
  local empty_dir
  empty_dir="$(make_temp_dir)"

  local stderr_output
  stderr_output="$(
    AI_DEV_BIN="$empty_dir/does-not-exist" \
      bash --noprofile --norc -c '
        . "$1"
        use_ai_dev
      ' _ "$helper" 2>&1
  )"
  local status=$?

  rm -rf "$empty_dir"

  if [[ "$status" -eq 0 && "$stderr_output" == *"command not found"* ]]; then
    pass "missing ai-dev warns without failing activation"
  else
    fail "missing ai-dev returned status=$status output='$stderr_output'"
  fi
}

test_failing_binary_fails_clearly() {
  local temp_dir
  temp_dir="$(make_temp_dir)"

  cat >"$temp_dir/ai-dev" <<'EOF'
#!/bin/sh
echo "parse configuration: invalid TOML" >&2
exit 1
EOF
  chmod +x "$temp_dir/ai-dev"

  local stderr_file="$temp_dir/stderr"
  AI_DEV_BIN="$temp_dir/ai-dev" \
    bash --noprofile --norc -c '
      . "$1"
      use_ai_dev
    ' _ "$helper" 2>"$stderr_file"
  local status=$?
  local stderr_output
  stderr_output="$(cat "$stderr_file")"

  rm -rf "$temp_dir"

  if [[
    "$status" -ne 0 &&
      "$stderr_output" == *"parse configuration: invalid TOML"* &&
      "$stderr_output" == *"failed to resolve environment"*
  ]]; then
    pass "invalid configuration fails activation clearly"
  else
    fail "failing ai-dev returned status=$status output='$stderr_output'"
  fi
}

test_stderr_is_not_evaluated() {
  local temp_dir
  temp_dir="$(make_temp_dir)"

  cat >"$temp_dir/ai-dev" <<'EOF'
#!/bin/sh
echo "export AI_DEV_STDERR_WAS_EVALUATED='yes'" >&2
echo "export AI_DEV_STDOUT_WAS_EVALUATED='yes'"
EOF
  chmod +x "$temp_dir/ai-dev"

  local result
  result="$(
    AI_DEV_BIN="$temp_dir/ai-dev" \
      bash --noprofile --norc -c '
        . "$1"
        use_ai_dev 2>/dev/null
        printf "%s/%s" "${AI_DEV_STDOUT_WAS_EVALUATED:-no}" "${AI_DEV_STDERR_WAS_EVALUATED:-no}"
      ' _ "$helper"
  )"

  rm -rf "$temp_dir"

  assert_equal "only trusted stdout is evaluated" "yes/no" "$result"
}

setup_integration_fixture() {
  integration_root="$(make_temp_dir)"
  managed_dir="$integration_root/managed"
  outside_dir="$integration_root/outside"
  project_a="$managed_dir/project-a"
  project_b="$managed_dir/project-b"
  worktree_a="$managed_dir/project-a-worktree"
  ai_dev_config="$integration_root/ai-dev-config"
  direnv_config="$integration_root/direnv-config"
  direnv_data="$integration_root/direnv-data"

  mkdir -p \
    "$project_a" \
    "$project_b" \
    "$outside_dir" \
    "$ai_dev_config/projects" \
    "$direnv_config/lib" \
    "$direnv_data"

  cp "$helper" "$direnv_config/lib/ai-dev.sh"
  printf '%s\n' 'use_ai_dev' >"$managed_dir/.envrc"

  git -C "$project_a" init -q
  git -C "$project_a" config user.name "ai-dev test"
  git -C "$project_a" config user.email "ai-dev-test@example.invalid"
  git -C "$project_a" remote add origin https://example.com/acme/project-a.git
  git -C "$project_a" commit --allow-empty -q -m initial
  git -C "$project_a" worktree add -q -b integration-worktree "$worktree_a"

  git -C "$project_b" init -q
  git -C "$project_b" remote add origin https://example.com/acme/project-b.git

  cat >"$ai_dev_config/global.toml" <<'EOF'
[environment]
GLOBAL_ONLY = "global"
OVERRIDDEN = "global"
EOF

  cat >"$ai_dev_config/projects/example.com-acme-project-a.toml" <<'EOF'
[environment]
PROJECT_ONLY = "project-a"
OVERRIDDEN = "project-a"
EOF

  cat >"$ai_dev_config/projects/example.com-acme-project-b.toml" <<'EOF'
[environment]
PROJECT_ONLY = "project-b"
OVERRIDDEN = "project-b"
EOF

  DIRENV_CONFIG="$direnv_config" \
    XDG_DATA_HOME="$direnv_data" \
    "$direnv_bin" allow "$managed_dir" >/dev/null
}

run_bash_direnv_scenario() {
  AI_DEV_BIN="$ai_dev_binary" \
    AI_DEV_CONFIG_HOME="$ai_dev_config" \
    DIRENV_BIN="$direnv_bin" \
    DIRENV_CONFIG="$direnv_config" \
    XDG_DATA_HOME="$direnv_data" \
    MANAGED_DIR="$managed_dir" \
    PROJECT_A="$project_a" \
    PROJECT_B="$project_b" \
    WORKTREE_A="$worktree_a" \
    OUTSIDE_DIR="$outside_dir" \
    HELPER="$helper" \
    bash --noprofile --norc -c '
      set -eu
      eval "$("$DIRENV_BIN" hook bash)"
      . "$HELPER"
      use_ai_dev_hook bash

      cd "$PROJECT_A"
      eval "$PROMPT_COMMAND"
      printf "a:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$PROJECT_B"
      eval "$PROMPT_COMMAND"
      printf "b:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$WORKTREE_A"
      eval "$PROMPT_COMMAND"
      printf "w:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$OUTSIDE_DIR"
      eval "$PROMPT_COMMAND"
      printf "o:%s:%s:%s\n" "${GLOBAL_ONLY:-unset}" "${PROJECT_ONLY:-unset}" "${OVERRIDDEN:-unset}"
    ' 2>/dev/null
}

run_zsh_direnv_scenario() {
  AI_DEV_BIN="$ai_dev_binary" \
    AI_DEV_CONFIG_HOME="$ai_dev_config" \
    DIRENV_BIN="$direnv_bin" \
    DIRENV_CONFIG="$direnv_config" \
    XDG_DATA_HOME="$direnv_data" \
    MANAGED_DIR="$managed_dir" \
    PROJECT_A="$project_a" \
    PROJECT_B="$project_b" \
    WORKTREE_A="$worktree_a" \
    OUTSIDE_DIR="$outside_dir" \
    HELPER="$helper" \
    "$zsh_bin" -f -c '
      set -eu
      eval "$("$DIRENV_BIN" hook zsh)"
      . "$HELPER"
      use_ai_dev_hook zsh

      cd "$PROJECT_A"
      printf "a:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$PROJECT_B"
      printf "b:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$WORKTREE_A"
      printf "w:%s:%s:%s\n" "$GLOBAL_ONLY" "$PROJECT_ONLY" "$OVERRIDDEN"

      cd "$OUTSIDE_DIR"
      printf "o:%s:%s:%s\n" "${GLOBAL_ONLY:-unset}" "${PROJECT_ONLY:-unset}" "${OVERRIDDEN:-unset}"
    ' 2>/dev/null
}

assert_integration_scenario() {
  local shell_name="$1"
  local output="$2"
  local expected
  expected=$(
    cat <<'EOF'
a:global:project-a:project-a
b:global:project-b:project-b
w:global:project-a:project-a
o:unset:unset:unset
EOF
  )

  assert_equal "$shell_name updates projects, worktrees, and unloads" "$expected" "$output"
}

test_existing_env_output() {
  local output
  output="$(
    cd "$project_a" &&
      AI_DEV_CONFIG_HOME="$ai_dev_config" "$ai_dev_binary" env --shell sh
  )"
  local expected
  expected=$(
    cat <<'EOF'
export GLOBAL_ONLY='global'
export OVERRIDDEN='project-a'
export PROJECT_ONLY='project-a'
EOF
  )

  assert_equal "existing ai-dev env output remains deterministic" "$expected" "$output"
}

test_real_invalid_toml() {
  printf '%s\n' '[environment' 'BROKEN = true' \
    >"$ai_dev_config/projects/example.com-acme-project-a.toml"

  local stderr_file="$integration_root/invalid-toml-stderr"
  (
    cd "$project_a"
    AI_DEV_BIN="$ai_dev_binary" \
      AI_DEV_CONFIG_HOME="$ai_dev_config" \
      bash --noprofile --norc -c '
        . "$1"
        use_ai_dev
      ' _ "$helper"
  ) 2>"$stderr_file"
  local status=$?
  local stderr_output
  stderr_output="$(cat "$stderr_file")"

  if [[
    "$status" -ne 0 &&
      "$stderr_output" == *"parse configuration"* &&
      "$stderr_output" == *"failed to resolve environment"*
  ]]; then
    pass "real invalid TOML fails activation clearly"
  else
    fail "invalid TOML returned status=$status output='$stderr_output'"
  fi
}

test_activation_in_shell "bash" "$(command -v bash)"

if zsh_bin="$(command -v zsh 2>/dev/null)"; then
  test_activation_in_shell "zsh" "$zsh_bin"
elif [[ "${AI_DEV_TEST_REQUIRE_ZSH:-0}" == "1" ]]; then
  fail "zsh is required but was not found"
else
  skip "activation works under zsh" "zsh is not installed"
fi

test_missing_binary_warns_and_succeeds
test_failing_binary_fails_clearly
test_stderr_is_not_evaluated

ai_dev_binary="${AI_DEV_TEST_BINARY:-}"
direnv_bin="$(command -v direnv 2>/dev/null || true)"

if [[ -z "$ai_dev_binary" ]]; then
  fail "AI_DEV_TEST_BINARY is required for integration tests"
elif [[ -z "$direnv_bin" ]]; then
  if [[ "${AI_DEV_TEST_REQUIRE_DIRENV:-0}" == "1" ]]; then
    fail "direnv is required but was not found"
  else
    skip "direnv integration scenarios" "direnv is not installed"
  fi
else
  setup_integration_fixture
  test_existing_env_output

  bash_output="$(run_bash_direnv_scenario)"
  assert_integration_scenario "bash direnv" "$bash_output"

  if [[ -n "${zsh_bin:-}" ]]; then
    zsh_output="$(run_zsh_direnv_scenario)"
    assert_integration_scenario "zsh direnv" "$zsh_output"
  fi

  test_real_invalid_toml
  rm -rf "$integration_root"
fi

if [[ "$failures" -eq 0 ]]; then
  echo "All ai-dev direnv tests passed."
  exit 0
fi

echo "$failures ai-dev direnv test(s) failed." >&2
exit 1
