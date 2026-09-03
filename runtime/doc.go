// Package runtime is the Writ value universe and evaluator.
//
// A Value is runtime data: numbers, strings, symbols, lists, maps, functions,
// macros, and host objects. Quote, unquote, splice, and comment are not
// values; they live on syntax.Form and disappear during evaluation. Nested
// quote residual data is a call list (quote …), not a quote-kind value.
//
// Eval takes already-parsed forms. It does not parse source.
package runtime
