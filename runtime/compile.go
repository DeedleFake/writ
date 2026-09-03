package runtime

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

// Pattern is one parameter or literal match.
type Pattern struct {
	Bind  bool
	Name  string
	Value Value
}

// Params is a function, macro, or handler parameter list.
type Params struct {
	Key  bool
	Pats []Pattern
	Keys []KeyPat
	Rest string
}

// KeyPat is one keyword parameter.
type KeyPat struct {
	Name string
	Pat  Pattern
}

// Clause is one function, macro, or handler clause.
type Clause struct {
	Params     Params
	Body       []syntax.Form
	ParamsForm *syntax.Form
}

// NamedFn is a top-level def or defm.
type NamedFn struct {
	Name     string
	Clauses  []Clause
	NameForm syntax.Form
}

// Handler is a compiled (on ...) form.
type Handler struct {
	Event   string
	Clauses []Clause
	env     *env
}

// NamedImport is a top-level keyed (import name: path ...).
type NamedImport struct {
	Name     string
	PathForm syntax.Form
	NameForm syntax.Form
}

// Program is expanded top-level forms.
type Program struct {
	Handlers []Handler
	Boot     []syntax.Form
	Fns      []NamedFn
	Macros   []NamedFn
	Imports  []NamedImport
}

type lastAdj struct {
	t    string
	name string
	ok   bool
}

type compileState struct {
	onMap          map[string][]Clause
	fnMap          map[string][]Clause
	fnNameForm     map[string]syntax.Form
	macroMap       map[string][]Clause
	macroNameForm  map[string]syntax.Form
	imports        []NamedImport
	importNames    map[string]struct{}
	seenOther      bool
	boot           []syntax.Form
	bootAfterOther []bool
	last           lastAdj
}

