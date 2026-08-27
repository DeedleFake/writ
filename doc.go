// Package writ is a Lisp: parser, formatter, evaluator, type checker,
// and embedding API.
//
// Integers are arbitrary precision (int64 fast path, math/big otherwise).
// Floats are IEEE float64. Inexact division of large integers is float64
// and may round. true, false, and nil are interned symbols; true and false
// are booleans; nil is not. Eval with the default unlimited step budget is
// not a sandbox. Native plugins are off until WithNativePlugins.
//
//	(+ 1 2)
//	(def (add a b) (+ a b))
//	(let [x: 1] (+ x 2))
package writ
