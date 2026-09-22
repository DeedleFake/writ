// Package syntax is Writ source forms and shared source-span errors.
//
// A Form is parse-tree syntax: literals, lists, maps, and quote/unquote/
// splice/comment markers. It is not a runtime value. Functions, macros, and
// host objects live on writ.Value after evaluation.
//
// Error is the shared parse/type/eval error type with optional byte spans.
package syntax
