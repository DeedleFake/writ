package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"deedles.dev/writ"
	"deedles.dev/writ/parser"
	"deedles.dev/writ/repl"
	"deedles.dev/writ/runtime"
)

const usage = "usage: writ [repl] [-I DIR] | writ <run|fmt|check> [flags] FILE.writ"

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
	cmd := args[0]
	switch cmd {
	case "run":
		return cmdRun(args[1:])
	case "fmt":
		return cmdFmt(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "repl":
		return cmdRepl(args[1:])
	default:
		if cmd != "" && cmd[0] == '-' {
			return cmdRepl(args)
		}
		return fmt.Errorf("%s", usage)
	}
}

func cmdRepl(args []string) error {
	var search []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-I" && i+1 < len(args) {
			search = append(search, args[i+1])
			i++
			continue
		}
		return fmt.Errorf("repl: unexpected %s", a)
	}
	opts := []writ.Option{writ.WithStdout(os.Stdout)}
	if len(search) > 0 {
		opts = append(opts, writ.WithSearchPath(search...))
	}
	rt := writ.New(opts...)
	rt.RegisterPrint()
	return (repl.REPL{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		RT:  rt,
	}).Run(context.Background())
}

func parseFileAndSearch(args []string) (file string, search []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-I" && i+1 < len(args) {
			search = append(search, args[i+1])
			i++
			continue
		}
		if file == "" && a != "" && a[0] != '-' {
			file = a
			continue
		}
		return "", nil, fmt.Errorf("unexpected %s", a)
	}
	return file, search, nil
}

func cmdRun(args []string) error {
	file, search, err := parseFileAndSearch(args)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}
	if file == "" {
		return fmt.Errorf("writ run FILE.writ")
	}
	opts := []writ.Option{writ.WithStdout(os.Stdout)}
	if len(search) > 0 {
		opts = append(opts, writ.WithSearchPath(search...))
	}
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
	write := false
	var file string
	for _, a := range args {
		if a == "-w" {
			write = true
			continue
		}
		if file == "" && a != "" && a[0] != '-' {
			file = a
			continue
		}
		return fmt.Errorf("fmt: unexpected %s", a)
	}
	if file == "" {
		return fmt.Errorf("writ fmt [-w] FILE.writ")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	text, err := parser.Format(string(data))
	if err != nil {
		return err
	}
	if write {
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
	file, search, err := parseFileAndSearch(args)
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if file == "" {
		return fmt.Errorf("writ check FILE.writ")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	opts := []writ.Option{}
	if len(search) > 0 {
		opts = append(opts, writ.WithSearchPath(search...))
	}
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
