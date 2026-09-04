# wasmhello-rust

Rust port of [`../wasmhello`](../wasmhello): a Writ WASM package that exports
`greet`, `unless`, and `version`.

## Build

Requires a Rust toolchain with the `wasm32-wasip1` target:

```bash
rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release
```

Output: `target/wasm32-wasip1/release/wasmhello_rust.wasm`.

## Try it

Copy the `.wasm` next to a script and import it:

```writ
(let [m: (import "wasmhello_rust.wasm")]
  (m.greet "rust"))
```

```bash
cp target/wasm32-wasip1/release/wasmhello_rust.wasm /tmp/
printf '%s\n' '(let [m: (import "wasmhello_rust.wasm")] (m.greet "rust"))' >/tmp/hi.writ
writ run /tmp/hi.writ
```
