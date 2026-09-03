# Type-check

`writ check FILE.writ` type-checks a script and prints diagnostics. Clean scripts exit 0 with empty stderr. Failures exit 1 with `FILE:line:col: message` on stderr.

## Sub-features

- `check-clean` accepts `example/hello.writ`.
- `check-mismatch` reports a builtin type error (`+` with a string).
- `check-missing` fails when the file does not exist.

## How to get to it (user POV)

- `writ check FILE.writ`
- `writ check -I DIR FILE.writ`
- `writ help check`

## Driving it with the writ CLI

Preconditions:

- Doctor passed.
- `example/hello.writ` is unmodified.

- **Clean.** Run `capture.sh check-hello -- writ check example/hello.writ`. Exit `0`. stdout and stderr empty.
- **Type error.** Write `$VERIFY_WRIT_HOME/bad.writ` containing `(+ 1 "x")`. Run `capture.sh check-bad -- writ check "$VERIFY_WRIT_HOME/bad.writ"`. Exit `1`. stderr matches `bad.writ:1:6:` (or the recorded column) and mentions `+`. Combined output contains `:`.
- **Missing file.** Run `capture.sh check-missing -- writ check "$VERIFY_WRIT_HOME/no-such.writ"`. Exit `1`. stderr contains `no such file` or `open`.
- **Proof.** Keep `check-bad.stderr` with the `line:col` diagnostic. A zero exit on the bad file is a fail.

## Gotchas

- Diagnostics go to stderr; do not assert on stdout.
- `check` does not open native plugins (embed tests cover that). A `.so` import during check is not a CLI plugin-load proof.
- Column is an offset into the file, 1-based line/col as printed by the CLI.
