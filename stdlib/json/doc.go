// Package json is a thin bridge between Writ values and JSON text.
//
// JSON → Writ: null→nil; true/false→true/false; string→string;
// number with integer spelling (optional "-" and digits only)→int,
// otherwise float; array→vector list; object→map with symbol keys
// (duplicate keys: last wins). Trailing data after one value is an error.
//
// Writ → JSON: nil→null; true/false→bool; other symbols→error;
// string→string; int→number; float→number (NaN/Inf→error);
// list (vector or call-shaped)→array; map→object with Name() keys;
// fn/macro/native/syntax→error.
//
// Hosts install it with Runtime.RegisterPackage, for example
// rt.RegisterPackage("json", json.Package()).
package json
