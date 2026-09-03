package writ

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"deedles.dev/writ/parser"
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/scanner"
	"deedles.dev/writ/types"
)

// Runtime evaluates scripts, holds the script store, and hosts packages.
// Public methods are safe for concurrent use.
type Runtime struct {
	m *runtime.Machine

	search        []string
	stdout        io.Writer
	allowAbsolute bool

	pkgs        map[string]runtime.Package
	extraArrows map[string][]types.Arrow
	events      map[string][]types.PayloadKey
	aliases     []types.Alias

	exportCache  map[string]cachedExport
	checkLoading []string
}

type cachedExport struct {
	t     types.Type
	diags []types.Diagnostic
}

// Option configures a [Runtime].
type Option func(*Runtime)

// WithSearchPath sets directories used to resolve import paths.
func WithSearchPath(paths ...string) Option {
	return func(rt *Runtime) { rt.search = append([]string{}, paths...) }
}

// WithScheduler replaces the default time.AfterFunc scheduler.
func WithScheduler(s runtime.Scheduler) Option {
	return func(rt *Runtime) { rt.m.SetScheduler(s) }
}

// WithStdout sets the writer used by the print builtin if registered.
func WithStdout(w io.Writer) Option {
	return func(rt *Runtime) { rt.stdout = w }
}

// WithEvalLimit caps eval/expand steps across expand, eval, import, after,
// Fire, and Apply until the next public call resets the remaining budget.
// Zero (the default) means unlimited and is not a sandbox.
func WithEvalLimit(n int) Option {
	return func(rt *Runtime) { rt.m.SetEvalLimit(n) }
}

// WithAfterError sets a hook for errors from (after ...) callbacks.
func WithAfterError(fn func(error)) Option {
	return func(rt *Runtime) { rt.m.SetAfterError(fn) }
}

// WithAllowAbsoluteImports allows absolute import paths and paths that
// leave the importing script directory / search path (including "..").
func WithAllowAbsoluteImports() Option {
	return func(rt *Runtime) { rt.allowAbsolute = true }
}

// New constructs a runtime.
func New(opts ...Option) *Runtime {
	rt := &Runtime{
		m:           runtime.New(),
		stdout:      io.Discard,
		pkgs:        map[string]runtime.Package{},
		extraArrows: map[string][]types.Arrow{},
		events:      map[string][]types.PayloadKey{},
		exportCache: map[string]cachedExport{},
	}
	rt.m.Import = rt.loadImport
	for _, o := range opts {
		o(rt)
	}
	return rt
}

// RegisterPackage installs an in-process package. (import "name") loads it
// without touching the filesystem.
func (rt *Runtime) RegisterPackage(name string, pkg runtime.Package) {
	rt.m.Lock()
	defer rt.m.Unlock()
	if rt.pkgs == nil {
		rt.pkgs = map[string]runtime.Package{}
	}
	rt.pkgs[name] = pkg
}

// RegisterBuiltin adds a host function visible as a call head.
func (rt *Runtime) RegisterBuiltin(name string, call runtime.Func, arrows ...types.Arrow) error {
	rt.m.Lock()
	defer rt.m.Unlock()
	return rt.registerBuiltin(name, call, arrows...)
}

func (rt *Runtime) registerBuiltin(name string, call runtime.Func, arrows ...types.Arrow) error {
	if scanner.IsKeyword(name) {
		return runtime.Errorf("cannot redefine %s", name)
	}
	if scanner.IsCoreBuiltin(name) {
		return runtime.Errorf("cannot redefine %s", name)
	}
	rt.m.RegisterExtra(name, call)
	if rt.extraArrows == nil {
		rt.extraArrows = map[string][]types.Arrow{}
	}
	rt.extraArrows[name] = arrows
	return nil
}

