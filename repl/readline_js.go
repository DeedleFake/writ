//go:build js && wasm

package repl

import "io"

func newLineReader(in io.Reader, out, _ io.Writer, _ string) (lineReader, error) {
	return newScanReader(in, out), nil
}
