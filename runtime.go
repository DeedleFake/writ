package writ

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Func is a native function. Arguments are positional.
type Func func(args []Value) (Value, error)

// Package is a native module returned by [Runtime.RegisterPackage] or by
// a plugin's WritPackage function.
type Package struct {
	Funcs map[string]Func
	Vals  map[string]Value
	Types map[string]Type
}

// Scheduler runs (after seconds ...) bodies. delay is in real time.
// The default scheduler uses time.AfterFunc; callbacks take the runtime
// lock, so they never overlap eval/Fire/after. A custom Scheduler must
// not invoke fn on the same goroutine that is still inside Eval, Apply,
// or Fire (that deadlocks). Overlapping callbacks are serialized by the
// lock. Default unlimited eval is not a sandbox.
type Scheduler func(delay time.Duration, fn func())

const maxPendingAfter = 4096

// PayloadKey is one event payload field.
type PayloadKey struct {
	Name string
	Type Type
}

type extraBuiltin struct {
	call   Func
	arrows []Arrow
}

type loadedPkg struct {
	exports  Value
	handlers []handler
	env      *env
	types    Type
}

// Runtime evaluates scripts, holds the script store, and hosts packages.
// Public methods are safe for concurrent use.
type Runtime struct {
	mu sync.Mutex

	search        []string
	sched         Scheduler
	stdout        io.Writer
	evalLimit     int
	budget        int
	onAfterErr    func(error)
	nativePlugins bool
	allowAbsolute bool
	checking      bool
	pendingAfter  int

	pkgs    map[string]Package
	extra   map[string]extraBuiltin
	events  map[string][]PayloadKey
	aliases []typeAlias

	props    *mapData
	env      *env
	handlers []handler
	macros   map[string][]clause
	file     string

	loaded       map[string]*loadedPkg
	loadedOrder  []string
	loading      []string
	checkLoading []string
	exportCache  map[string]cachedExport
}

type cachedExport struct {
	t     Type
	diags []Diagnostic
}

// Option configures a [Runtime].
type Option func(*Runtime)

// WithSearchPath sets directories used to resolve import paths.
func WithSearchPath(paths ...string) Option {
	return func(rt *Runtime) { rt.search = append([]string{}, paths...) }
}

// WithScheduler replaces the default time.AfterFunc scheduler.
func WithScheduler(s Scheduler) Option {
	return func(rt *Runtime) { rt.sched = s }
}

// WithStdout sets the writer used by the print builtin if registered.
func WithStdout(w io.Writer) Option {
	return func(rt *Runtime) { rt.stdout = w }
}

// WithEvalLimit caps eval/expand steps across expand, eval, import, after,
// Fire, and Apply until the next public call resets the remaining budget.
// Zero (the default) means unlimited and is not a sandbox.
func WithEvalLimit(n int) Option {
	return func(rt *Runtime) { rt.evalLimit = n }
}

// WithAfterError sets a hook for errors from (after ...) callbacks.
func WithAfterError(fn func(error)) Option {
	return func(rt *Runtime) { rt.onAfterErr = fn }
}

// WithNativePlugins allows (import) of Go plugin files (.so/.dylib/.dll).
// Off by default. Plugins are never opened during Check.
func WithNativePlugins() Option {
	return func(rt *Runtime) { rt.nativePlugins = true }
}

// WithAllowAbsoluteImports allows absolute import paths and paths that
// leave the importing script directory / search path (including "..").
func WithAllowAbsoluteImports() Option {
	return func(rt *Runtime) { rt.allowAbsolute = true }
}

// New constructs a runtime.
func New(opts ...Option) *Runtime {
	rt := &Runtime{
		sched:       defaultSched,
		stdout:      io.Discard,
		pkgs:        map[string]Package{},
		extra:       map[string]extraBuiltin{},
		events:      map[string][]PayloadKey{},
		props:       newMap(),
		loaded:      map[string]*loadedPkg{},
		exportCache: map[string]cachedExport{},
	}
	for _, o := range opts {
		o(rt)
	}
	return rt
}

func (rt *Runtime) beginBudget() {
	if rt.evalLimit > 0 {
		rt.budget = rt.evalLimit
	}
}

// hostCall runs fn without holding mu so host hooks and native
// functions may call Runtime methods.
func (rt *Runtime) hostCall(fn func() (Value, error)) (Value, error) {
	if rt == nil {
		return fn()
	}
	rt.mu.Unlock()
	defer rt.mu.Lock()
	return fn()
}

