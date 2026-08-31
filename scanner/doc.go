// Package scanner tokenizes Writ source for the parser and for editors.
//
// New returns a stepped scanner. Editors call SetHighlight(true) so
// Next and Peek remap kinds for highlighting without changing spans,
// Text, or token count.
package scanner
