# Run a script

`writ run FILE.writ` evaluates the file's top-level forms, then calls `main` if it was defined. `print` writes to stdout. `(import)` loads another script or an in-process package.

## Sub-features

- `run-main` evaluates `example/hello.writ` and runs `main`.
- `run-import` uses keyed import of `lib.writ` (`lib.double`).
- `run-no-main` prints from top level when `main` is absent.
- `run-search` resolves imports via `-I`.

## How to get to it (user POV)

- `writ run FILE.writ`
- `writ run -I DIR FILE.writ` (repeatable `-I`)
- From the repo: `writ run example/hello.writ`

## Driving it with the writ CLI

Preconditions:

- Doctor passed.
- Working directory is `VERIFY_WRIT_ROOT`.
- `example/hello.writ` and `example/lib.writ` are unmodified.

- **Run hello.** Run `.cursor/skills/verify-writ/scripts/capture.sh run-hello -- writ run example/hello.writ`. Exit `0`. stdout is exactly two lines: `hello from writ` then `42`.
- **No main.** Write `$VERIFY_WRIT_HOME/nomain.writ` with `(print "no main")`. Run `capture.sh run-nomain -- writ run "$VERIFY_WRIT_HOME/nomain.writ"` with `CAPTURE_CWD=$VERIFY_WRIT_HOME` or an absolute path. Exit `0`. stdout contains `no main`.
- **Search path.** Write `$VERIFY_WRIT_HOME/search/lib.writ` as `(def (double n) (* n 2))` and `$VERIFY_WRIT_HOME/use.writ` as `(import lib: "lib.writ")` plus `(def (main) (print (lib.double 3)))`. Run `capture.sh run-search -- writ run -I "$VERIFY_WRIT_HOME/search" "$VERIFY_WRIT_HOME/use.writ"`. Exit `0`. stdout contains `6`.
- **Proof.** Keep `run-hello.stdout` showing both printed lines. Re-running `writ run example/hello.writ` is the second view (no stored DB).

## Gotchas

- Without `main`, top-level `print` still runs; missing `main` is not an error.
- Relative import paths resolve from the importing file, not cwd. `-I` is extra search dirs.
- Native `.so` plugins are not loaded by this CLI. Do not use `writ run` as proof of `plugin.Open`.
- `print` is registered by the CLI. A library embed without `RegisterPrint` will not print.
