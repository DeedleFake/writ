package runtime

import (
	"maps"
	"strings"

	"deedles.dev/writ/ir"
	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

type lastAdj struct {
	t    string
	name string
	ok   bool
}

type compileState struct {
	onMap          map[string][]ir.Clause
	fnMap          map[string][]ir.Clause
	fnNameForm     map[string]syntax.Form
	macroMap       map[string][]ir.Clause
	macroNameForm  map[string]syntax.Form
	imports        []ir.NamedImport
	importNames    map[string]struct{}
	seenOther      bool
	boot           []syntax.Form
	bootAfterOther []bool
	last           lastAdj
}

func newCompileState() *compileState {
	return &compileState{
		onMap:         map[string][]ir.Clause{},
		fnMap:         map[string][]ir.Clause{},
		fnNameForm:    map[string]syntax.Form{},
		macroMap:      map[string][]ir.Clause{},
		macroNameForm: map[string]syntax.Form{},
		importNames:   map[string]struct{}{},
	}
}

func (s *compileState) breakAdj() { s.last.ok = false }

func importFnClash(name string) error {
	return errf("%s cannot be both an import and a function", name)
}

func importMacroClash(name string) error {
	return errf("%s cannot be both an import and a macro", name)
}

func alreadyImport(name string) error {
	return errf("%s is already bound as an import", name)
}

func (s *compileState) takenByImport(name string) error {
	if _, has := s.importNames[name]; has {
		return alreadyImport(name)
	}
	return nil
}

func sessionImportBinding(rt *Machine, session bool, name string) error {
	if !session || rt == nil {
		return nil
	}
	if _, has := rt.macros[name]; has {
		return importMacroClash(name)
	}
	if rt.env != nil {
		if v, ok := rt.env.get(name); ok {
			if v.k == KindFn {
				return importFnClash(name)
			}
			return alreadyImport(name)
		}
	}
	return nil
}

func sessionDefVsImport(rt *Machine, session bool, name string) error {
	if !session || rt == nil || rt.env == nil {
		return nil
	}
	if v, ok := rt.env.get(name); ok && v.k != KindFn {
		return alreadyImport(name)
	}
	return nil
}

func (s *compileState) addImports(imps []ir.NamedImport, rt *Machine, session bool) error {
	for _, imp := range imps {
		if scanner.IsKeyword(imp.Name) || scanner.IsBuiltin(imp.Name) {
			return errf("cannot redefine %s", imp.Name)
		}
		if _, has := s.importNames[imp.Name]; has {
			return errf("duplicate %s:", imp.Name)
		}
		if _, has := s.fnMap[imp.Name]; has {
			return importFnClash(imp.Name)
		}
		if _, has := s.macroMap[imp.Name]; has {
			return importMacroClash(imp.Name)
		}
		if err := sessionImportBinding(rt, session, imp.Name); err != nil {
			return err
		}
		s.importNames[imp.Name] = struct{}{}
		s.imports = append(s.imports, imp)
	}
	s.breakAdj()
	return nil
}

func asKeyedImport(form syntax.Form) ([]ir.NamedImport, bool, error) {
	if form.Kind() != syntax.KindList || form.IsVec() || len(form.Items()) == 0 || !syntax.IsName(form.Items()[0], "import") {
		return nil, false, nil
	}
	items := syntax.FilterComments(form.Items()[1:])
	var out []ir.NamedImport
	keyed := false
	pos := 0
	i := 0
	for i < len(items) {
		a := items[i]
		if a.IsKey() {
			keyed = true
			name := a.KeyName()
			if i+1 >= len(items) {
				return nil, false, errf("missing value for %s", a.Name())
			}
			for _, prev := range out {
				if prev.Name == name {
					return nil, false, errf("duplicate %s", a.Name())
				}
			}
			out = append(out, ir.NamedImport{Name: name, PathForm: items[i+1], NameForm: a})
			i += 2
			continue
		}
		if keyed {
			return nil, false, errMsg("positional argument after key:")
		}
		pos++
		i++
	}
	if !keyed {
		return nil, false, nil
	}
	if pos > 0 {
		return nil, false, errMsg("do not mix positional and keyword parameters")
	}
	if len(out) == 0 {
		return nil, false, errMsg("import needs one path")
	}
	return out, true, nil
}

func (s *compileState) addFn(kind, name string, params ir.Params, body []syntax.Form, paramsForm, nameForm syntax.Form, adj bool) error {
	mp := s.fnMap
	if kind == "macro" {
		mp = s.macroMap
	}
	if adj {
		if _, has := mp[name]; has && (!s.last.ok || s.last.t != kind || s.last.name != name) {
			who := "def"
			if kind == "macro" {
				who = "defm"
			}
			return errf("(%s %s ...) must sit next to the last (%s %s ...)", who, name, who, name)
		}
	}
	list := mp[name]
	if ir.UnreachableBy(list, params) {
		return errf("unreachable clause for %s", name)
	}
	list = append(list, ir.Clause{Params: params, Body: body, ParamsForm: &paramsForm})
	mp[name] = list
	s.last = lastAdj{t: kind, name: name, ok: true}
	if kind == "fn" {
		s.fnNameForm[name] = nameForm
	} else {
		s.macroNameForm[name] = nameForm
	}
	return nil
}

func (s *compileState) addOn(ev syntax.Form, paramsForm syntax.Form, body []syntax.Form) error {
	if ev.Kind() != syntax.KindSymbol {
		return errMsg("(on event (args...) body) needs an event name")
	}
	if paramsForm.Kind() != syntax.KindList || paramsForm.IsVec() {
		return errMsg("(on event (args...) body) needs a parameter list")
	}
	name := ev.Name()
	if _, has := s.onMap[name]; has && (!s.last.ok || s.last.t != "on" || s.last.name != name) {
		return errf("(on %s ...) must sit next to the last (on %s ...)", name, name)
	}
	params, err := ir.ParseParams(paramsForm, "on")
	if err != nil {
		return irErr(err)
	}
	list := s.onMap[name]
	if ir.UnreachableBy(list, params) {
		return errf("unreachable clause for %s", name)
	}
	pf := paramsForm
	list = append(list, ir.Clause{Params: params, Body: body, ParamsForm: &pf})
	s.onMap[name] = list
	s.last = lastAdj{t: "on", name: name, ok: true}
	return nil
}

func sessionClash(rt *Machine, session bool, name string, asMacro bool) error {
	if !session || rt == nil {
		return nil
	}
	if asMacro {
		if rt.env != nil {
			if v, ok := rt.env.get(name); ok && v.k == KindFn {
				return errf("%s cannot be both a function and a macro", name)
			}
		}
		return nil
	}
	if _, has := rt.macros[name]; has {
		return errf("%s cannot be both a function and a macro", name)
	}
	return nil
}

// session is true for Eval so later lines expand with macros from earlier Evals.
func compileForms(forms []syntax.Form, rt *Machine, session bool) (ir.Program, error) {
	s := newCompileState()
	for _, form := range forms {
		if form.Kind() == syntax.KindComment {
			continue
		}
		if imps, ok, err := asKeyedImport(form); err != nil {
			return ir.Program{}, err
		} else if ok {
			if s.seenOther {
				return ir.Program{}, errForm(form, "(import ...) must appear before other top-level forms")
			}
			if err := s.addImports(imps, rt, session); err != nil {
				return ir.Program{}, err
			}
			continue
		}
		if form.Kind() == syntax.KindList && !form.IsVec() && len(form.Items()) > 0 && syntax.IsName(form.Items()[0], "on") {
			s.seenOther = true
			var ev, paramsForm syntax.Form
			if len(form.Items()) > 1 {
				ev = form.Items()[1]
			}
			if len(form.Items()) > 2 {
				paramsForm = form.Items()[2]
			}
			if err := s.addOn(ev, paramsForm, form.Items()[3:]); err != nil {
				return ir.Program{}, err
			}
			continue
		}
		if got, ok, err := ir.AsDefForm(form, "def"); err != nil {
			return ir.Program{}, irErr(err)
		} else if ok {
			s.seenOther = true
			if err := sessionClash(rt, session, got.Name, false); err != nil {
				return ir.Program{}, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return ir.Program{}, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return ir.Program{}, err
			}
			if _, has := s.macroMap[got.Name]; has {
				return ir.Program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("fn", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return ir.Program{}, err
			}
			continue
		}
		if got, ok, err := ir.AsDefForm(form, "defm"); err != nil {
			return ir.Program{}, irErr(err)
		} else if ok {
			s.seenOther = true
			if err := sessionClash(rt, session, got.Name, true); err != nil {
				return ir.Program{}, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return ir.Program{}, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return ir.Program{}, err
			}
			if _, has := s.fnMap[got.Name]; has {
				return ir.Program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if scanner.IsKeyword(got.Name) || scanner.IsBuiltin(got.Name) {
				return ir.Program{}, errf("cannot redefine %s", got.Name)
			}
			if err := s.addFn("macro", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return ir.Program{}, err
			}
			continue
		}
		s.bootAfterOther = append(s.bootAfterOther, s.seenOther)
		s.seenOther = true
		s.breakAdj()
		s.boot = append(s.boot, form)
	}

	env := makeEnv(nil)
	if session && rt != nil && rt.env != nil {
		env = makeEnv(rt.env)
	}
	var fns []ir.NamedFn
	for name, clauses := range s.fnMap {
		fns = append(fns, ir.NamedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var macros []ir.NamedFn
	for name, clauses := range s.macroMap {
		macros = append(macros, ir.NamedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	installFns(fns, env)
	macroTable := map[string][]ir.Clause{}
	if session && rt != nil {
		maps.Copy(macroTable, rt.macros)
	}
	maps.Copy(macroTable, toMacroTable(macros))
	c := newCtx(rt, env, macroTable)

	expandBody := func(clauses []ir.Clause) error {
		for i := range clauses {
			body, err := expandForms(clauses[i].Body, env, c)
			if err != nil {
				return err
			}
			clauses[i].Body = body
		}
		return nil
	}

	streamOther := false
	takeExpanded := func(form syntax.Form) (bool, error) {
		if imps, ok, err := asKeyedImport(form); err != nil {
			return false, err
		} else if ok {
			if streamOther {
				return false, errForm(form, "(import ...) must appear before other top-level forms")
			}
			for i := range imps {
				p, err := expandVal(imps[i].PathForm, env, c)
				if err != nil {
					return false, err
				}
				imps[i].PathForm = p
			}
			if err := s.addImports(imps, rt, session); err != nil {
				return false, err
			}
			return true, nil
		}
		if got, ok, err := ir.AsDefForm(form, "def"); err != nil {
			return false, irErr(err)
		} else if ok {
			if err := sessionClash(rt, session, got.Name, false); err != nil {
				return false, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return false, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return false, err
			}
			if _, has := macroTable[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if _, has := s.macroMap[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("fn", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, false); err != nil {
				return false, err
			}
			env.set(got.Name, makeFnVal(s.fnMap[got.Name], env))
			streamOther = true
			return true, nil
		}
		if got, ok, err := ir.AsDefForm(form, "defm"); err != nil {
			return false, irErr(err)
		} else if ok {
			if err := sessionClash(rt, session, got.Name, true); err != nil {
				return false, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return false, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return false, err
			}
			if _, has := s.fnMap[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("macro", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, false); err != nil {
				return false, err
			}
			macroTable[got.Name] = s.macroMap[got.Name]
			streamOther = true
			return true, nil
		}
		if form.Kind() == syntax.KindList && !form.IsVec() && len(form.Items()) > 0 && syntax.IsName(form.Items()[0], "on") {
			var ev, paramsForm syntax.Form
			if len(form.Items()) > 1 {
				ev = form.Items()[1]
			}
			if len(form.Items()) > 2 {
				paramsForm = form.Items()[2]
			}
			if ev.Kind() != syntax.KindSymbol {
				return false, errMsg("(on event (args...) body) needs an event name")
			}
			if paramsForm.Kind() != syntax.KindList || paramsForm.IsVec() {
				return false, errMsg("(on event (args...) body) needs a parameter list")
			}
			params, err := ir.ParseParams(paramsForm, "on")
			if err != nil {
				return false, irErr(err)
			}
			list := s.onMap[ev.Name()]
			if ir.UnreachableBy(list, params) {
				return false, errf("unreachable clause for %s", ev.Name())
			}
			pf := paramsForm
			list = append(list, ir.Clause{Params: params, Body: form.Items()[3:], ParamsForm: &pf})
			s.onMap[ev.Name()] = list
			streamOther = true
			return true, nil
		}
		return false, nil
	}

	for i := range s.imports {
		p, err := expandVal(s.imports[i].PathForm, env, c)
		if err != nil {
			return ir.Program{}, err
		}
		s.imports[i].PathForm = p
	}
	if rt != nil && rt.Import != nil {
		if err := evalNamedImports(s.imports, env, c); err != nil {
			return ir.Program{}, err
		}
	}

	var newBoot []syntax.Form
	s.last.ok = false
	for i, form := range s.boot {
		if i < len(s.bootAfterOther) && s.bootAfterOther[i] {
			streamOther = true
		}
		xs, err := expandForms([]syntax.Form{form}, env, c)
		if err != nil {
			return ir.Program{}, err
		}
		for _, ex := range xs {
			took, err := takeExpanded(ex)
			if err != nil {
				return ir.Program{}, err
			}
			if took {
				continue
			}
			streamOther = true
			newBoot = append(newBoot, ex)
		}
	}
	for name, clauses := range s.fnMap {
		if err := expandBody(clauses); err != nil {
			return ir.Program{}, err
		}
		s.fnMap[name] = clauses
	}
	for name, clauses := range s.onMap {
		if err := expandBody(clauses); err != nil {
			return ir.Program{}, err
		}
		s.onMap[name] = clauses
	}

	var handlers []ir.Handler
	for ev, clauses := range s.onMap {
		handlers = append(handlers, ir.Handler{Event: ev, Clauses: clauses})
	}
	var outFns []ir.NamedFn
	for name, clauses := range s.fnMap {
		outFns = append(outFns, ir.NamedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var outMacros []ir.NamedFn
	for name, clauses := range s.macroMap {
		outMacros = append(outMacros, ir.NamedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	return ir.Program{Handlers: handlers, Boot: newBoot, Fns: outFns, Macros: outMacros, Imports: s.imports}, nil
}

type callRaw struct {
	pos  []syntax.Form
	keys []struct {
		name string
		raw  syntax.Form
	}
}

func parseCallRaw(raw []syntax.Form) (callRaw, error) {
	var out callRaw
	keyed := false
	for i := 0; i < len(raw); {
		a := raw[i]
		if a.IsKey() {
			keyed = true
			name := a.KeyName()
			if i+1 >= len(raw) {
				return callRaw{}, errf("missing value for %s", a.Name())
			}
			for _, k := range out.keys {
				if k.name == name {
					return callRaw{}, errf("duplicate %s", a.Name())
				}
			}
			out.keys = append(out.keys, struct {
				name string
				raw  syntax.Form
			}{name, raw[i+1]})
			i += 2
			continue
		}
		if keyed {
			return callRaw{}, errMsg("positional argument after key:")
		}
		out.pos = append(out.pos, a)
		i++
	}
	return out, nil
}

func makeFnVal(clauses []ir.Clause, env *env) Value {
	return Value{k: KindFn, p: &fnVal{clauses: clauses, keys: ir.UnionKeys(clauses), env: env}}
}

func makeMacroVal(name string, clauses []ir.Clause, env *env) Value {
	return Value{k: KindMacro, p: &fnVal{clauses: clauses, keys: ir.UnionKeys(clauses), env: env, name: name}}
}

func installFns(fns []ir.NamedFn, env *env) {
	for _, f := range fns {
		env.set(f.Name, makeFnVal(f.Clauses, env))
	}
}

func toMacroTable(macros []ir.NamedFn) map[string][]ir.Clause {
	m := map[string][]ir.Clause{}
	for _, x := range macros {
		m[x.Name] = x.Clauses
	}
	return m
}

// env is a lexical environment.
type env struct {
	parent *env
	vars   map[string]Value
}

func makeEnv(parent *env) *env {
	return &env{parent: parent, vars: map[string]Value{}}
}

func (e *env) get(name string) (Value, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return Value{}, false
}

func (e *env) set(name string, v Value) {
	e.vars[name] = v
}

func displayName(name string) string {
	if strings.ContainsAny(name, "\n\r") || len(name) > 80 {
		return "(invalid name)"
	}
	return name
}

func lookup(env *env, name string) (Value, error) {
	v, ok := env.get(name)
	if !ok {
		return Value{}, errf("unknown name: %s", displayName(name))
	}
	return v, nil
}

func irErr(err error) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*ir.Error); ok {
		return &Error{Start: e.Start, End: e.End, Message: e.Message}
	}
	return err
}
