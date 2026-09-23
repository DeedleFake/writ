package writ

import (
	"deedles.dev/writ/syntax"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"
)

// Func is a native function. Arguments are positional.
type Func func(args []Value) (Value, error)

// Macro is a native macro. Arguments are unevaluated forms. The result is
// one form, or a list of forms to splice at the call site.
type Macro func(args []syntax.Form) (syntax.Form, error)

// Package is funcs, macros, and values for (import).
// Types, when set, holds type descriptors keyed by export name. The host
// checker uses them instead of the defaults (funcs as (dynamic any), macros
// as macro, values as any). Authors normally set Types via WithPackageTypes
// / ExportGuestPackage(WithPackageTypes(...)).
type Package struct {
	Funcs  map[string]Func
	Macros map[string]Macro
	Vals   map[string]Value
	Types  map[string]Type
}

// Scheduler runs (after seconds ...) bodies. delay is in real time.
// The default scheduler uses time.AfterFunc; callbacks take the machine
// lock, so they never overlap eval/Fire/after. A custom Scheduler must
// not invoke fn on the same goroutine that is still inside Eval, Apply,
// or Fire (that deadlocks). Overlapping callbacks are serialized by the
// lock. Default unlimited eval is not a sandbox.
type Scheduler func(delay time.Duration, fn func())

// Importer loads (import spec) for the machine. The host wires filesystem
// policy, parsing, and evaluation.
type Importer func(spec, fromFile string) (Value, error)

const maxPendingAfter = 4096

type loadedPkg struct {
	exports  Value
	handlers []handler
	env      *env
	pkg      *Package // wasm / RegisterPackage surface for typed exports
}

// machine evaluates already-parsed forms.
type machine struct {
	mu sync.Mutex

	sched        Scheduler
	evalLimit    int
	budget       int
	onAfterErr   func(error)
	checking     bool
	pendingAfter int

	extra  map[string]Func
	events map[string][]string

	props    *mapData
	env      *env
	handlers []handler
	macros   map[string][]clause
	file     string

	loaded      map[string]*loadedPkg
	loadedOrder []string
	loading     []string

	Import Importer
}

// New constructs a machine.
func newMachine() *machine {
	return &machine{
		sched:  defaultSched,
		extra:  map[string]Func{},
		events: map[string][]string{},
		props:  newMap(),
		loaded: map[string]*loadedPkg{},
	}
}

// Lock serializes eval, Fire, and host callbacks.
func (m *machine) Lock() { m.mu.Lock() }

// Unlock releases the machine lock.
func (m *machine) Unlock() { m.mu.Unlock() }

// BeginBudget resets the remaining eval step budget.
func (m *machine) BeginBudget() {
	if m.evalLimit > 0 {
		m.budget = m.evalLimit
	}
}

// hostCall runs fn without holding mu so host hooks and native
// functions may call host Runtime methods.
func (m *machine) hostCall(fn func() (Value, error)) (Value, error) {
	if m == nil {
		return fn()
	}
	m.mu.Unlock()
	defer m.mu.Lock()
	return fn()
}

func (m *machine) hostCallForm(fn func() (syntax.Form, error)) (syntax.Form, error) {
	if m == nil {
		return fn()
	}
	m.mu.Unlock()
	defer m.mu.Lock()
	return fn()
}

// SetEvalLimit caps eval/expand steps. Zero means unlimited.
func (m *machine) SetEvalLimit(n int) {
	m.evalLimit = n
}

// SetScheduler replaces the after scheduler.
func (m *machine) SetScheduler(s Scheduler) {
	if s == nil {
		s = defaultSched
	}
	m.sched = s
}

// SetAfterError sets a hook for errors from (after ...) callbacks.
func (m *machine) SetAfterError(fn func(error)) {
	m.onAfterErr = fn
}

// SetChecking toggles check mode: after is a no-op and plugins stay closed
// if the host importer honors it.
func (m *machine) SetChecking(v bool) { m.checking = v }

// Checking reports check mode.
func (m *machine) Checking() bool { return m.checking }

// SetFile records the current script path for import resolution.
func (m *machine) SetFile(file string) { m.file = file }

// File returns the current script path.
func (m *machine) File() string { return m.file }

// SetEventKeys records payload key order for Fire.
func (m *machine) SetEventKeys(name string, keys []string) {
	if m.events == nil {
		m.events = map[string][]string{}
	}
	m.events[name] = append([]string{}, keys...)
}

// RegisterExtra installs a host function visible as a call head.
func (m *machine) RegisterExtra(name string, fn Func) {
	if m.extra == nil {
		m.extra = map[string]Func{}
	}
	m.extra[name] = fn
}

