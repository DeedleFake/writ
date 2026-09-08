// Package ir is the shared compile IR for Writ.
//
// It holds expanded programs, parameter lists, and parsers for fn/if/def
// heads. runtime evaluates Programs; types type-checks them. Neither package
// imports the other.
package ir
