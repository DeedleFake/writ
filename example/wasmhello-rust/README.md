# wasmhello-rust

Rust port of [`../wasmhello`](../wasmhello): a Writ WASM package that exports
`greet`, `unless`, `version`, and opaque-handle demos `mk` / `inc` / `get` / `echo`.

## Build

Requires a Rust toolchain with the `wasm32-wasip1` target:

```bash
rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release
```

Output: `target/wasm32-wasip1/release/wasmhello_rust.wasm`.

## Try it

```writ
(let [m: (import "wasmhello_rust.wasm")]
  (let [c: (m.mk)]
    (m.inc c)
    (m.get c)))
```

```bash
cp target/wasm32-wasip1/release/wasmhello_rust.wasm /tmp/
printf '%s\n' '(let [m: (import "wasmhello_rust.wasm")] (m.greet "rust"))' >/tmp/hi.writ
writ run /tmp/hi.writ
```
