// Package runtime is the Writ value universe and evaluator.
//
// A Value is runtime data: numbers, strings, symbols, lists, maps, functions,
// macros, host objects, and KindSyntax (a boxed syntax.Form). Quote, unquote,
// splice, and comment live on syntax.Form. Nested quote evaluates to a
// KindSyntax value holding the remaining quote form.
//
// Eval takes already-parsed forms. It does not parse source.
package runtime
