// Package writ is the Writ Lisp embedding API: Runtime, Value, and Type.
//
// Integers are arbitrary precision (int64 fast path, math/big otherwise).
// Floats are IEEE float64. Inexact division of large integers is float64
// and may round. true, false, and nil are interned symbols; true and false
// are booleans; nil is not. Eval with the default unlimited step budget is
// not a sandbox. Binary packages are WASM only (no Go plugins).
//
// A Value is runtime data: numbers, strings, symbols, lists, maps, functions,
// macros, host objects, and KindSyntax (a boxed syntax.Form). Quote, unquote,
// splice, and comment live on syntax.Form. Nested quote evaluates to a
// KindSyntax value holding the remaining quote form.
//
// Type is the static type algebra. Check walks already-expanded forms and
// does not evaluate. Eval takes already-parsed forms and does not parse.
//
//	(+ 1 2)
//	(def (add a b) (+ a b))
//	(let [x: 1] (+ x 2))
package writ
