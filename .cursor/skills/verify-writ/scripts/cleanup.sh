#!/bin/sh
set -eu

if [ -z "${VERIFY_WRIT_HOME:-}" ]; then
	printf 'cleanup: VERIFY_WRIT_HOME is not set\n' >&2
	exit 1
fi

case "$VERIFY_WRIT_HOME" in
/tmp/verify-writ-*) ;;
*)
	printf 'cleanup: refusing to remove unexpected VERIFY_WRIT_HOME=%s\n' "$VERIFY_WRIT_HOME" >&2
	exit 1
	;;
esac

if [ -d "$VERIFY_WRIT_HOME/pids" ]; then
	for f in "$VERIFY_WRIT_HOME/pids"/*; do
		[ -f "$f" ] || continue
		pid=$(cat "$f")
		if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
			kill "$pid" 2>/dev/null || true
			sleep 0.1
			kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
		fi
	done
fi

rm -rf "$VERIFY_WRIT_HOME"
printf 'cleanup: removed %s (evidence kept at %s)\n' "$VERIFY_WRIT_HOME" "${VERIFY_WRIT_EVIDENCE:-unset}"
