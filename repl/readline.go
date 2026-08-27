//go:build !js && !wasm

package repl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"deedles.dev/writ/scanner"
	"github.com/peterh/liner"
	"golang.org/x/term"
)

func newLineReader(in io.Reader, out, _ io.Writer, history string) (lineReader, error) {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(inFile.Fd())) || !term.IsTerminal(int(outFile.Fd())) {
		return newScanReader(in, out), nil
	}
	if history == "" {
		history = defaultHistoryFile()
	}
	if history != "" {
		if err := os.MkdirAll(filepath.Dir(history), 0o700); err != nil {
			history = ""
		}
	}
	s := liner.NewLiner()
	s.SetCtrlCAborts(true)
	s.SetWordCompleter(completeWord)
	s.SetTabCompletionStyle(liner.TabPrints)
	if history != "" {
		if f, err := os.Open(history); err == nil {
			_, _ = s.ReadHistory(f)
			_ = f.Close()
		}
	}
	return &linerReader{s: s, history: history}, nil
}

func defaultHistoryFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "writ", "history")
}

type linerReader struct {
	s       *liner.State
	history string
}

func (r *linerReader) ReadLine(prompt string) (string, error) {
	line, err := r.s.Prompt(prompt)
	if err == liner.ErrPromptAborted {
		return "", errInterrupt
	}
	if err == nil && strings.TrimSpace(line) != "" {
		r.s.AppendHistory(line)
	}
	return line, err
}

func (r *linerReader) Close() error {
	if r.history != "" {
		if f, err := os.Create(r.history); err == nil {
			_, _ = r.s.WriteHistory(f)
			_ = f.Close()
		}
	}
	return r.s.Close()
}

func completeWord(line string, pos int) (head string, comps []string, tail string) {
	if pos > len(line) {
		pos = len(line)
	}
	start := pos
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if !isAtomChar(r) {
			break
		}
		start -= size
	}
	prefix := line[start:pos]
	head = line[:start]
	tail = line[pos:]
	for _, w := range scanner.Words() {
		if strings.HasPrefix(w, prefix) {
			comps = append(comps, w)
		}
	}
	return head, comps, tail
}

func isAtomChar(r rune) bool {
	switch r {
	case '(', ')', '[', ']', '\'', ',', '@', '"', ';':
		return false
	}
	return !unicode.IsSpace(r)
}
