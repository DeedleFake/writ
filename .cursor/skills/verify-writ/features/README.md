# Writ verification map

This directory is the maintained source for verifying user-facing Writ CLI behavior. Read this index before driving, then use the matching feature file.

## Baseline preconditions

- Repo root is `deedles.dev/writ` (`go.mod`).
- `VERIFY_WRIT_BIN` is a `go build` of `./cmd/writ` from this checkout (`GOTOOLCHAIN=auto`).
- `VERIFY_WRIT_HOME` is a fresh `/tmp/verify-writ-$RUN_ID`.
- `VERIFY_WRIT_EVIDENCE` is set and empty for this run.
- `XDG_CONFIG_HOME=$VERIFY_WRIT_HOME/config` so REPL history cannot touch the user's `~/.config/writ/history`.
- `doctor.sh` passed.
- Never drive a `writ` that this run did not build.

## Driving conventions

- Start from baseline unless a feature lists extra preconditions.
- Commands are literal. Keep flags and example paths unchanged unless the recipe copies files first.
- Short-lived commands (`help`, `run`, `check`, `fmt`) are subprocesses via `capture.sh`.
- REPL eval may use a stdin pipe (non-TTY). History and tab completion need a TTY.
- Restore copied example files after mutation. Keep proof artifacts.

## Proof and skip reporting

- CLI proof is the command, stdout, stderr, and exit code.
- Mutation proof (`fmt -w`, REPL `def`) includes a second read of the file or a follow-up eval.
- Record the feature ID on every artifact name.
- An unreachable path is reported with the command and the unmet precondition. Do not mark it verified via `go test`.

## Feature entry contract

Each feature file: H1, one paragraph, then exactly four H2s: `Sub-features`, `How to get to it (user POV)`, `Driving it with the writ CLI`, `Gotchas`.

## Features

- [Run a script](./run.md) — `writ run`, `main`, `print`, `(import)`.
- [Type-check](./check.md) — `writ check` clean and failing diagnostics.
- [Format](./fmt.md) — `writ fmt` stdout and `-w`, symlink refusal.
- [REPL](./repl.md) — `writ` / `writ repl`, continuation, errors.
- [Help](./help.md) — `writ help` and per-command help.
