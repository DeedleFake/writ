package runtime

import (
	"sort"
	"sync"
	"time"
)

// Func is a native function. Arguments are positional.
type Func func(args []Value) (Value, error)

// Package is funcs and values for (import). The checker types funcs as
// fn(...) -> dynamic(any()) and values as any().
type Package struct {
	Funcs map[string]Func
	Vals  map[string]Value
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
	handlers []Handler
	env      *env
}

// Machine evaluates already-parsed forms.
type Machine struct {
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
	handlers []Handler
	macros   map[string][]Clause
	file     string

	loaded      map[string]*loadedPkg
	loadedOrder []string
	loading     []string

	Import Importer
}

// New constructs a machine.
func New() *Machine {
	return &Machine{
		sched:  defaultSched,
		extra:  map[string]Func{},
		events: map[string][]string{},
		props:  newMap(),
		loaded: map[string]*loadedPkg{},
	}
}

// Lock serializes eval, Fire, and host callbacks.
func (m *Machine) Lock() { m.mu.Lock() }

// Unlock releases the machine lock.
func (m *Machine) Unlock() { m.mu.Unlock() }

// BeginBudget resets the remaining eval step budget.
func (m *Machine) BeginBudget() {
	if m.evalLimit > 0 {
		m.budget = m.evalLimit
	}
}

// hostCall runs fn without holding mu so host hooks and native
// functions may call host Runtime methods.
func (m *Machine) hostCall(fn func() (Value, error)) (Value, error) {
	if m == nil {
		return fn()
	}
	m.mu.Unlock()
	defer m.mu.Lock()
	return fn()
}

// SetEvalLimit caps eval/expand steps. Zero means unlimited.
func (m *Machine) SetEvalLimit(n int) {
	m.evalLimit = n
}

// SetScheduler replaces the after scheduler.
func (m *Machine) SetScheduler(s Scheduler) {
	if s == nil {
		s = defaultSched
	}
	m.sched = s
}

// SetAfterError sets a hook for errors from (after ...) callbacks.
func (m *Machine) SetAfterError(fn func(error)) {
	m.onAfterErr = fn
}

// SetChecking toggles check mode: after is a no-op and plugins stay closed
// if the host importer honors it.
func (m *Machine) SetChecking(v bool) { m.checking = v }

// Checking reports check mode.
func (m *Machine) Checking() bool { return m.checking }

// SetFile records the current script path for import resolution.
func (m *Machine) SetFile(file string) { m.file = file }

// File returns the current script path.
func (m *Machine) File() string { return m.file }

// SetEventKeys records payload key order for Fire.
func (m *Machine) SetEventKeys(name string, keys []string) {
	if m.events == nil {
		m.events = map[string][]string{}
	}
	m.events[name] = append([]string{}, keys...)
}

// RegisterExtra installs a host function visible as a call head.
func (m *Machine) RegisterExtra(name string, fn Func) {
	if m.extra == nil {
		m.extra = map[string]Func{}
	}
	m.extra[name] = fn
}

func (m *Machine) getVar(key string) Value {
	if m.props == nil {
		return Nil
	}
	v, ok := m.props.get(key)
	if !ok {
		return Nil
	}
	return v
}

