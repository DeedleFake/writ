//go:build !js && !wasm

package repl

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"deedles.dev/writ/scanner"
	"github.com/ergochat/readline"
	"golang.org/x/term"
)

func newLineReader(in io.Reader, out, errW io.Writer, history string) (lineReader, error) {
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
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
	cfg := &readline.Config{
		Prompt:          prompt,
		HistoryFile:     history,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
		AutoComplete:    wordCompleter{words: scanner.Words()},
		Stdin:           f,
		Stdout:          out,
		Stderr:          errW,
		FuncIsTerminal: func() bool {
			return term.IsTerminal(int(f.Fd()))
		},
	}
	rl, err := readline.NewFromConfig(cfg)
	if err != nil {
		return newScanReader(in, out), nil
	}
	return &rlReader{rl: rl}, nil
}

func defaultHistoryFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "writ", "history")
}

type rlReader struct {
	rl *readline.Instance
}

func (r *rlReader) ReadLine(prompt string) (string, error) {
	r.rl.SetPrompt(prompt)
	line, err := r.rl.ReadLine()
	if err == readline.ErrInterrupt {
		return "", errInterrupt
	}
	return line, err
}

func (r *rlReader) Close() error {
	return r.rl.Close()
}

// Completes the atom at the cursor, including after '(' or '['.
type wordCompleter struct {
	words []string
}

func (c wordCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos > len(line) {
		pos = len(line)
	}
	start := pos
	for start > 0 && isAtomChar(line[start-1]) {
		start--
	}
	prefix := string(line[start:pos])
	var out [][]rune
	for _, w := range c.words {
		if strings.HasPrefix(w, prefix) {
			out = append(out, []rune(w[len(prefix):]))
		}
	}
	return out, pos - start
}

func isAtomChar(r rune) bool {
	switch r {
	case '(', ')', '[', ']', '\'', ',', '@', '"', ';':
		return false
	}
	return !unicode.IsSpace(r)
}
