package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"deedles.dev/writ"
	"deedles.dev/writ/parser"
	"deedles.dev/writ/repl"
	"deedles.dev/writ/runtime"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return cmdRepl(nil)
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		return cmdHelp(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "fmt":
		return cmdFmt(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "repl":
		return cmdRepl(args[1:])
	default:
		if args[0] != "" && args[0][0] == '-' {
			return cmdRepl(args)
		}
		return fmt.Errorf("writ %s: unknown command; run 'writ help' for usage", args[0])
	}
}

const rootHelp = `Writ is a Lisp interpreter.

Usage:

	writ <command> [arguments]

The commands are:

	repl        start an interactive session
	run         evaluate a script
	fmt         format a script
	check       type-check a script

Use "writ help <command>" for more information about a command.

With no arguments, writ starts a REPL.
`

var commandHelp = map[string]string{
	"repl": `usage: writ repl [-I directory]

Repl starts an interactive session. With no command, writ also starts a REPL.

The -I flag adds a directory to the import search path. It may be repeated.

When stdin and stdout are a terminal, the REPL uses line editing, history,
and tab completion. Ctrl+C cancels the current line.
`,
	"run": `usage: writ run [-I directory] FILE.writ

Run evaluates FILE.writ, then calls main if it was defined.

The -I flag adds a directory to the import search path. It may be repeated.
`,
	"fmt": `usage: writ fmt [-w] FILE.writ

Fmt writes FILE.writ in canonical form to stdout.

The -w flag writes the result back to FILE.writ instead of stdout.
`,
	"check": `usage: writ check [-I directory] FILE.writ

Check type-checks FILE.writ and prints diagnostics.

The -I flag adds a directory to the import search path. It may be repeated.
`,
}

func cmdHelp(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stdout, rootHelp)
		return nil
	}
	switch args[0] {
	case "-h", "-help", "--help":
		fmt.Fprint(os.Stdout, rootHelp)
		return nil
	}
	text, ok := commandHelp[args[0]]
	if !ok {
		return fmt.Errorf("writ help %s: unknown help topic; run 'writ help'", args[0])
	}
	fmt.Fprint(os.Stdout, text)
	return nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string, topic string) (ok bool, err error) {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(os.Stdout, commandHelp[topic])
			return false, nil
		}
		return false, fmt.Errorf("%v; run 'writ help %s' for usage", err, topic)
	}
	return true, nil
}

func addSearch(fs *flag.FlagSet) *[]string {
	var dirs []string
	fs.Func("I", "search `directory` for imports", func(s string) error {
		dirs = append(dirs, s)
		return nil
	})
	return &dirs
}

func withSearch(opts []writ.Option, search []string) []writ.Option {
	if len(search) > 0 {
		opts = append(opts, writ.WithSearchPath(search...))
	}
	return opts
}

func cmdRepl(args []string) error {
	fs := newFlagSet("repl")
	search := addSearch(fs)
	ok, err := parseFlags(fs, args, "repl")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("repl: unexpected %s; run 'writ help repl' for usage", fs.Arg(0))
	}
	opts := withSearch([]writ.Option{writ.WithStdout(os.Stdout)}, *search)
	rt := writ.New(opts...)
	rt.RegisterPrint()
	return (repl.REPL{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		RT:  rt,
	}).Run(context.Background())
}

func cmdRun(args []string) error {
	fs := newFlagSet("run")
	search := addSearch(fs)
	ok, err := parseFlags(fs, args, "run")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: writ run [-I directory] FILE.writ; run 'writ help run' for usage")
	}
	file := fs.Arg(0)
	opts := withSearch([]writ.Option{writ.WithStdout(os.Stdout)}, *search)
	rt := writ.New(opts...)
	rt.RegisterPrint()
	if _, err := rt.EvalFile(file); err != nil {
		return err
	}
	if mainFn, ok := rt.Lookup("main"); ok && mainFn.Kind() == runtime.KindFn {
		_, err := rt.Apply(mainFn, nil)
		return err
	}
	return nil
}

func cmdFmt(args []string) error {
	fs := newFlagSet("fmt")
	write := fs.Bool("w", false, "write result to FILE.writ instead of stdout")
	ok, err := parseFlags(fs, args, "fmt")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: writ fmt [-w] FILE.writ; run 'writ help fmt' for usage")
	}
	file := fs.Arg(0)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	text, err := parser.Format(string(data))
	if err != nil {
		return err
	}
	if *write {
		return writeFileAtomic(file, text)
	}
	_, err = os.Stdout.WriteString(text)
	return err
}

func writeFileAtomic(path, text string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink: %s", path)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".writ-fmt-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.WriteString(tmp, text); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func cmdCheck(args []string) error {
	fs := newFlagSet("check")
	search := addSearch(fs)
	ok, err := parseFlags(fs, args, "check")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: writ check [-I directory] FILE.writ; run 'writ help check' for usage")
	}
	file := fs.Arg(0)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	opts := withSearch(nil, *search)
	rt := writ.New(opts...)
	rt.RegisterPrint()
	res := rt.CheckFile(file)
	src := string(data)
	for _, d := range res.Diagnostics {
		line, col := offsetLineCol(src, d.Start)
		fmt.Fprintf(os.Stderr, "%s:%d:%d: %s\n", file, line, col, d.Message)
	}
	if len(res.Diagnostics) > 0 {
		os.Exit(1)
	}
	return nil
}

func offsetLineCol(src string, off int) (line, col int) {
	line, col = 1, 1
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
