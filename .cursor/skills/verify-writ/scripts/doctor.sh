#!/bin/sh
set -eu

fail() {
	printf 'doctor: %s\n' "$*" >&2
	exit 1
}

if [ -z "${VERIFY_WRIT_BIN:-}" ]; then
	fail "VERIFY_WRIT_BIN is not set"
fi
if [ ! -x "$VERIFY_WRIT_BIN" ]; then
	fail "VERIFY_WRIT_BIN is not executable: $VERIFY_WRIT_BIN"
fi

case "$VERIFY_WRIT_BIN" in
*/bin/writ) ;;
*)
	fail "VERIFY_WRIT_BIN should be the isolated build (.../bin/writ), not a random writ on PATH: $VERIFY_WRIT_BIN"
	;;
esac

help_out="$("$VERIFY_WRIT_BIN" help)" || fail "writ help failed"
printf '%s' "$help_out" | grep -q 'Writ is a Lisp interpreter.' || fail "writ help missing title"
printf '%s' "$help_out" | grep -q 'The commands are:' || fail "writ help missing command list"
for cmd in repl run fmt check; do
	printf '%s' "$help_out" | grep -q "$cmd" || fail "writ help missing $cmd"
done

run_help="$("$VERIFY_WRIT_BIN" help run)" || fail "writ help run failed"
printf '%s' "$run_help" | grep -q 'usage: writ run' || fail "writ help run missing usage"

if [ -n "${VERIFY_WRIT_ROOT:-}" ]; then
	mod="$VERIFY_WRIT_ROOT/go.mod"
	[ -f "$mod" ] || fail "no go.mod at VERIFY_WRIT_ROOT"
	grep -q '^module deedles.dev/writ$' "$mod" || fail "go.mod is not deedles.dev/writ"
fi

printf 'doctor: ok %s\n' "$VERIFY_WRIT_BIN"