// RegisterPackage installs an in-process package. (import "name") loads it
// without touching the filesystem.
func (rt *Runtime) RegisterPackage(name string, pkg Package) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.pkgs == nil {
		rt.pkgs = map[string]Package{}
	}
	rt.pkgs[name] = pkg
}

// RegisterBuiltin adds a host function visible as a call head.
func (rt *Runtime) RegisterBuiltin(name string, call Func, arrows ...Arrow) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.registerBuiltin(name, call, arrows...)
}

func (rt *Runtime) registerBuiltin(name string, call Func, arrows ...Arrow) error {
	if isKeyword(name) {
		return errf("cannot redefine %s", name)
	}
	if isCoreBuiltin(name) {
		return errf("cannot redefine %s", name)
	}
	if rt.extra == nil {
		rt.extra = map[string]extraBuiltin{}
	}
	rt.extra[name] = extraBuiltin{call: call, arrows: arrows}
	return nil
}

// RegisterPrint installs a print builtin that writes to stdout (or
// [WithStdout]).
func (rt *Runtime) RegisterPrint() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	w := rt.stdout
	if w == nil {
		w = os.Stdout
	}
	_ = rt.registerBuiltin("print", func(args []Value) (Value, error) {
		for _, a := range args {
			_, _ = io.WriteString(w, printVal(a))
		}
		_, _ = io.WriteString(w, "\n")
		return Nil, nil
	}, PosRestArrow(NilType()))
}

// RegisterEvent declares an event for (on ...) and [Runtime.Fire].
// If any events are registered, unknown event names are type errors.
func (rt *Runtime) RegisterEvent(name string, keys ...PayloadKey) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.events == nil {
		rt.events = map[string][]PayloadKey{}
	}
	rt.events[name] = append([]PayloadKey{}, keys...)
}

// RegisterAlias names a closed union of exact strings for type display
// and host domain types.
func (rt *Runtime) RegisterAlias(name string, members ...string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.registerAlias(name, members...)
}

func (rt *Runtime) registerAlias(name string, members ...string) {
	ts := make([]Type, len(members))
	for i, m := range members {
		ts[i] = ExactString(m)
	}
	rt.aliases = append(rt.aliases, typeAlias{name: name, t: tOr(ts), members: append([]string{}, members...)})
}

// RegisterTypeAlias names an arbitrary type.
func (rt *Runtime) RegisterTypeAlias(name string, t Type) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.aliases = append(rt.aliases, typeAlias{name: name, t: t})
}

// AliasType returns the type last registered under name.
func (rt *Runtime) AliasType(name string) (Type, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for i := len(rt.aliases) - 1; i >= 0; i-- {
		if rt.aliases[i].name == name {
			return rt.aliases[i].t, true
		}
	}
	return Type{}, false
}

// SetScheduler replaces the after scheduler.
func (rt *Runtime) SetScheduler(s Scheduler) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if s == nil {
		s = defaultSched
	}
	rt.sched = s
}

// SetAfterError sets a hook for errors from (after ...) callbacks.
func (rt *Runtime) SetAfterError(fn func(error)) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.onAfterErr = fn
}

// Lookup returns a top-level binding from the last Eval.
func (rt *Runtime) Lookup(name string) (Value, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.env == nil {
		return Value{}, false
	}
	return rt.env.get(name)
}

// GetProp reads the script store.
func (rt *Runtime) GetProp(path ...string) Value {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(path) == 0 {
		return Nil
	}
	v, _ := getPropPath(rt, path, "get-prop")
	return v
}

// SetProp writes the script store. nil deletes.
func (rt *Runtime) SetProp(val Value, path ...string) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(path) == 0 {
		return errMsg("set-prop needs a key")
	}
	_, err := setPropPath(rt, path, val, "set-prop")
	return err
}

func (rt *Runtime) getVar(key string) Value {
	if rt.props == nil {
		return Nil
	}
	v, ok := rt.props.get(key)
	if !ok {
		return Nil
	}
	return v
}

func (rt *Runtime) setVar(key string, val Value) {
	if rt.props == nil {
		rt.props = newMap()
	}
	if val.IsNil() {
		rt.props.del(key)
		return
	}
	rt.props.put(key, val)
}

