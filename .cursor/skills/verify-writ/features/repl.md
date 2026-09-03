# REPL

With no arguments, `writ` starts a REPL (`writ repl` is the same). It evaluates forms, prints the result, and keeps going after parse/eval errors. Unclosed `(` continues with `... `.

## Sub-features

- `repl-eval` evaluates `(+ 1 2)` and prints `3`.
- `repl-cont` continues an incomplete form.
- `repl-search` loads an import via `-I`.
- `repl-bare` is `writ` with no subcommand (same as `writ repl`).

## How to get to it (user POV)

- `writ`
- `writ repl`
- `writ repl -I DIR` or `writ -I DIR`
- `writ help repl`

## Driving it with the writ CLI

Preconditions:

- Doctor passed.
- `XDG_CONFIG_HOME=$VERIFY_WRIT_HOME/config`.
- Stdin is a pipe unless the recipe says TTY.

- **Eval.** `printf '(+ 1 2)\n' |` then `capture.sh repl-eval -- writ repl`. Exit `0`. stdout contains `> ` and `3`.
- **Bare.** Same input piped to `capture.sh repl-bare -- writ`. stdout contains `3`.
- **Continuation.** `printf '(+ 1\n2)\n' |` then `capture.sh repl-cont -- writ repl`. stdout contains `... ` and `3`.
- **Search path.** Write `$VERIFY_WRIT_HOME/lib.writ` as `(def (n) 3)\n`. Pipe `(print ((map-get (import "lib.writ") 'n)))\n` into `capture.sh repl-search -- writ repl -I "$VERIFY_WRIT_HOME"`. stdout contains `3`.
- **Proof.** Transcripts include the prompt and the printed value. A TTY is not required for these paths.

## Gotchas

- After EOF the REPL writes a final `> ` or newline; match with `contains`, not exact whole-stdout equality, unless you pin the build.
- Readline history is TTY-only, path `$XDG_CONFIG_HOME/writ/history`. Pipe mode must not create the user's real history file.
- Ctrl+C cancels the current line in a TTY; do not treat that as process death.
- Parse errors print to stderr and the session continues; a later good form still prints.