func (m *machine) getVar(key string) Value {
	if m.props == nil {
		return Nil
	}
	v, ok := m.props.get(key)
	if !ok {
		return Nil
	}
	return v
}

func (m *machine) setVar(key string, val Value) {
	if m.props == nil {
		m.props = newMap()
	}
	if val.IsNil() {
		m.props.del(key)
		return
	}
	m.props.put(Symbol(key), val)
}

// LookupLocked returns a top-level binding. Caller must hold Lock.
func (m *machine) LookupLocked(name string) (Value, bool) {
	if m.env == nil {
		return Value{}, false
	}
	return m.env.get(name)
}

// GetPropLocked reads the script store. Caller must hold Lock.
func (m *machine) GetPropLocked(path ...string) Value {
	if len(path) == 0 {
		return Nil
	}
	v, _ := getPropPath(m, path, "prop-get")
	return v
}

// SetPropLocked writes the script store. Caller must hold Lock.
func (m *machine) SetPropLocked(val Value, path ...string) error {
	if len(path) == 0 {
		return errMsg("prop-set needs a key")
	}
	_, err := setPropPath(m, path, val, "prop-set")
	return err
}

// ResetLocked clears store, env, macros, handlers, and loaded modules. Caller must hold Lock.
func (m *machine) ResetLocked() {
	m.props = newMap()
	m.env = nil
	m.handlers = nil
	m.macros = nil
	m.loaded = map[string]*loadedPkg{}
	m.loadedOrder = nil
	m.loading = nil
	m.file = ""
	m.pendingAfter = 0
}

// Expand compiles and macro-expands forms. It does not parse source.
func (m *machine) Expand(forms []syntax.Form) (program, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BeginBudget()
	return compileForms(forms, m, false)
}

// ExpandLocked is Expand without taking the lock.
func (m *machine) ExpandLocked(forms []syntax.Form) (program, error) {
	return compileForms(forms, m, false)
}

// Eval evaluates already-parsed forms (boot forms only).
func (m *machine) Eval(forms []syntax.Form) (Value, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BeginBudget()
	return m.EvalLocked(forms)
}

// EvalLocked is Eval without taking the lock.
func (m *machine) EvalLocked(forms []syntax.Form) (Value, error) {
	file := m.file
	prog, err := compileForms(forms, m, true)
	if err != nil {
		return Value{}, asError(err).WithFile(file)
	}
	env := m.env
	if env == nil {
		env = makeEnv(nil)
	}
	installFns(prog.Fns, env)
	m.env = env
	if m.macros == nil {
		m.macros = map[string][]clause{}
	}
	maps.Copy(m.macros, toMacroTable(prog.Macros))
	for i := range prog.Handlers {
		prog.Handlers[i].env = env
	}
	c := newCtx(m, env, m.macros)
	if err := evalNamedImports(prog.Imports, env, c); err != nil {
		return Value{}, asError(err).WithFile(file)
	}
	v, err := evalForms(prog.Boot, env, c)
	m.handlers = append(m.handlers, prog.Handlers...)
	if err != nil {
		return v, asError(err).WithFile(file)
	}
	return v, nil
}

// EvalModule evaluates an imported script without replacing the caller's env.
func (m *machine) EvalModule(path string, forms []syntax.Form) (Value, error) {
	prog, err := compileForms(forms, m, false)
	if err != nil {
		return Value{}, asError(err).WithFile(path)
	}
	env := makeEnv(nil)
	installFns(prog.Fns, env)
	macros := toMacroTable(prog.Macros)
	c := newCtx(m, env, macros)
	c.file = path
	if err := evalNamedImports(prog.Imports, env, c); err != nil {
		return Value{}, asError(err).WithFile(path)
	}
	if _, err := evalForms(prog.Boot, env, c); err != nil {
		return Value{}, asError(err).WithFile(path)
	}
	names := map[string]Value{}
	for _, f := range prog.Fns {
		if !exported(f.Name) {
			continue
		}
		if v, ok := env.get(f.Name); ok {
			names[f.Name] = v
		}
	}
	for _, mac := range prog.Macros {
		if !exported(mac.Name) {
			continue
		}
		if _, ok := names[mac.Name]; ok {
			continue
		}
		names[mac.Name] = makeMacroVal(mac.Name, mac.Clauses, env)
	}
	exp := mapFromNames(names)
	for i := range prog.Handlers {
		prog.Handlers[i].env = env
	}
	m.rememberLoaded(path, &loadedPkg{exports: exp, handlers: prog.Handlers, env: env})
	m.handlers = append(m.handlers, prog.Handlers...)
	return exp, nil
}

