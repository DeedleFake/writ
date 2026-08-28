# Writ

[![Go Reference](https://pkg.go.dev/badge/deedles.dev/writ.svg)](https://pkg.go.dev/deedles.dev/writ)

Writ is a Lisp embedded in Go. This module is the language library and a CLI.

## Install

```
go install deedles.dev/writ/cmd/writ@latest
```

## CLI

```
writ                        # REPL
writ help                   # list commands
writ help run               # command help
writ repl                   # REPL
writ -I DIR                 # REPL with import search path
writ run FILE.writ
writ fmt FILE.writ          # formatted source on stdout
writ fmt -w FILE.writ       # rewrite the file
writ check FILE.writ        # type-check; non-zero exit on error
```

`writ help` and `writ -h` / `writ --help` print the same overview. `writ help <command>` and `writ <command> -h` print the same command help.

With no arguments, `writ` starts a REPL (`writ repl` is the same). Unclosed `(`, `[`, strings, and tick symbols continue on the next line. `-I DIR` sets the import search path, as with `writ run`, and is valid on `writ` or `writ repl`. When stdin and stdout are a terminal, the REPL uses line editing, history, and tab completion of keywords and builtins. History is stored under the user config directory. Ctrl+C cancels the current line. Ctrl+Z is ignored (it does not suspend).

`writ run` evaluates top-level forms, then calls `main` if it was defined. The CLI registers `print`, which writes to stdout.

```
(def (main)
  (print "hello"))
```

## Language

Integers are arbitrary-precision. `1` is an integer; `1.0` is a float. `+`, `-`, and `*` stay integers when every operand is an integer. `/` of integers stays an integer when the division is exact; otherwise it is a float. Division by zero is an error.

`true`, `false`, and `nil` are interned symbols. `true` and `false` are booleans; `nil` is not. All three satisfy `symbol?`.

```
(+ 1 2)
(def (add a b) (+ a b))
(let [x: 1 y: 2] (+ x y))
(if (int? n) (+ n 1) else 0)
(pipe xs (list-map (fn * #1 2)) (list-reduce 0 (fn + #1 #2)))
```

Lists are `[a b c]`. Maps are `[k: v]` or empty `[:]`. Mixing plain items and `k:` pairs in one `[]` is a parse error. Map keys are symbols: `(map-get m 'k)`. Required field access is `m.k` or `(. m k)`; a missing key or a non-map left side is an error. `map-get` still returns `nil` for a missing key. Dots in a symbol name are written with ticks: `` `io.write` ``. `'`, `,`, and `@` apply to the whole dotted token (`'io.write` is quote of `(. io write)`); unquote of only the left is `(. ,m write)`.

A `defm` body is expand-time fragments. Each result is one form at the call site. A top-level `@` splices a list of forms into that sequence. In a `fn` / `let` / `if` / `after` / `on` body, and at the top of a script, a call that expands to several forms is those forms in place. In expression position they run in order and the value is the last form.

```
(defm (example @rest)
  '(print "Example.")
  @rest)
```

## Import

`(import "path")` evaluates another script once per runtime and returns a map of that script's top-level `def` and `defm` names. Names that start with `-` are private and are not exported. It is an expression and may appear in `let`. A `defm` in that map is a macro: `(m.unless …)` expands with unevaluated arguments, for both keyed import and `(let [m: (import "m")] …)`. Passing a macro to `list-map` (or otherwise applying it as a function) is an error.

At the top of a script, keyed import binds names without exporting them:

```
(import io: "io" lib: "lib.writ")
(lib.double 21)
```

Each key is the local name; each value is a path expression. Keyed `import` must appear before any other top-level form (`def`, `defm`, `on`, or a boot expression), including forms produced by macro expansion in the same compile. Several keyed imports may be consecutive. The REPL does not apply that file-order rule across sequential `Eval` calls. A later `def` re-exports a name if needed.

Relative paths are resolved from the importing file. Search directories can be set on the runtime (`WithSearchPath`) or with `writ run -I DIR` / `writ check -I DIR` / `writ repl -I DIR`.

Resolution order for a path without a known suffix:

1. An in-process package registered with `RegisterPackage`
2. A `.writ` file under the importing file’s directory and `WithSearchPath` (cwd is used only when there is no importing file and no search path)
3. A native plugin (`.so` / `.dylib` / `.dll`) only if the host called `WithNativePlugins`

Absolute paths and `..` that leave those roots are rejected unless `WithAllowAbsoluteImports` is set. Untrusted `Eval` should leave plugins off and set an explicit search path. The default unlimited eval budget is not a sandbox.

```
(let [lib: (import "lib.writ")]
  (lib.double 21))
```

## Embed

```go
import (
    "deedles.dev/writ"
    "deedles.dev/writ/runtime"
    "deedles.dev/writ/types"
)

rt := writ.New()
rt.RegisterPrint()
rt.RegisterPackage("mathx", runtime.Package{
    Funcs: map[string]runtime.Func{
        "double": func(args []runtime.Value) (runtime.Value, error) {
            return runtime.Int64(args[0].BigInt().Int64() * 2), nil
        },
    },
})
rt.RegisterEvent("tick", types.PayloadKey{Name: "n", Type: types.IntType()})
rt.RegisterAlias("color", "red", "blue")
rt.SetScheduler(func(d time.Duration, fn func()) { /* own loop */ })

if _, err := rt.EvalFile("script.writ"); err != nil { ... }
_ = rt.Fire("tick", map[string]runtime.Value{"n": runtime.Int64(1)})
res := rt.Check(src) // diagnostics and type hints
```

A native plugin exports:

```go
func WritPackage() runtime.Package
```

Build with `go build -buildmode=plugin`. In-process `RegisterPackage` is the embedding path that works everywhere, including WASM.

## Parse, format, and tokens

Editors and formatters import the package that owns the name:

```go
forms, err := parser.Parse(src)
text, err := parser.Format(src)
toks := scanner.Tokenize(src)
res := rt.Check(src)
```
