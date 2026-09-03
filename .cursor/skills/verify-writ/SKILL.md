---
name: verify-writ
description: Drive the Writ Lisp CLI (run, check, fmt, repl, help) the way a user does. Use when proving CLI behavior, import, type-check diagnostics, formatting, or REPL eval after a change to deedles.dev/writ.
---

# Verify Writ

Writ is a Lisp embedded in Go. The user-facing app is the `writ` CLI in `cmd/writ`. The Go library (`RegisterPackage`, `Eval`, `Check`) is a second surface; do not treat library tests as CLI proof. Go plugin / `.so` packages are gone; binary packages are WASM only.

This skill is for agents. Commands are literal.

## Launch

There is no long-lived server. Launch means build one binary, then start each drive as its own process.

From the repo root (`deedles.dev/writ`):

```sh
export VERIFY_WRIT_ROOT="$(pwd)"
export RUN_ID="$(date +%Y%m%dT%H%M%S)-$$"
export VERIFY_WRIT_HOME="/tmp/verify-writ-$RUN_ID"
export VERIFY_WRIT_EVIDENCE="${VERIFY_WRIT_EVIDENCE:-$VERIFY_WRIT_ROOT/.cursor/skills/verify-writ/artifacts/$RUN_ID}"
export XDG_CONFIG_HOME="$VERIFY_WRIT_HOME/config"
mkdir -p "$VERIFY_WRIT_HOME/bin" "$VERIFY_WRIT_EVIDENCE"
GOTOOLCHAIN=auto go build -o "$VERIFY_WRIT_HOME/bin/writ" ./cmd/writ
export VERIFY_WRIT_BIN="$VERIFY_WRIT_HOME/bin/writ"
export PATH="$VERIFY_WRIT_HOME/bin:$PATH"
```

Ready: `"$VERIFY_WRIT_BIN" help` exits 0 and stdout contains `The commands are:`.

Go 1.24+ can fetch the `go 1.27.0` toolchain via `GOTOOLCHAIN=auto`. Do not override `HOME` (module cache). Isolate REPL history with `XDG_CONFIG_HOME` only; history would otherwise land in `$XDG_CONFIG_HOME/writ/history` (or `os.UserConfigDir()/writ/history`).

Two CLIs can run side by side. Do not drive a `writ` binary you did not just build. Do not point `XDG_CONFIG_HOME` at the user's real config.

## Doctor

Read-only. Run first if anything looks off:

```sh
.cursor/skills/verify-writ/scripts/doctor.sh
```

Requires `VERIFY_WRIT_BIN` (and `VERIFY_WRIT_ROOT` when checking `go.mod`). Pass means: binary is ours, `writ help` / `writ help run` match the CLI strings below, module path is `deedles.dev/writ`.

## Drive

Harness is the CLI process. No browser. Prefer argv and stdout/stderr over PTY except where the map asks for a TTY.

| Command | Ready / proof handle |
| --- | --- |
| `writ help` | stdout starts with `Writ is a Lisp interpreter.` and lists `repl`, `run`, `fmt`, `check` |
| `writ help run` | `usage: writ run [-I directory] FILE.writ` |
| `writ run FILE.writ` | evaluates top-level forms, then calls `main` if defined; `print` writes to stdout |
| `writ check FILE.writ` | exit 0 if clean; else exit 1 and stderr `FILE:line:col: message` |
| `writ fmt FILE.writ` | canonical form on stdout; `-w` rewrites the file; `-w` on a symlink fails with `symlink` |
| `writ` / `writ repl` | prompt `> `; continuation `... `; prints `runtime.Print` of the last value |

Capture every drive:

```sh
.cursor/skills/verify-writ/scripts/capture.sh NAME -- writ run example/hello.writ
```

REPL without a TTY (stdin pipe) still prints `> ` and evaluates. That is enough for eval proof. Use a TTY (`script` / `tmux`) only for history or tab completion, and keep `XDG_CONFIG_HOME` under `VERIFY_WRIT_HOME`.

Seed scripts: `example/hello.writ` and `example/lib.writ` (import `lib.double`, `main` prints `hello from writ` then `42`). Copy them into `$VERIFY_WRIT_HOME` before mutating.

Read the [feature map](features/README.md) before driving. Prove the mapped entry points, not `go test`.

## Evidence

Directory: `$VERIFY_WRIT_EVIDENCE` (default `.cursor/skills/verify-writ/artifacts/$RUN_ID`). Gitignored. Survives cleanup.

Each capture writes `NAME.cmd`, `NAME.stdout`, `NAME.stderr`, `NAME.exit`. For `fmt -w`, also copy the file after the write (`NAME.after`). For REPL, the transcript is stdout (prompts + values).

Proof standards:

- Drive `cmd/writ`, not `rt.Eval` in a test helper.
- Record the command and the resulting stdout/stderr/exit, not only the last line.
- For `run`, assert printed values (`hello from writ` / `42`), not just exit 0.
- For `check` failure, assert `line:col` and the diagnostic text.
- For `fmt -w`, read the file back; do not trust the flag name.
- Mocks: none. Plugins are gone; only WASM binary packages (plus `.writ` and `RegisterPackage`).

## Cleanup

```sh
.cursor/skills/verify-writ/scripts/cleanup.sh
```

Kills PIDs recorded under `$VERIFY_WRIT_HOME/pids` (if a TTY REPL was left running), then `rm -rf "$VERIFY_WRIT_HOME"`. Does not delete `$VERIFY_WRIT_EVIDENCE`. Never `pkill writ`.

## Helpers

All under `.cursor/skills/verify-writ/scripts/`, executable:

```sh
.cursor/skills/verify-writ/scripts/doctor.sh
.cursor/skills/verify-writ/scripts/capture.sh run-hello -- writ run example/hello.writ
.cursor/skills/verify-writ/scripts/cleanup.sh
```

`capture.sh` runs from `VERIFY_WRIT_ROOT` unless `CAPTURE_CWD` is set. `NAME` is a single path segment.