// RegisterPrint installs a print builtin that writes to stdout (or
// [WithStdout]).
func (rt *Runtime) RegisterPrint() {
	rt.m.Lock()
	defer rt.m.Unlock()
	w := rt.stdout
	if w == nil {
		w = os.Stdout
	}
	_ = rt.registerBuiltin("print", func(args []runtime.Value) (runtime.Value, error) {
		for _, a := range args {
			_, _ = io.WriteString(w, runtime.Print(a))
		}
		_, _ = io.WriteString(w, "\n")
		return runtime.Nil, nil
	}, types.PosRestArrow(types.NilType()))
}

// RegisterEvent declares an event for (on ...) and [Runtime.Fire].
// If any events are registered, unknown event names are type errors.
func (rt *Runtime) RegisterEvent(name string, keys ...types.PayloadKey) {
	rt.m.Lock()
	defer rt.m.Unlock()
	if rt.events == nil {
		rt.events = map[string][]types.PayloadKey{}
	}
	rt.events[name] = append([]types.PayloadKey{}, keys...)
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.Name
	}
	rt.m.SetEventKeys(name, names)
}

// RegisterAlias names a closed union of exact strings for type display
// and host domain types.
func (rt *Runtime) RegisterAlias(name string, members ...string) {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.registerAlias(name, members...)
}

func (rt *Runtime) registerAlias(name string, members ...string) {
	ts := make([]types.Type, len(members))
	for i, m := range members {
		ts[i] = types.ExactString(m)
	}
	rt.aliases = append(rt.aliases, types.Alias{Name: name, Type: types.Union(ts...), Members: append([]string{}, members...)})
}

// RegisterTypeAlias names an arbitrary type.
func (rt *Runtime) RegisterTypeAlias(name string, t types.Type) {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.aliases = append(rt.aliases, types.Alias{Name: name, Type: t})
}

// AliasType returns the type last registered under name.
func (rt *Runtime) AliasType(name string) (types.Type, bool) {
	rt.m.Lock()
	defer rt.m.Unlock()
	for _, v := range slices.Backward(rt.aliases) {
		if v.Name == name {
			return v.Type, true
		}
	}
	return types.Type{}, false
}

// SetScheduler replaces the after scheduler.
func (rt *Runtime) SetScheduler(s runtime.Scheduler) {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.SetScheduler(s)
}

// SetAfterError sets a hook for errors from (after ...) callbacks.
func (rt *Runtime) SetAfterError(fn func(error)) {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.SetAfterError(fn)
}

// Lookup returns a top-level binding.
func (rt *Runtime) Lookup(name string) (runtime.Value, bool) {
	rt.m.Lock()
	defer rt.m.Unlock()
	return rt.m.LookupLocked(name)
}

// GetProp reads the script store.
func (rt *Runtime) GetProp(path ...string) runtime.Value {
	rt.m.Lock()
	defer rt.m.Unlock()
	return rt.m.GetPropLocked(path...)
}

// SetProp writes the script store. nil deletes.
func (rt *Runtime) SetProp(val runtime.Value, path ...string) error {
	rt.m.Lock()
	defer rt.m.Unlock()
	return rt.m.SetPropLocked(val, path...)
}

// Reset clears the script store, top-level env, macros, handlers, and import cache.
func (rt *Runtime) Reset() {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.ResetLocked()
	rt.exportCache = map[string]cachedExport{}
	rt.checkLoading = nil
}

// Check type-checks source from r.
func (rt *Runtime) Check(r io.Reader) types.CheckResult {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	rt.m.SetChecking(true)
	defer rt.m.SetChecking(false)
	return rt.checkSrc(r, rt.m.File())
}

// CheckFile type-checks a file.
func (rt *Runtime) CheckFile(path string) types.CheckResult {
	f, err := os.Open(path)
	if err != nil {
		return types.CheckResult{Diagnostics: []types.Diagnostic{{Message: err.Error()}}}
	}
	defer f.Close()
	abs, _ := filepath.Abs(path)
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	rt.m.SetChecking(true)
	prev := rt.m.File()
	rt.m.SetFile(abs)
	defer func() {
		rt.m.SetChecking(false)
		rt.m.SetFile(prev)
	}()
	return rt.checkSrc(f, abs)
}

