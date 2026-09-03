# Format

`writ fmt FILE.writ` writes canonical form to stdout. `writ fmt -w FILE.writ` replaces the file. It refuses to write through a symlink.

## Sub-features

- `fmt-stdout` prints formatted source and leaves the file unchanged.
- `fmt-write` rewrites the file to match stdout.
- `fmt-symlink` rejects `-w` on a symlink without changing the target.

## How to get to it (user POV)

- `writ fmt FILE.writ`
- `writ fmt -w FILE.writ`
- `writ help fmt`

## Driving it with the writ CLI

Preconditions:

- Doctor passed.
- Scratch copies live under `$VERIFY_WRIT_HOME`, not `example/`.

- **Stdout.** Copy `example/hello.writ` to `$VERIFY_WRIT_HOME/a.writ`. Run `capture.sh fmt-stdout -- writ fmt "$VERIFY_WRIT_HOME/a.writ"`. Exit `0`. stdout contains `def` and `import`. `cmp` the copy to `example/hello.writ` (unchanged).
- **Spaces.** Write `$VERIFY_WRIT_HOME/spaces.writ` with `(+ 1   2)\n`. Run `capture.sh fmt-spaces -- writ fmt "$VERIFY_WRIT_HOME/spaces.writ"`. Exit `0`. stdout is the canonical form (not the triple-space source).
- **Write.** Run `capture.sh fmt-write -- writ fmt -w "$VERIFY_WRIT_HOME/spaces.writ"`. Exit `0`. Read `$VERIFY_WRIT_HOME/spaces.writ`; it equals `fmt-spaces.stdout`.
- **Symlink.** `ln -s spaces.writ "$VERIFY_WRIT_HOME/link.writ"` (same directory). Run `capture.sh fmt-symlink -- writ fmt -w "$VERIFY_WRIT_HOME/link.writ"`. Non-zero exit. stderr/stdout contains `symlink`. Target file bytes unchanged.
- **Proof.** Keep `fmt-spaces.stdout` and the post-`-w` file copy. Do not treat a silent `-w` as success without reading the file.

## Gotchas

- `fmt` is not a pretty-printer that always inserts newlines; assert against `writ fmt` stdout, not a hand-made layout.
- `-w` uses a temp file in the same directory then `Rename`. Proof is the target path contents.
- Never `fmt -w` the files under `example/` during verification.