// RememberPackage caches a wasm package and its export map. Caller must hold Lock.
func (m *machine) RememberPackage(path string, p Package) {
	cp := p
	m.rememberLoaded(path, &loadedPkg{exports: PackageValue(p), pkg: &cp})
}

// LoadedPackage returns the Package remembered for a wasm import, if any.
func (m *machine) LoadedPackage(path string) (Package, bool) {
	l, ok := m.loaded[path]
	if !ok || l.pkg == nil {
		return Package{}, false
	}
	return *l.pkg, true
}

func (m *machine) rememberLoaded(path string, l *loadedPkg) {
	if _, ok := m.loaded[path]; !ok {
		m.loadedOrder = append(m.loadedOrder, path)
	}
	m.loaded[path] = l
}

func (m *machine) PushLoading(path string) {
	m.loading = append(m.loading, path)
}

func (m *machine) PopLoading() {
	if n := len(m.loading); n > 0 {
		m.loading = m.loading[:n-1]
	}
}

func (m *machine) LoadingCycle(path string) bool {
	return slices.Contains(m.loading, path)
}

func (m *machine) LoadingPath() []string {
	return append([]string{}, m.loading...)
}

func (m *machine) Loaded(path string) (Value, bool) {
	l, ok := m.loaded[path]
	if !ok {
		return Value{}, false
	}
	return l.exports, true
}

// ApplyLocked is Apply without taking the lock.
func (m *machine) ApplyLocked(fn Value, args []Value) (Value, error) {
	c := newCtx(m, m.env, m.macros)
	return applyFn(fn, callParts{pos: args, keys: map[string]Value{}}, makeEnv(m.env), c)
}

// FireLocked is Fire without taking the lock.
func (m *machine) FireLocked(event string, payload map[string]Value) error {
	if payload == nil {
		payload = map[string]Value{}
	}
	keys := map[string]Value{}
	maps.Copy(keys, payload)
	c := newCtx(m, m.env, m.macros)
	var order []string
	spec, haveSpec := m.events[event]
	if haveSpec {
		order = append(order, spec...)
	}
	for _, h := range m.handlers {
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
				parent = m.env
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

func evalImport(args []syntax.Form, env *env, c *ctx) (Value, error) {
	parsed, err := parseCallRaw(syntax.FilterComments(args))
	if err != nil {
		return Value{}, err
	}
	if len(parsed.keys) > 0 {
		return Value{}, errMsg("(import ...) only works at the top of a script")
	}
	if len(parsed.pos) != 1 {
		return Value{}, errMsg("import needs one path")
	}
	v, err := evalVal(parsed.pos[0], env, c)
	if err != nil {
		return Value{}, err
	}
	if v.k != KindString {
		return Value{}, errMsg("import needs a string path")
	}
	if c.rt == nil || c.rt.Import == nil {
		return Value{}, errMsg("import needs a runtime")
	}
	return c.rt.Import(v.s, c.file)
}

func evalNamedImports(imps []namedImport, env *env, c *ctx) error {
	for _, imp := range imps {
		path, err := evalVal(imp.PathForm, env, c)
		if err != nil {
			return err
		}
		if path.k != KindString {
			return errForm(imp.PathForm, "import needs a string path")
		}
		if c.rt == nil || c.rt.Import == nil {
			return errMsg("import needs a runtime")
		}
		pkg, err := c.rt.Import(path.s, c.file)
		if err != nil {
			return err
		}
		env.set(imp.Name, pkg)
	}
	return nil
}

// PackageValue builds the map returned by (import) of a native package.
func PackageValue(p Package) Value {
	names := map[string]Value{}
	for name, f := range p.Funcs {
		fn := f
		names[name] = Value{k: KindFn, p: &fnVal{native: fn, name: name}}
	}
	for name, f := range p.Macros {
		fn := f
		names[name] = Value{k: KindMacro, p: &fnVal{macro: fn, name: name}}
	}
	maps.Copy(names, p.Vals)
	return mapFromNames(names)
}

// exported reports whether a top-level def/defm name is in a module export map.
// Names that start with '-' are private to the defining script.
func exported(name string) bool {
	return name != "" && name[0] != '-'
}

func mapFromNames(names map[string]Value) Value {
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]MapPair, len(keys))
	for i, k := range keys {
		pairs[i] = MapPair{Key: Symbol(k), Value: names[k]}
	}
	return MapFrom(pairs...)
}