// Eval compiles and evaluates source from r (boot forms only). Defs, macros, and
// handlers persist on this runtime until [Runtime.Reset]; later Eval
// calls expand using those macros. Redefining a function replaces it
// (clauses are not merged across calls). A name cannot be both a
// function and a macro. (on ...) handlers accumulate across Eval calls.
func (rt *Runtime) Eval(r io.Reader) (runtime.Value, error) {
	forms, err := parser.Parse(r)
	if err != nil {
		rt.m.Lock()
		file := rt.m.File()
		rt.m.Unlock()
		return runtime.Value{}, parseErr(err, file)
	}
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	return rt.m.EvalLocked(forms)
}

// EvalFile evaluates a file's boot forms. It does not call main.
// Persistence is the same as [Runtime.Eval]; call [Runtime.Reset]
// before reloading a file if (on ...) handlers should not stack.
func (rt *Runtime) EvalFile(path string) (runtime.Value, error) {
	f, err := os.Open(path)
	if err != nil {
		return runtime.Value{}, err
	}
	defer f.Close()
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	forms, err := parser.Parse(f)
	if err != nil {
		return runtime.Value{}, parseErr(err, abs)
	}
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	rt.m.SetFile(abs)
	return rt.m.EvalLocked(forms)
}

func parseErr(err error, file string) error {
	var e *runtime.Error
	if errors.As(err, &e) {
		return e.WithFile(file)
	}
	return err
}

// Apply calls fn with positional args.
func (rt *Runtime) Apply(fn runtime.Value, args []runtime.Value) (runtime.Value, error) {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	return rt.m.ApplyLocked(fn, args)
}

// Fire runs matching (on event ...) handlers. Missing payload keys are nil.
func (rt *Runtime) Fire(event string, payload map[string]runtime.Value) error {
	rt.m.Lock()
	defer rt.m.Unlock()
	rt.m.BeginBudget()
	return rt.m.FireLocked(event, payload)
}

func (rt *Runtime) typeConfig(file string) types.Config {
	return types.Config{
		Events:  rt.events,
		Aliases: rt.aliases,
		Extra:   rt.extraArrows,
		File:    file,
		Import:  rt.importType,
	}
}

func (rt *Runtime) checkSrc(r io.Reader, file string) types.CheckResult {
	if file != "" {
		if slices.Contains(rt.checkLoading, file) {
			return types.CheckResult{Diagnostics: []types.Diagnostic{{Message: "import cycle: " + file}}}
		}
		rt.checkLoading = append(rt.checkLoading, file)
		defer func() { rt.checkLoading = rt.checkLoading[:len(rt.checkLoading)-1] }()
	}
	parsed, err := parser.Parse(r)
	if err != nil {
		e := runtime.AsError(err)
		end := e.End
		if end == 0 {
			end = e.Start + 1
		}
		return types.CheckResult{Diagnostics: []types.Diagnostic{{Start: e.Start, End: end, Message: e.Message}}}
	}
	prog, err := rt.m.ExpandLocked(parsed)
	if err != nil {
		e := runtime.AsError(err)
		end := e.End
		if end <= e.Start {
			end = e.Start + 1
		}
		return types.CheckResult{Diagnostics: []types.Diagnostic{{Start: e.Start, End: end, Message: e.Message}}}
	}
	res := types.Check(parsed, prog, rt.typeConfig(file))
	if file != "" {
		cycle := false
		for _, d := range res.Diagnostics {
			if strings.Contains(d.Message, "cycle") {
				cycle = true
				break
			}
		}
		if !cycle {
			if rt.exportCache == nil {
				rt.exportCache = map[string]cachedExport{}
			}
			rt.exportCache[file] = cachedExport{t: res.Export, diags: res.Diagnostics}
		}
	}
	return res
}

// Check type-checks r with a fresh runtime. print is registered so
// scripts that use it type-check the same way as `writ check`.
func Check(r io.Reader) types.CheckResult {
	rt := New()
	rt.RegisterPrint()
	return rt.Check(r)
}