// Reset clears the script store, handlers, and import cache.
func (rt *Runtime) Reset() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.props = newMap()
	rt.env = nil
	rt.handlers = nil
	rt.macros = nil
	rt.loaded = map[string]*loadedPkg{}
	rt.loadedOrder = nil
	rt.loading = nil
	rt.exportCache = map[string]cachedExport{}
	rt.file = ""
	rt.pendingAfter = 0
}

// Check type-checks src.
func (rt *Runtime) Check(src string) CheckResult {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	rt.checking = true
	defer func() { rt.checking = false }()
	return rt.checkSrc(src, rt.file)
}

// CheckFile type-checks a file.
func (rt *Runtime) CheckFile(path string) CheckResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckResult{Diagnostics: []Diagnostic{{Message: err.Error()}}}
	}
	abs, _ := filepath.Abs(path)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	rt.checking = true
	prev := rt.file
	rt.file = abs
	defer func() {
		rt.checking = false
		rt.file = prev
	}()
	return rt.checkSrc(string(data), abs)
}

// Eval compiles and evaluates src (boot forms only).
func (rt *Runtime) Eval(src string) (Value, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	return rt.evalSrc(src, rt.file)
}

// EvalFile evaluates a file's boot forms. It does not call main.
func (rt *Runtime) EvalFile(path string) (Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	return rt.evalSrc(string(data), abs)
}

func (rt *Runtime) evalSrc(src, file string) (Value, error) {
	forms, err := Parse(src)
	if err != nil {
		return Value{}, asError(err).withFile(file)
	}
	if file != "" {
		rt.file = file
	}
	prog, err := compileForms(forms, rt)
	if err != nil {
		return Value{}, asError(err).withFile(file)
	}
	env := makeEnv(nil)
	installFns(prog.Fns, env)
	rt.env = env
	rt.macros = toMacroTable(prog.Macros)
	for i := range prog.Handlers {
		prog.Handlers[i].env = env
	}
	c := newCtx(rt, env, rt.macros)
	v, err := evalForms(prog.Boot, env, c)
	var hs []handler
	for _, path := range rt.loadedOrder {
		if l := rt.loaded[path]; l != nil {
			hs = append(hs, l.handlers...)
		}
	}
	hs = append(hs, prog.Handlers...)
	rt.handlers = hs
	if err != nil {
		return v, asError(err).withFile(file)
	}
	return v, nil
}

// Apply calls fn with positional args.
func (rt *Runtime) Apply(fn Value, args []Value) (Value, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	c := newCtx(rt, rt.env, rt.macros)
	return applyFn(fn, callParts{pos: args, keys: map[string]Value{}}, makeEnv(rt.env), c)
}

// Fire runs matching (on event ...) handlers. Missing payload keys are nil.
func (rt *Runtime) Fire(event string, payload map[string]Value) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.beginBudget()
	if payload == nil {
		payload = map[string]Value{}
	}
	keys := map[string]Value{}
	for k, v := range payload {
		keys[k] = v
	}
	c := newCtx(rt, rt.env, rt.macros)
	var order []string
	spec, haveSpec := rt.events[event]
	if haveSpec {
		for _, k := range spec {
			order = append(order, k.Name)
		}
	}
	for _, h := range rt.handlers {
		if h.Event != event {
			continue
		}
		for _, cl := range h.Clauses {
			var call callParts
			if cl.params.Key {
				filtered := map[string]Value{}
				for _, kp := range cl.params.Keys {
					if v, ok := keys[kp.Name]; ok {
						filtered[kp.Name] = v
					}
				}
				call = callParts{keys: filtered}
			} else {
				pos := make([]Value, len(cl.params.Pats))
				for i, p := range cl.params.Pats {
					if haveSpec {
						if i < len(order) {
							if v, ok := payload[order[i]]; ok {
								pos[i] = v
								continue
							}
						}
					} else if p.Bind {
						if v, ok := payload[p.Name]; ok {
							pos[i] = v
							continue
						}
					}
					pos[i] = Nil
				}
				call = callParts{pos: pos, keys: map[string]Value{}}
			}
			parent := h.env
			if parent == nil {
				parent = rt.env
			}
			child := makeEnv(parent)
			if !tryBind(cl.params, call, child) {
				continue
			}
			if _, err := evalForms(cl.Body, child, c); err != nil {
				return err
			}
			break
		}
	}
	return nil
}
