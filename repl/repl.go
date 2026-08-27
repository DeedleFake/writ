// Package repl is an interactive evaluator for Writ source.
package repl

import (
	"bufio"
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

// Options configures [Run].
type Options struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	RT  *writ.Runtime
}

// Run reads forms from In until EOF and evaluates them. One Runtime is
// used for the whole session. Parse and eval errors are written to Err
// and the session continues. EOF returns nil.
func Run(opts Options) error {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errW := opts.Err
	if errW == nil {
		errW = os.Stderr
	}
	rt := opts.RT
	if rt == nil {
		rt = writ.New(writ.WithStdout(out))
		rt.RegisterPrint()
	}

	sc := bufio.NewScanner(in)
	var buf strings.Builder
	cont := false
	for {
		p := prompt
		if cont {
			p = contPrompt
		}
		if _, err := io.WriteString(out, p); err != nil {
			return err
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return err
			}
			if buf.Len() > 0 {
				fmt.Fprintln(errW, "incomplete input")
			}
			_, _ = io.WriteString(out, "\n")
			return nil
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(sc.Text())
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