func newCompileState() *compileState {
	return &compileState{
		onMap:         map[string][]Clause{},
		fnMap:         map[string][]Clause{},
		fnNameForm:    map[string]syntax.Form{},
		macroMap:      map[string][]Clause{},
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

func (s *compileState) addImports(imps []NamedImport, rt *Machine, session bool) error {
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

func asKeyedImport(form syntax.Form) ([]NamedImport, bool, error) {
	if form.Kind() != syntax.KindList || form.IsVec() || len(form.Items()) == 0 || !syntax.IsName(form.Items()[0], "import") {
		return nil, false, nil
	}
	items := syntax.FilterComments(form.Items()[1:])
	var out []NamedImport
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
			out = append(out, NamedImport{Name: name, PathForm: items[i+1], NameForm: a})
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

func (s *compileState) addFn(kind, name string, params Params, body []syntax.Form, paramsForm, nameForm syntax.Form, adj bool) error {
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
	if unreachableBy(list, params) {
		return errf("unreachable clause for %s", name)
	}
	list = append(list, Clause{Params: params, Body: body, ParamsForm: &paramsForm})
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
	params, err := parseParams(paramsForm, "on")
	if err != nil {
		return err
	}
	list := s.onMap[name]
	if unreachableBy(list, params) {
		return errf("unreachable clause for %s", name)
	}
	pf := paramsForm
	list = append(list, Clause{Params: params, Body: body, ParamsForm: &pf})
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
func compileForms(forms []syntax.Form, rt *Machine, session bool) (Program, error) {
	s := newCompileState()
	for _, form := range forms {
		if form.Kind() == syntax.KindComment {
			continue
		}
		if imps, ok, err := asKeyedImport(form); err != nil {
			return Program{}, err
		} else if ok {
			if s.seenOther {
				return Program{}, errForm(form, "(import ...) must appear before other top-level forms")
			}
			if err := s.addImports(imps, rt, session); err != nil {
				return Program{}, err
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
				return Program{}, err
			}
			continue
		}
		if got, ok, err := asDefForm(form, "def"); err != nil {
			return Program{}, err
		} else if ok {
			s.seenOther = true
			if err := sessionClash(rt, session, got.Name, false); err != nil {
				return Program{}, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return Program{}, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return Program{}, err
			}
			if _, has := s.macroMap[got.Name]; has {
				return Program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("fn", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return Program{}, err
			}
			continue
		}
		if got, ok, err := asDefForm(form, "defm"); err != nil {
			return Program{}, err
		} else if ok {
			s.seenOther = true
			if err := sessionClash(rt, session, got.Name, true); err != nil {
				return Program{}, err
			}
			if err := sessionDefVsImport(rt, session, got.Name); err != nil {
				return Program{}, err
			}
			if err := s.takenByImport(got.Name); err != nil {
				return Program{}, err
			}
			if _, has := s.fnMap[got.Name]; has {
				return Program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if scanner.IsKeyword(got.Name) || scanner.IsBuiltin(got.Name) {
				return Program{}, errf("cannot redefine %s", got.Name)
			}
			if err := s.addFn("macro", got.Name, got.Params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return Program{}, err
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
	var fns []NamedFn
	for name, clauses := range s.fnMap {
		fns = append(fns, NamedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var macros []NamedFn
	for name, clauses := range s.macroMap {
		macros = append(macros, NamedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	installFns(fns, env)
	macroTable := map[string][]Clause{}
	if session && rt != nil {
		maps.Copy(macroTable, rt.macros)
	}
	maps.Copy(macroTable, toMacroTable(macros))
	c := newCtx(rt, env, macroTable)

	expandBody := func(clauses []Clause) error {
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
		if got, ok, err := asDefForm(form, "def"); err != nil {
			return false, err
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
		if got, ok, err := asDefForm(form, "defm"); err != nil {
			return false, err
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
			params, err := parseParams(paramsForm, "on")
			if err != nil {
				return false, err
			}
			list := s.onMap[ev.Name()]
			if unreachableBy(list, params) {
				return false, errf("unreachable clause for %s", ev.Name())
			}
			pf := paramsForm
			list = append(list, Clause{Params: params, Body: form.Items()[3:], ParamsForm: &pf})
			s.onMap[ev.Name()] = list
			streamOther = true
			return true, nil
		}
		return false, nil
	}

	for i := range s.imports {
		p, err := expandVal(s.imports[i].PathForm, env, c)
		if err != nil {
			return Program{}, err
		}
		s.imports[i].PathForm = p
	}
	if rt != nil && rt.Import != nil {
		if err := evalNamedImports(s.imports, env, c); err != nil {
			return Program{}, err
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
			return Program{}, err
		}
		for _, ex := range xs {
			took, err := takeExpanded(ex)
			if err != nil {
				return Program{}, err
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
			return Program{}, err
		}
		s.fnMap[name] = clauses
	}
	for name, clauses := range s.onMap {
		if err := expandBody(clauses); err != nil {
			return Program{}, err
		}
		s.onMap[name] = clauses
	}

	var handlers []Handler
	for ev, clauses := range s.onMap {
		handlers = append(handlers, Handler{Event: ev, Clauses: clauses, env: env})
	}
	var outFns []NamedFn
	for name, clauses := range s.fnMap {
		outFns = append(outFns, NamedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var outMacros []NamedFn
	for name, clauses := range s.macroMap {
		outMacros = append(outMacros, NamedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	return Program{Handlers: handlers, Boot: newBoot, Fns: outFns, Macros: outMacros, Imports: s.imports}, nil
}

// DefHead is a parsed (def ...) or (defm ...) head.
type DefHead struct {
	Name       string
	NameForm   syntax.Form
	Params     Params
	ParamsForm syntax.Form
	Body       []syntax.Form
	HeadForm   syntax.Form
}

func asDefForm(form syntax.Form, kw string) (DefHead, bool, error) {
	if form.Kind() != syntax.KindList || len(form.Items()) == 0 || !syntax.IsName(form.Items()[0], kw) {
		return DefHead{}, false, nil
	}
	h, err := parseDefHead(form, kw)
	if err != nil {
		return DefHead{}, false, err
	}
	return h, true, nil
}

// AsDefForm reports whether form is a (def ...) or (defm ...) when kw matches.
func AsDefForm(form syntax.Form, kw string) (DefHead, bool, error) {
	return asDefForm(form, kw)
}

func parseDefHead(form syntax.Form, kw string) (DefHead, error) {
	hint := "(" + kw + " (name args...) body)"
	if form.Kind() != syntax.KindList || len(form.Items()) == 0 || !syntax.IsName(form.Items()[0], kw) {
		return DefHead{}, errMsg(hint)
	}
	if len(form.Items()) < 2 {
		return DefHead{}, errMsg(hint)
	}
	head := form.Items()[1]
	if head.Kind() != syntax.KindList || head.IsVec() {
		return DefHead{}, errMsg(hint)
	}
	if len(head.Items()) == 0 {
		return DefHead{}, errMsg(hint + " needs a name")
	}
	nameForm := head.Items()[0]
	if nameForm.Kind() != syntax.KindSymbol || nameForm.Name() == "" || strings.HasSuffix(nameForm.Name(), ":") {
		return DefHead{}, errMsg(hint + " needs a name")
	}
	if nameForm.IsTrue() || nameForm.IsFalse() || nameForm.IsNil() {
		return DefHead{}, errf("cannot redefine %s", nameForm.Name())
	}
	if scanner.IsKeyword(nameForm.Name()) {
		return DefHead{}, errf("cannot redefine %s", nameForm.Name())
	}
	paramsForm := syntax.CallList(head.Items()[1:]...)
	paramsForm = paramsForm.WithItems(head.Items()[1:])
	if head.HasSpan() {
		sp, _ := head.Span()
		paramsForm = paramsForm.WithSpan(sp.Start, sp.End)
	}
	params, err := parseParams(paramsForm, kw)
	if err != nil {
		return DefHead{}, err
	}
	return DefHead{
		Name:       nameForm.Name(),
		NameForm:   nameForm,
		Params:     params,
		ParamsForm: paramsForm,
		Body:       form.Items()[2:],
		HeadForm:   head,
	}, nil
}

func isLit(v syntax.Form) bool {
	return v.Kind() == syntax.KindInt || v.Kind() == syntax.KindFloat || v.Kind() == syntax.KindString || syntax.ReservedLit(v)
}

func asPattern(v syntax.Form) (Pattern, error) {
	if v.Kind() == syntax.KindSymbol {
		if syntax.ReservedLit(v) {
			return Pattern{Value: Symbol(v.Name())}, nil
		}
		if strings.HasSuffix(v.Name(), ":") && len(v.Name()) > 1 {
			return Pattern{}, errMsg("parameter must be a name or a literal")
		}
		if v.Name() == "" {
			return Pattern{}, errMsg("empty parameter name")
		}
		return Pattern{Bind: true, Name: v.Name()}, nil
	}
	if isLit(v) {
		return Pattern{Value: ValueFromLiteralForm(v)}, nil
	}
	return Pattern{}, errMsg("parameter must be a name or a literal")
}

func parseParams(form syntax.Form, ctx string) (Params, error) {
	if form.Kind() != syntax.KindList {
		return Params{}, errf("%s needs a parameter list", ctx)
	}
	if form.IsVec() {
		return Params{}, errf("%s needs a parameter list in (...)", ctx)
	}
	var mode string
	var pos []Pattern
	var keys []KeyPat
	var seen []string
	var rest string
	for i := 0; i < len(form.Items()); {
		p := form.Items()[i]
		if p.Kind() == syntax.KindComment {
			i++
			continue
		}
		if p.Kind() == syntax.KindSplice {
			if ctx != "defm" {
				return Params{}, errMsg("only defm can use @rest")
			}
			if mode == "key" {
				return Params{}, errMsg("do not mix positional and keyword parameters")
			}
			if rest != "" {
				return Params{}, errMsg("only one @rest parameter is allowed")
			}
			inner := p.Inner()
			if inner.Kind() != syntax.KindSymbol || inner.Name() == "" || strings.HasSuffix(inner.Name(), ":") {
				return Params{}, errMsg("@rest needs a name")
			}
			for _, x := range form.Items()[i+1:] {
				if x.Kind() != syntax.KindComment {
					return Params{}, errMsg("@rest must be last")
				}
			}
			mode = "pos"
			rest = inner.Name()
			if containsStr(seen, rest) {
				return Params{}, errf("duplicate parameter %s", rest)
			}
			seen = append(seen, rest)
			i++
			continue
		}
		if rest != "" {
			return Params{}, errMsg("@rest must be last")
		}
		if p.IsKey() {
			if mode == "pos" {
				return Params{}, errMsg("do not mix positional and keyword parameters")
			}
			mode = "key"
			name := p.KeyName()
			if name == "" {
				return Params{}, errMsg("empty parameter name")
			}
			if containsStr(seen, name) {
				return Params{}, errf("duplicate parameter %s", name)
			}
			seen = append(seen, name)
			nextOK := i+1 < len(form.Items()) && !form.Items()[i+1].IsKey() && form.Items()[i+1].Kind() != syntax.KindComment
			if nextOK {
				pat, err := asPattern(form.Items()[i+1])
				if err != nil {
					return Params{}, err
				}
				keys = append(keys, KeyPat{Name: name, Pat: pat})
				i += 2
			} else {
				keys = append(keys, KeyPat{Name: name, Pat: Pattern{Bind: true, Name: name}})
				i++
			}
			continue
		}
		if mode == "key" {
			return Params{}, errMsg("do not mix positional and keyword parameters")
		}
		mode = "pos"
		pat, err := asPattern(p)
		if err != nil {
			return Params{}, err
		}
		pos = append(pos, pat)
		i++
	}
	if mode == "key" {
		return Params{Key: true, Keys: keys}, nil
	}
	return Params{Pats: pos, Rest: rest}, nil
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

func clauseKeys(params Params) []string {
	if !params.Key {
		return nil
	}
	out := make([]string, len(params.Keys))
	for i, k := range params.Keys {
		out[i] = k.Name
	}
	return out
}

func unionKeys(clauses []Clause) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range clauses {
		for _, k := range clauseKeys(c.Params) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func patCovers(earlier Pattern, later *Pattern) bool {
	if earlier.Bind {
		return true
	}
	if later == nil || later.Bind {
		return false
	}
	return earlier.Value.Equal(later.Value)
}

func clauseCovers(earlier, later Params) bool {
	if earlier.Key != later.Key {
		return false
	}
	if !earlier.Key && !later.Key {
		if earlier.Rest == "" {
			if later.Rest != "" {
				return false
			}
			if len(earlier.Pats) != len(later.Pats) {
				return false
			}
			for i := range earlier.Pats {
				lp := later.Pats[i]
				if !patCovers(earlier.Pats[i], &lp) {
					return false
				}
			}
			return true
		}
		if len(later.Pats) < len(earlier.Pats) {
			return false
		}
		for i := range earlier.Pats {
			lp := later.Pats[i]
			if !patCovers(earlier.Pats[i], &lp) {
				return false
			}
		}
		return true
	}
	laterBy := map[string]Pattern{}
	for _, p := range later.Keys {
		laterBy[p.Name] = p.Pat
	}
	earlierNames := map[string]struct{}{}
	for _, p := range earlier.Keys {
		earlierNames[p.Name] = struct{}{}
		lp, ok := laterBy[p.Name]
		var ptr *Pattern
		if ok {
			ptr = &lp
		}
		if !patCovers(p.Pat, ptr) {
			return false
		}
	}
	for _, p := range later.Keys {
		if _, ok := earlierNames[p.Name]; !ok {
			return false
		}
	}
	return true
}

func unreachableBy(prev []Clause, next Params) bool {
	for _, c := range prev {
		if clauseCovers(c.Params, next) {
			return true
		}
	}
	return false
}

func isFnSep(v syntax.Form) bool { return syntax.IsName(v, "fn") }

func isFnCall(v syntax.Form) bool {
	if v.Kind() != syntax.KindList || v.IsVec() {
		return false
	}
	xs := syntax.FilterComments(v.Items())
	return len(xs) > 0 && syntax.IsName(xs[0], "fn")
}

func walkSlots(v syntax.Form, onSlot func(name string, node syntax.Form) error) error {
	switch v.Kind() {
	case syntax.KindComment:
		return nil
	case syntax.KindSymbol:
		if strings.HasPrefix(v.Name(), "#") {
			return onSlot(v.Name(), v)
		}
		return nil
	case syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice:
		return walkSlots(v.Inner(), onSlot)
	case syntax.KindList:
		if isFnCall(v) {
			return nil
		}
		for _, x := range v.Items() {
			if err := walkSlots(x, onSlot); err != nil {
				return err
			}
		}
		return nil
	case syntax.KindMap:
		for _, pair := range v.Pairs() {
			if err := walkSlots(pair.Value, onSlot); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func maxSlot(args []syntax.Form) (int, error) {
	max := 0
	for _, a := range args {
		err := walkSlots(a, func(name string, node syntax.Form) error {
			if !scanner.IsSlot(name) {
				return errFormf(node, "bad slot %s", name)
			}
			n, _ := strconv.Atoi(name[1:])
			if n > max {
				max = n
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return max, nil
}

func hasNestedFn(args []syntax.Form) bool {
	var walk func(syntax.Form) bool
	walk = func(v syntax.Form) bool {
		switch v.Kind() {
		case syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice:
			return walk(v.Inner())
		case syntax.KindList:
			if isFnCall(v) {
				return true
			}
			return slices.ContainsFunc(v.Items(), walk)
		case syntax.KindMap:
			vals := make([]syntax.Form, len(v.Pairs()))
			for i, pair := range v.Pairs() {
				vals[i] = pair.Value
			}
			return slices.ContainsFunc(vals, walk)
		default:
			return false
		}
	}
	return slices.ContainsFunc(args, walk)
}

func slotParams(n int) Params {
	pats := make([]Pattern, n)
	for i := 1; i <= n; i++ {
		pats[i-1] = Pattern{Bind: true, Name: "#" + strconv.Itoa(i)}
	}
	return Params{Pats: pats}
}

func shortFnBody(args []syntax.Form) []syntax.Form {
	xs := syntax.FilterComments(args)
	if len(xs) <= 1 {
		return xs
	}
	return []syntax.Form{syntax.CallList(xs...)}
}

type fnParsed struct {
	kind    string
	clauses []Clause
}

func parseFn(args []syntax.Form) (fnParsed, error) {
	n, err := maxSlot(args)
	if err != nil {
		return fnParsed{}, err
	}
	if n > 0 {
		if hasNestedFn(args) {
			return fnParsed{}, errMsg("a short fn cannot contain another fn")
		}
		return fnParsed{
			kind:    "short",
			clauses: []Clause{{Params: slotParams(n), Body: shortFnBody(args)}},
		}, nil
	}
	if len(args) == 0 {
		return fnParsed{}, errMsg("(fn (args...) body)")
	}
	var clauses []Clause
	i := 0
	for i < len(args) {
		paramsForm := args[i]
		if paramsForm.Kind() != syntax.KindList {
			return fnParsed{}, errMsg("(fn (args...) body)")
		}
		params, err := parseParams(paramsForm, "fn")
		if err != nil {
			return fnParsed{}, err
		}
		i++
		var body []syntax.Form
		for i < len(args) && !isFnSep(args[i]) {
			body = append(body, args[i])
			i++
		}
		if unreachableBy(clauses, params) {
			return fnParsed{}, errMsg("unreachable clause")
		}
		pf := paramsForm
		clauses = append(clauses, Clause{Params: params, Body: body, ParamsForm: &pf})
		if i < len(args) && isFnSep(args[i]) {
			i++
			if i >= len(args) {
				return fnParsed{}, errMsg("fn after fn needs a parameter list")
			}
		}
	}
	return fnParsed{kind: "long", clauses: clauses}, nil
}

// ParseFn parses (fn ...) arguments.
func ParseFn(args []syntax.Form) (kind string, clauses []Clause, err error) {
	parsed, err := parseFn(args)
	if err != nil {
		return "", nil, err
	}
	return parsed.kind, parsed.clauses, nil
}

// IfClause is one branch of (if ...).
type IfClause struct {
	Test *syntax.Form
	Not  bool
	Body []syntax.Form
}

func isElseSym(v syntax.Form) bool { return syntax.IsName(v, "else") }
func isIfSym(v syntax.Form) bool   { return syntax.IsName(v, "if") }
func isNotSym(v syntax.Form) bool  { return syntax.IsName(v, "not") }

func readIfTest(args []syntax.Form, i int, ctx string) (test syntax.Form, not bool, next int, err error) {
	if i >= len(args) {
		return syntax.Form{}, false, 0, errf("%s needs a test", ctx)
	}
	if isNotSym(args[i]) {
		i++
		if i >= len(args) {
			return syntax.Form{}, false, 0, errf("%s not needs a test", ctx)
		}
		return args[i], true, i + 1, nil
	}
	return args[i], false, i + 1, nil
}

func parseIfArgs(args []syntax.Form) ([]IfClause, error) {
	if len(args) == 0 {
		return nil, errMsg("(if test ...)")
	}
	var clauses []IfClause
	test, not, i, err := readIfTest(args, 0, "if")
	if err != nil {
		return nil, err
	}
	curTest, curNot := test, not
	var body []syntax.Form
	for i < len(args) {
		a := args[i]
		if isElseSym(a) {
			t := curTest
			clauses = append(clauses, IfClause{Test: &t, Not: curNot, Body: body})
			i++
			if i < len(args) && isIfSym(args[i]) {
				i++
				test, not, ni, err := readIfTest(args, i, "else if")
				if err != nil {
					return nil, err
				}
				curTest, curNot, i = test, not, ni
				body = nil
				continue
			}
			clauses = append(clauses, IfClause{Body: args[i:]})
			return clauses, nil
		}
		body = append(body, a)
		i++
	}
	t := curTest
	clauses = append(clauses, IfClause{Test: &t, Not: curNot, Body: body})
	return clauses, nil
}

// ParseIfArgs parses (if ...) arguments.
func ParseIfArgs(args []syntax.Form) ([]IfClause, error) {
	return parseIfArgs(args)
}

func containsStr(xs []string, s string) bool {
	return slices.Contains(xs, s)
}

func makeFnVal(clauses []Clause, env *env) Value {
	return Value{k: KindFn, p: &fnVal{clauses: clauses, keys: unionKeys(clauses), env: env}}
}

func makeMacroVal(name string, clauses []Clause, env *env) Value {
	return Value{k: KindMacro, p: &fnVal{clauses: clauses, keys: unionKeys(clauses), env: env, name: name}}
}

func installFns(fns []NamedFn, env *env) {
	for _, f := range fns {
		env.set(f.Name, makeFnVal(f.Clauses, env))
	}
}

func toMacroTable(macros []NamedFn) map[string][]Clause {
	m := map[string][]Clause{}
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
