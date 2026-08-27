// Package repl is an interactive evaluator for Writ source.
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"deedles.dev/writ"
	"deedles.dev/writ/parser"
	"deedles.dev/writ/runtime"
)

const (
	prompt     = "> "
	contPrompt = "... "
)

// REPL evaluates forms from In until EOF or the context is cancelled.
type REPL struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	RT  *writ.Runtime
	// HistoryFile is the readline history path. Empty uses the user
	// config directory. Readline is used only when In is a terminal.
	HistoryFile string
}

// Run reads forms until EOF or ctx is done. One Runtime is used for the
// session. Parse and eval errors are written to Err and the session
// continues. EOF returns nil. Cancelled ctx returns ctx.Err().
func (r REPL) Run(ctx context.Context) error {
	in := r.In
	if in == nil {
		in = os.Stdin
	}
	out := r.Out
	if out == nil {
		out = os.Stdout
	}
	errW := r.Err
	if errW == nil {
		errW = os.Stderr
	}
	rt := r.RT
	if rt == nil {
		rt = writ.New(writ.WithStdout(out))
		rt.RegisterPrint()
	}

	rd, err := newLineReader(in, out, errW, r.HistoryFile)
	if err != nil {
		return err
	}
	defer rd.Close()

	stop := context.AfterFunc(ctx, func() { _ = rd.Close() })
	defer stop()

	var buf strings.Builder
	cont := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := prompt
		if cont {
			p = contPrompt
		}
		line, err := rd.ReadLine(p)
		if err != nil {
			if errors.Is(err, errInterrupt) {
				buf.Reset()
				cont = false
				continue
			}
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				if buf.Len() > 0 {
					fmt.Fprintln(errW, "incomplete input")
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				_, _ = io.WriteString(out, "\n")
				return nil
			}
			return err
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
		src := buf.String()
		forms, err := parser.Parse(src)
		if err != nil {
			if parser.Incomplete(err) {
				cont = true
				continue
			}
			fmt.Fprintln(errW, err)
			buf.Reset()
			cont = false
			continue
		}
		buf.Reset()
		cont = false
		if skipResult(forms) {
			continue
		}
		v, err := rt.Eval(src)
		if err != nil {
			fmt.Fprintln(errW, err)
			continue
		}
		fmt.Fprintln(out, runtime.Print(v))
	}
}

func skipResult(forms []runtime.Value) bool {
	for _, f := range forms {
		if f.Kind() != runtime.KindComment {
			return false
		}
	}
	return true
}

type lineReader interface {
	ReadLine(prompt string) (string, error)
	Close() error
}

var errInterrupt = errors.New("interrupt")

type scanReader struct {
	sc  *bufio.Scanner
	out io.Writer
}

func (s *scanReader) ReadLine(prompt string) (string, error) {
	if _, err := io.WriteString(s.out, prompt); err != nil {
		return "", err
	}
	if !s.sc.Scan() {
		if err := s.sc.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return s.sc.Text(), nil
}

func (s *scanReader) Close() error { return nil }

func newScanReader(in io.Reader, out io.Writer) *scanReader {
	return &scanReader{sc: bufio.NewScanner(in), out: out}
}