func (m *Machine) setVar(key string, val Value) {
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
func (m *Machine) LookupLocked(name string) (Value, bool) {
	if m.env == nil {
		return Value{}, false
	}
	return m.env.get(name)
}

// GetPropLocked reads the script store. Caller must hold Lock.
func (m *Machine) GetPropLocked(path ...string) Value {
	if len(path) == 0 {
		return Nil
	}
	v, _ := getPropPath(m, path, "get-prop")
	return v
}

// SetPropLocked writes the script store. Caller must hold Lock.
func (m *Machine) SetPropLocked(val Value, path ...string) error {
	if len(path) == 0 {
		return errMsg("set-prop needs a key")
	}
	_, err := setPropPath(m, path, val, "set-prop")
	return err
}

// ResetLocked clears store, env, macros, handlers, and loaded modules. Caller must hold Lock.
func (m *Machine) ResetLocked() {
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
func (m *Machine) Expand(forms []Value) (Program, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BeginBudget()
	return compileForms(forms, m, false)
}

// ExpandLocked is Expand without taking the lock.
func (m *Machine) ExpandLocked(forms []Value) (Program, error) {
	return compileForms(forms, m, false)
}

// Eval evaluates already-parsed forms (boot forms only).
func (m *Machine) Eval(forms []Value) (Value, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BeginBudget()
	return m.EvalLocked(forms)
}

// EvalLocked is Eval without taking the lock.
func (m *Machine) EvalLocked(forms []Value) (Value, error) {
	file := m.file
	prog, err := compileForms(forms, m, true)
	if err != nil {
		return Value{}, asError(err).withFile(file)
	}
	env := m.env
	if env == nil {
		env = makeEnv(nil)
	}
	installFns(prog.Fns, env)
	m.env = env
	if m.macros == nil {
		m.macros = map[string][]Clause{}
	}
	for name, clauses := range toMacroTable(prog.Macros) {
		m.macros[name] = clauses
	}
	for i := range prog.Handlers {
		prog.Handlers[i].env = env
	}
	c := newCtx(m, env, m.macros)
	if err := evalNamedImports(prog.Imports, env, c); err != nil {
		return Value{}, asError(err).withFile(file)
	}
	v, err := evalForms(prog.Boot, env, c)
	m.handlers = append(m.handlers, prog.Handlers...)
	if err != nil {
		return v, asError(err).withFile(file)
	}
	return v, nil
}

// EvalModule evaluates an imported script without replacing the caller's env.
func (m *Machine) EvalModule(path string, forms []Value) (Value, error) {
	prog, err := compileForms(forms, m, false)
	if err != nil {
		return Value{}, asError(err).withFile(path)
	}
	env := makeEnv(nil)
	installFns(prog.Fns, env)
	macros := toMacroTable(prog.Macros)
	c := newCtx(m, env, macros)
	c.file = path
	if err := evalNamedImports(prog.Imports, env, c); err != nil {
		return Value{}, asError(err).withFile(path)
	}
	if _, err := evalForms(prog.Boot, env, c); err != nil {
		return Value{}, asError(err).withFile(path)
	}
	names := map[string]Value{}
	for _, f := range prog.Fns {
		if !Exported(f.Name) {
			continue
		}
		if v, ok := env.get(f.Name); ok {
			names[f.Name] = v
		}
	}
	for _, mac := range prog.Macros {
		if !Exported(mac.Name) {
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

// RememberPackage caches a native plugin export. Caller must hold Lock.
func (m *Machine) RememberPackage(path string, exp Value) {
	m.rememberLoaded(path, &loadedPkg{exports: exp})
}

func (m *Machine) rememberLoaded(path string, l *loadedPkg) {
	if _, ok := m.loaded[path]; !ok {
		m.loadedOrder = append(m.loadedOrder, path)
	}
	m.loaded[path] = l
}

func (m *Machine) PushLoading(path string) {
	m.loading = append(m.loading, path)
}

func (m *Machine) PopLoading() {
	if n := len(m.loading); n > 0 {
		m.loading = m.loading[:n-1]
	}
}

func (m *Machine) LoadingCycle(path string) bool {
	for _, p := range m.loading {
		if p == path {
			return true
		}
	}
	return false
}

func (m *Machine) LoadingPath() []string {
	return append([]string{}, m.loading...)
}

func (m *Machine) Loaded(path string) (Value, bool) {
	l, ok := m.loaded[path]
	if !ok {
		return Value{}, false
	}
	return l.exports, true
}

// ApplyLocked is Apply without taking the lock.
func (m *Machine) ApplyLocked(fn Value, args []Value) (Value, error) {
	c := newCtx(m, m.env, m.macros)
	return applyFn(fn, callParts{pos: args, keys: map[string]Value{}}, makeEnv(m.env), c)
}

// FireLocked is Fire without taking the lock.
func (m *Machine) FireLocked(event string, payload map[string]Value) error {
	if payload == nil {
		payload = map[string]Value{}
	}
	keys := map[string]Value{}
	for k, v := range payload {
		keys[k] = v
	}
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
			if cl.Params.Key {
				filtered := map[string]Value{}
				for _, kp := range cl.Params.Keys {
					if v, ok := keys[kp.Name]; ok {
						filtered[kp.Name] = v
					}
				}
				call = callParts{keys: filtered}
			} else {
				pos := make([]Value, len(cl.Params.Pats))
				for i, p := range cl.Params.Pats {
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
			if !tryBind(cl.Params, call, child) {
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

func evalImport(args []Value, env *env, c *ctx) (Value, error) {
	parsed, err := parseCallRaw(filterComments(args))
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

func evalNamedImports(imps []NamedImport, env *env, c *ctx) error {
	for _, imp := range imps {
		path, err := evalVal(imp.PathForm, env, c)
		if err != nil {
			return err
		}
		if path.k != KindString {
			return errVal(imp.PathForm, "import needs a string path")
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
	for name, v := range p.Vals {
		names[name] = v
	}
	return mapFromNames(names)
}

// Exported reports whether a top-level def/defm name is in a module export map.
// Names that start with '-' are private to the defining script.
func Exported(name string) bool {
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
