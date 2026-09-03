# Help

`writ help` prints the command list. `writ help <command>` prints that command's usage. `-h` / `--help` / `-help` match `help`.

## Sub-features

- `help-root` lists `repl`, `run`, `fmt`, `check`.
- `help-topic` prints `usage: writ <command>` for each command.
- `help-flags` aliases (`-h`, `--help`) match `writ help`.
- `help-unknown` rejects an unknown topic.

## How to get to it (user POV)

- `writ help`
- `writ help run` (also `repl`, `fmt`, `check`)
- `writ -h` / `writ --help`
- `writ run -h`

## Driving it with the writ CLI

Preconditions:

- Doctor passed.

- **Root.** `capture.sh help-root -- writ help`. Exit `0`. stdout contains `Writ is a Lisp interpreter.` and `The commands are:` and the four commands.
- **Topic.** `capture.sh help-run -- writ help run`. stdout contains `usage: writ run`. Repeat for `repl`, `fmt`, `check` (`help-repl`, `help-fmt`, `help-check`).
- **Alias.** `capture.sh help-h -- writ -h`. Bytes equal `help-root.stdout`. `capture.sh help-run-h -- writ run -h` equals `help-run.stdout`.
- **Unknown.** `capture.sh help-nope -- writ help nope`. Non-zero. stderr contains `unknown help topic`.
- **Unknown command.** `capture.sh help-badcmd -- writ nope`. Non-zero. stderr contains `unknown command` and `writ help`.

## Gotchas

- Root help goes to stdout; unknown topic/command go to stderr with exit 1.
- `writ help -h` is still root help, not an error.
