#!/bin/sh
set -eu

purge=0
case "${1:-}" in
	"") ;;
	--purge) purge=1 ;;
	*)
		printf '%s\n' "usage: uninstall.sh [--purge]" >&2
		exit 2
		;;
esac

home="${HOME:-}"
[ -n "$home" ] || exit 1

remove_binary() {
	path="$1"
	if [ -f "$path" ] || [ -L "$path" ]; then
		if [ -w "$path" ]; then
			rm "$path"
			printf '%s\n' "ai-dev: removed $path"
		else
			printf '%s\n' "ai-dev: cannot remove $path without permission"
		fi
	fi
}

remove_binary "${AI_DEV_INSTALL_DIR:-$home/.local/bin}/ai-dev"
old_ifs="$IFS"
IFS=:
for path in ${PATH:-}; do
	[ -n "$path" ] && remove_binary "$path/ai-dev"
done
IFS="$old_ifs"
if command -v go >/dev/null 2>&1; then
	gobin="$(go env GOBIN 2>/dev/null || true)"
	if [ -n "$gobin" ]; then
		remove_binary "$gobin/ai-dev"
	else
		gopath="$(go env GOPATH 2>/dev/null || true)"
		old_ifs="$IFS"
		IFS=:
		for path in $gopath; do
			[ -n "$path" ] && remove_binary "$path/bin/ai-dev"
		done
		IFS="$old_ifs"
	fi
fi
remove_binary "$home/go/bin/ai-dev"

if [ "$purge" = "1" ]; then
	for directory in "$home/.config/ai-dev" "$home/.local/share/ai-dev" "$home/.local/state/ai-dev"; do
		if [ -d "$directory" ]; then
			rm -rf "$directory"
			printf '%s\n' "ai-dev: purged $directory"
		fi
	done
else
	printf '%s\n' "ai-dev: configuration preserved"
fi
