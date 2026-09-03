// Package syntax is Writ source forms.
//
// A Form is parse-tree syntax: literals, lists, maps, and quote/unquote/
// splice/comment markers. It is not a runtime value. Functions, macros, and
// host objects live on runtime.Value after evaluation.
package syntax
