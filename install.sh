#!/bin/sh
set -eu

REPOSITORY="${AI_DEV_REPOSITORY:-ricardoferreirades/ai-dev}"
INSTALL_DIR="${AI_DEV_INSTALL_DIR:-}"
REQUESTED_VERSION="${AI_DEV_VERSION:-latest}"
FROM_SOURCE="${AI_DEV_FROM_SOURCE:-0}"

say() {
	printf '%s\n' "ai-dev: $*"
}

fail() {
	printf '%s\n' "ai-dev: error: $*" >&2
	exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

os="$(uname -s 2>/dev/null || true)"
arch="$(uname -m 2>/dev/null || true)"
case "$os" in
	Darwin) platform="darwin" ;;
	Linux) platform="linux" ;;
	*) fail "unsupported operating system: $os" ;;
esac
case "$arch" in
	arm64|aarch64) architecture="arm64" ;;
	x86_64|amd64) architecture="amd64" ;;
	*) fail "unsupported CPU architecture: $arch" ;;
esac

if [ -z "$INSTALL_DIR" ]; then
	old_ifs="$IFS"
	IFS=:
	for path in $PATH; do
		[ -n "$path" ] || continue
		if [ -d "$path" ] && [ -w "$path" ]; then
			INSTALL_DIR="$path"
			break
		fi
	done
	IFS="$old_ifs"
fi

if [ -z "$INSTALL_DIR" ]; then
	INSTALL_DIR="${HOME:-}/.local/bin"
	[ -n "$HOME" ] || fail "HOME is not set"
	mkdir -p "$INSTALL_DIR"
fi

temporary_dir="$(mktemp -d 2>/dev/null || mktemp -d -t ai-dev)"
cleanup() {
	rm -rf "$temporary_dir"
}
trap cleanup EXIT INT TERM

binary="$temporary_dir/ai-dev"
if [ "$FROM_SOURCE" = "1" ]; then
	command -v go >/dev/null 2>&1 || fail "Go is required for AI_DEV_FROM_SOURCE=1"
	project_dir="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
	CGO_ENABLED=0 go build -trimpath -o "$binary" "$project_dir"
else
	api_url="https://api.github.com/repos/$REPOSITORY/releases/latest"
	if [ "$REQUESTED_VERSION" != "latest" ]; then
		tag="v${REQUESTED_VERSION#v}"
	else
		release_json="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$api_url")" || fail "could not retrieve the latest release"
		tag="$(printf '%s' "$release_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
		[ -n "$tag" ] || fail "the repository has no published release yet"
	fi
	version="${tag#v}"
	archive="ai-dev_${version}_${platform}_${architecture}.tar.gz"
	base_url="https://github.com/$REPOSITORY/releases/download/$tag"
	curl -fsSL "$base_url/$archive" -o "$temporary_dir/$archive" || fail "release asset not found: $archive"
	curl -fsSL "$base_url/checksums.txt" -o "$temporary_dir/checksums.txt" || fail "release checksums not found"
	expected_checksum="$(awk -v file="$archive" '$2 == file { print $1 }' "$temporary_dir/checksums.txt")"
	[ -n "$expected_checksum" ] || fail "release checksum not found for $archive"
	if command -v shasum >/dev/null 2>&1; then
		actual_checksum="$(shasum -a 256 "$temporary_dir/$archive" | awk '{ print $1 }')"
	elif command -v sha256sum >/dev/null 2>&1; then
		actual_checksum="$(sha256sum "$temporary_dir/$archive" | awk '{ print $1 }')"
	else
		fail "shasum or sha256sum is required to verify the release"
	fi
	[ "$expected_checksum" = "$actual_checksum" ] || fail "release checksum verification failed"
	tar -xzf "$temporary_dir/$archive" -C "$temporary_dir"
	[ -x "$temporary_dir/ai-dev" ] || chmod +x "$temporary_dir/ai-dev"
	[ -f "$temporary_dir/ai-dev" ] || fail "release archive did not contain ai-dev"
	fi

mkdir -p "$INSTALL_DIR"
if [ -e "$INSTALL_DIR/ai-dev" ] && [ ! -w "$INSTALL_DIR/ai-dev" ]; then
	fail "cannot replace $INSTALL_DIR/ai-dev; choose AI_DEV_INSTALL_DIR or use a writable PATH directory"
fi
install -m 0755 "$binary" "$INSTALL_DIR/ai-dev"

case ":${PATH:-}:" in
	*":$INSTALL_DIR:"*) path_ready=1 ;;
	*) path_ready=0 ;;
esac

if [ "$path_ready" = "0" ]; then
	shell_name="$(basename "${SHELL:-sh}")"
	case "$shell_name" in
		zsh) shell_file="${ZDOTDIR:-$HOME}/.zshrc" ;;
		bash) shell_file="${HOME}/.bashrc" ;;
		*) shell_file="${HOME}/.profile" ;;
	esac
	mkdir -p "$(dirname "$shell_file")"
	marker="# ai-dev installer"
	if [ ! -f "$shell_file" ] || ! grep -Fqx "$marker" "$shell_file" 2>/dev/null; then
		{
			printf '\n%s\n' "$marker"
			printf 'export PATH="%s:$PATH"\n' "$INSTALL_DIR"
		} >> "$shell_file"
	fi
	path_hint="export PATH=\"$INSTALL_DIR:\$PATH\""
else
	path_hint=""
fi

if [ -x "$INSTALL_DIR/ai-dev" ]; then
	installed_version="$($INSTALL_DIR/ai-dev version 2>/dev/null || true)"
	[ -n "$installed_version" ] || installed_version="installed"
	say "$installed_version at $INSTALL_DIR/ai-dev"
fi
if [ -n "$path_hint" ]; then
	say "open a new shell or run: $path_hint"
fi
