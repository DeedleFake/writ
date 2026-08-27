package writ

import (
	"strconv"
	"strings"
)

type pattern struct {
	Bind  bool
	Name  string
	Value Value
}

type params struct {
	Key  bool
	Pats []pattern
	Keys []keyPat
	Rest string
}

type keyPat struct {
	Name string
	Pat  pattern
}

type clause struct {
	params     params
	Body       []Value
	ParamsForm *Value
}

type namedFn struct {
	Name     string
	Clauses  []clause
	NameForm Value
}

type handler struct {
	Event   string
	Clauses []clause
	env     *env
}

type program struct {
	Handlers []handler
	Boot     []Value
	Fns      []namedFn
	Macros   []namedFn
}

type lastAdj struct {
	t    string
	name string
	ok   bool
}

type compileState struct {
	onMap         map[string][]clause
	fnMap         map[string][]clause
	fnNameForm    map[string]Value
	macroMap      map[string][]clause
	macroNameForm map[string]Value
	boot          []Value
	last          lastAdj
}

func newCompileState() *compileState {
	return &compileState{
		onMap:         map[string][]clause{},
		fnMap:         map[string][]clause{},
		fnNameForm:    map[string]Value{},
		macroMap:      map[string][]clause{},
		macroNameForm: map[string]Value{},
	}
}

func (s *compileState) breakAdj() { s.last.ok = false }

func (s *compileState) addFn(kind, name string, params params, body []Value, paramsForm, nameForm Value, adj bool) error {
	mp := s.fnMap
	if kind == "macro" {
		mp = s.macroMap
	}
	if adj {
		if _, has := mp[name]; has && !(s.last.ok && s.last.t == kind && s.last.name == name) {
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
	list = append(list, clause{params: params, Body: body, ParamsForm: &paramsForm})
	mp[name] = list
	s.last = lastAdj{t: kind, name: name, ok: true}
	if kind == "fn" {
		s.fnNameForm[name] = nameForm
	} else {
		s.macroNameForm[name] = nameForm
	}
	return nil
}

func (s *compileState) addOn(ev Value, paramsForm Value, body []Value) error {
	if ev.k != KindSymbol {
		return errMsg("(on event (args...) body) needs an event name")
	}
	if paramsForm.k != KindList || paramsForm.vec {
		return errMsg("(on event (args...) body) needs a parameter list")
	}
	if _, has := s.onMap[ev.s]; has && !(s.last.ok && s.last.t == "on" && s.last.name == ev.s) {
		return errf("(on %s ...) must sit next to the last (on %s ...)", ev.s, ev.s)
	}
	params, err := parseParams(paramsForm, "on")
	if err != nil {
		return err
	}
	list := s.onMap[ev.s]
	if unreachableBy(list, params) {
		return errf("unreachable clause for %s", ev.s)
	}
	pf := paramsForm
	list = append(list, clause{params: params, Body: body, ParamsForm: &pf})
	s.onMap[ev.s] = list
	s.last = lastAdj{t: "on", name: ev.s, ok: true}
	return nil
}

func compileForms(forms []Value, rt *Runtime) (program, error) {
	s := newCompileState()
	for _, form := range forms {
		if form.k == KindComment {
			continue
		}
		if form.k == KindList && !form.vec && len(form.xs) > 0 && isSymName(form.xs[0], "on") {
			var ev, paramsForm Value
			if len(form.xs) > 1 {
				ev = form.xs[1]
			}
			if len(form.xs) > 2 {
				paramsForm = form.xs[2]
			}
			if err := s.addOn(ev, paramsForm, form.xs[3:]); err != nil {
				return program{}, err
			}
			continue
		}
		if got, ok, err := asDefForm(form, "def"); err != nil {
			return program{}, err
		} else if ok {
			if _, has := s.macroMap[got.Name]; has {
				return program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("fn", got.Name, got.params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return program{}, err
			}
			continue
		}
		if got, ok, err := asDefForm(form, "defm"); err != nil {
			return program{}, err
		} else if ok {
			if _, has := s.fnMap[got.Name]; has {
				return program{}, errf("%s cannot be both a function and a macro", got.Name)
			}
			if isKeyword(got.Name) || isBuiltinName(got.Name) {
				return program{}, errf("cannot redefine %s", got.Name)
			}
			if err := s.addFn("macro", got.Name, got.params, got.Body, got.ParamsForm, got.NameForm, true); err != nil {
				return program{}, err
			}
			continue
		}
		s.breakAdj()
		s.boot = append(s.boot, form)
	}

	env := makeEnv(nil)
	var fns []namedFn
	for name, clauses := range s.fnMap {
		fns = append(fns, namedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var macros []namedFn
	for name, clauses := range s.macroMap {
		macros = append(macros, namedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	installFns(fns, env)
	macroTable := toMacroTable(macros)
	c := newCtx(rt, env, macroTable)

	expandBody := func(clauses []clause) error {
		for i := range clauses {
			body := make([]Value, len(clauses[i].Body))
			for j, b := range clauses[i].Body {
				ex, err := expandVal(b, env, c)
				if err != nil {
					return err
				}
				body[j] = ex
			}
			clauses[i].Body = body
		}
		return nil
	}

	takeExpanded := func(form Value) (bool, error) {
		if got, ok, err := asDefForm(form, "def"); err != nil {
			return false, err
		} else if ok {
			if _, has := macroTable[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if _, has := s.macroMap[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("fn", got.Name, got.params, got.Body, got.ParamsForm, got.NameForm, false); err != nil {
				return false, err
			}
			env.set(got.Name, makeFnVal(s.fnMap[got.Name], env))
			return true, nil
		}
		if got, ok, err := asDefForm(form, "defm"); err != nil {
			return false, err
		} else if ok {
			if _, has := s.fnMap[got.Name]; has {
				return false, errf("%s cannot be both a function and a macro", got.Name)
			}
			if err := s.addFn("macro", got.Name, got.params, got.Body, got.ParamsForm, got.NameForm, false); err != nil {
				return false, err
			}
			macroTable[got.Name] = s.macroMap[got.Name]
			return true, nil
		}
		if form.k == KindList && !form.vec && len(form.xs) > 0 && isSymName(form.xs[0], "on") {
			var ev, paramsForm Value
			if len(form.xs) > 1 {
				ev = form.xs[1]
			}
			if len(form.xs) > 2 {
				paramsForm = form.xs[2]
			}
			if ev.k != KindSymbol {
				return false, errMsg("(on event (args...) body) needs an event name")
			}
			if paramsForm.k != KindList || paramsForm.vec {
				return false, errMsg("(on event (args...) body) needs a parameter list")
			}
			params, err := parseParams(paramsForm, "on")
			if err != nil {
				return false, err
			}
			list := s.onMap[ev.s]
			if unreachableBy(list, params) {
				return false, errf("unreachable clause for %s", ev.s)
			}
			pf := paramsForm
			list = append(list, clause{params: params, Body: form.xs[3:], ParamsForm: &pf})
			s.onMap[ev.s] = list
			return true, nil
		}
		return false, nil
	}

	var newBoot []Value
	s.last.ok = false
	for _, form := range s.boot {
		ex, err := expandVal(form, env, c)
		if err != nil {
			return program{}, err
		}
		took, err := takeExpanded(ex)
		if err != nil {
			return program{}, err
		}
		if took {
			continue
		}
		newBoot = append(newBoot, ex)
	}
	for name, clauses := range s.fnMap {
		if err := expandBody(clauses); err != nil {
			return program{}, err
		}
		s.fnMap[name] = clauses
	}
	for name, clauses := range s.onMap {
		if err := expandBody(clauses); err != nil {
			return program{}, err
		}
		s.onMap[name] = clauses
	}

	var handlers []handler
	for ev, clauses := range s.onMap {
		handlers = append(handlers, handler{Event: ev, Clauses: clauses, env: env})
	}
	var outFns []namedFn
	for name, clauses := range s.fnMap {
		outFns = append(outFns, namedFn{Name: name, Clauses: clauses, NameForm: s.fnNameForm[name]})
	}
	var outMacros []namedFn
	for name, clauses := range s.macroMap {
		outMacros = append(outMacros, namedFn{Name: name, Clauses: clauses, NameForm: s.macroNameForm[name]})
	}
	return program{Handlers: handlers, Boot: newBoot, Fns: outFns, Macros: outMacros}, nil
}

type defHead struct {
	Name       string
	NameForm   Value
	params     params
	ParamsForm Value
	Body       []Value
	HeadForm   Value
}

func asDefForm(form Value, kw string) (defHead, bool, error) {
	if form.k != KindList || len(form.xs) == 0 || !isSymName(form.xs[0], kw) {
		return defHead{}, false, nil
	}
	h, err := parseDefHead(form, kw)
	if err != nil {
		return defHead{}, false, err
	}
	return h, true, nil
}

func parseDefHead(form Value, kw string) (defHead, error) {
	hint := "(" + kw + " (name args...) body)"
	if form.k != KindList || len(form.xs) == 0 || !isSymName(form.xs[0], kw) {
		return defHead{}, errMsg(hint)
	}
	if len(form.xs) < 2 {
		return defHead{}, errMsg(hint)
	}
	head := form.xs[1]
	if head.k != KindList || head.vec {
		return defHead{}, errMsg(hint)
	}
	if len(head.xs) == 0 {
		return defHead{}, errMsg(hint + " needs a name")
	}
	nameForm := head.xs[0]
	if nameForm.k != KindSymbol || nameForm.s == "" || strings.HasSuffix(nameForm.s, ":") {
		return defHead{}, errMsg(hint + " needs a name")
	}
	if nameForm.s == "true" || nameForm.s == "false" || nameForm.s == "nil" {
		return defHead{}, errf("cannot redefine %s", nameForm.s)
	}
	paramsForm := CallList(head.xs[1:]...)
	paramsForm.xs = head.xs[1:]
	if head.hasSpan {
		paramsForm = paramsForm.withSpan(head.span.Start, head.span.End)
	}
	params, err := parseParams(paramsForm, kw)
	if err != nil {
		return defHead{}, err
	}
	return defHead{
		Name:       nameForm.s,
		NameForm:   nameForm,
		params:     params,
		ParamsForm: paramsForm,
		Body:       form.xs[2:],
		HeadForm:   head,
	}, nil
}

func isLit(v Value) bool {
	return v.k == KindInt || v.k == KindFloat || v.k == KindString || reservedLit(v)
}

func asPattern(v Value) (pattern, error) {
	if v.k == KindSymbol {
		if reservedLit(v) {
			return pattern{Value: Symbol(v.s)}, nil
		}
		if strings.HasSuffix(v.s, ":") && len(v.s) > 1 {
			return pattern{}, errMsg("parameter must be a name or a literal")
		}
		if v.s == "" {
			return pattern{}, errMsg("empty parameter name")
		}
		return pattern{Bind: true, Name: v.s}, nil
	}
	if isLit(v) {
		return pattern{Value: v}, nil
	}
	return pattern{}, errMsg("parameter must be a name or a literal")
}

func parseParams(form Value, ctx string) (params, error) {
	if form.k != KindList {
		return params{}, errf("%s needs a parameter list", ctx)
	}
	if form.vec {
		return params{}, errf("%s needs a parameter list in (...)", ctx)
	}
	var mode string
	var pos []pattern
	var keys []keyPat
	var seen []string
	var rest string
	for i := 0; i < len(form.xs); {
		p := form.xs[i]
		if p.k == KindComment {
			i++
			continue
		}
		if p.k == KindSplice {
			if ctx != "defm" {
				return params{}, errMsg("only defm can use @rest")
			}
			if mode == "key" {
				return params{}, errMsg("do not mix positional and keyword parameters")
			}
			if rest != "" {
				return params{}, errMsg("only one @rest parameter is allowed")
			}
			inner := p.innerVal()
			if inner.k != KindSymbol || inner.s == "" || strings.HasSuffix(inner.s, ":") {
				return params{}, errMsg("@rest needs a name")
			}
			for _, x := range form.xs[i+1:] {
				if x.k != KindComment {
					return params{}, errMsg("@rest must be last")
				}
			}
			mode = "pos"
			rest = inner.s
			if containsStr(seen, rest) {
				return params{}, errf("duplicate parameter %s", rest)
			}
			seen = append(seen, rest)
			i++
			continue
		}
		if rest != "" {
			return params{}, errMsg("@rest must be last")
		}
		if p.isKeySym() {
			if mode == "pos" {
				return params{}, errMsg("do not mix positional and keyword parameters")
			}
			mode = "key"
			name := p.keyName()
			if name == "" {
				return params{}, errMsg("empty parameter name")
			}
			if containsStr(seen, name) {
				return params{}, errf("duplicate parameter %s", name)
			}
			seen = append(seen, name)
			nextOK := i+1 < len(form.xs) && !form.xs[i+1].isKeySym() && form.xs[i+1].k != KindComment
			if nextOK {
				pat, err := asPattern(form.xs[i+1])
				if err != nil {
					return params{}, err
				}
				keys = append(keys, keyPat{Name: name, Pat: pat})
				i += 2
			} else {
				keys = append(keys, keyPat{Name: name, Pat: pattern{Bind: true, Name: name}})
				i++
			}
			continue
		}
		if mode == "key" {
			return params{}, errMsg("do not mix positional and keyword parameters")
		}
		mode = "pos"
		pat, err := asPattern(p)
		if err != nil {
			return params{}, err
		}
		pos = append(pos, pat)
		i++
	}
	if mode == "key" {
		return params{Key: true, Keys: keys}, nil
	}
	return params{Pats: pos, Rest: rest}, nil
}

type callRaw struct {
	pos  []Value
	keys []struct {
		name string
		raw  Value
	}
}

func parseCallRaw(raw []Value) (callRaw, error) {
	var out callRaw
	keyed := false
	for i := 0; i < len(raw); {
		a := raw[i]
		if a.isKeySym() {
			keyed = true
			name := a.keyName()
			if i+1 >= len(raw) {
				return callRaw{}, errf("missing value for %s", a.s)
			}
			for _, k := range out.keys {
				if k.name == name {
					return callRaw{}, errf("duplicate %s", a.s)
				}
			}
			out.keys = append(out.keys, struct {
				name string
				raw  Value
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

func clauseKeys(params params) []string {
	if !params.Key {
		return nil
	}
	out := make([]string, len(params.Keys))
	for i, k := range params.Keys {
		out[i] = k.Name
	}
	return out
}

func unionKeys(clauses []clause) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, c := range clauses {
		for _, k := range clauseKeys(c.params) {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func patCovers(earlier pattern, later *pattern) bool {
	if earlier.Bind {
		return true
	}
	if later == nil || later.Bind {
		return false
	}
	return earlier.Value.Equal(later.Value)
}

func clauseCovers(earlier, later params) bool {
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
	laterBy := map[string]pattern{}
	for _, p := range later.Keys {
		laterBy[p.Name] = p.Pat
	}
	earlierNames := map[string]struct{}{}
	for _, p := range earlier.Keys {
		earlierNames[p.Name] = struct{}{}
		lp, ok := laterBy[p.Name]
		var ptr *pattern
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

func unreachableBy(prev []clause, next params) bool {
	for _, c := range prev {
		if clauseCovers(c.params, next) {
			return true
		}
	}
	return false
}

func isFnSep(v Value) bool { return isSymName(v, "fn") }

func isFnCall(v Value) bool {
	if v.k != KindList || v.vec {
		return false
	}
	xs := filterComments(v.xs)
	return len(xs) > 0 && isSymName(xs[0], "fn")
}

func walkSlots(v Value, onSlot func(name string, node Value) error) error {
	switch v.k {
	case KindComment:
		return nil
	case KindSymbol:
		if strings.HasPrefix(v.s, "#") {
			return onSlot(v.s, v)
		}
		return nil
	case KindQuote, KindUnquote, KindSplice:
		return walkSlots(v.innerVal(), onSlot)
	case KindList:
		if isFnCall(v) {
			return nil
		}
		for _, x := range v.xs {
			if err := walkSlots(x, onSlot); err != nil {
				return err
			}
		}
		return nil
	case KindMap:
		if v.mp == nil {
			return nil
		}
		for _, x := range v.mp.vals {
			if err := walkSlots(x, onSlot); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func maxSlot(args []Value) (int, error) {
	max := 0
	for _, a := range args {
		err := walkSlots(a, func(name string, node Value) error {
			if !isSlotTok(name) {
				return errValf(node, "bad slot %s", name)
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

func hasNestedFn(args []Value) bool {
	var walk func(Value) bool
	walk = func(v Value) bool {
		switch v.k {
		case KindQuote, KindUnquote, KindSplice:
			return walk(v.innerVal())
		case KindList:
			if isFnCall(v) {
				return true
			}
			for _, x := range v.xs {
				if walk(x) {
					return true
				}
			}
			return false
		case KindMap:
			if v.mp == nil {
				return false
			}
			for _, x := range v.mp.vals {
				if walk(x) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	for _, a := range args {
		if walk(a) {
			return true
		}
	}
	return false
}

func slotParams(n int) params {
	pats := make([]pattern, n)
	for i := 1; i <= n; i++ {
		pats[i-1] = pattern{Bind: true, Name: "#" + strconv.Itoa(i)}
	}
	return params{Pats: pats}
}

func shortFnBody(args []Value) []Value {
	xs := filterComments(args)
	if len(xs) <= 1 {
		return xs
	}
	return []Value{CallList(xs...)}
}

type fnParsed struct {
	kind    string
	clauses []clause
}

func parseFn(args []Value) (fnParsed, error) {
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
			clauses: []clause{{params: slotParams(n), Body: shortFnBody(args)}},
		}, nil
	}
	if len(args) == 0 {
		return fnParsed{}, errMsg("(fn (args...) body)")
	}
	var clauses []clause
	i := 0
	for i < len(args) {
		paramsForm := args[i]
		if paramsForm.k != KindList {
			return fnParsed{}, errMsg("(fn (args...) body)")
		}
		params, err := parseParams(paramsForm, "fn")
		if err != nil {
			return fnParsed{}, err
		}
		i++
		var body []Value
		for i < len(args) && !isFnSep(args[i]) {
			body = append(body, args[i])
			i++
		}
		if unreachableBy(clauses, params) {
			return fnParsed{}, errMsg("unreachable clause")
		}
		pf := paramsForm
		clauses = append(clauses, clause{params: params, Body: body, ParamsForm: &pf})
		if i < len(args) && isFnSep(args[i]) {
			i++
			if i >= len(args) {
				return fnParsed{}, errMsg("fn after fn needs a parameter list")
			}
		}
	}
	return fnParsed{kind: "long", clauses: clauses}, nil
}

type ifClause struct {
	test *Value
	not  bool
	body []Value
}

func isElseSym(v Value) bool { return isSymName(v, "else") }
func isIfSym(v Value) bool   { return isSymName(v, "if") }
func isNotSym(v Value) bool  { return isSymName(v, "not") }

func readIfTest(args []Value, i int, ctx string) (test Value, not bool, next int, err error) {
	if i >= len(args) {
		return Value{}, false, 0, errf("%s needs a test", ctx)
	}
	if isNotSym(args[i]) {
		i++
		if i >= len(args) {
			return Value{}, false, 0, errf("%s not needs a test", ctx)
		}
		return args[i], true, i + 1, nil
	}
	return args[i], false, i + 1, nil
}

func parseIfArgs(args []Value) ([]ifClause, error) {
	if len(args) == 0 {
		return nil, errMsg("(if test ...)")
	}
	var clauses []ifClause
	test, not, i, err := readIfTest(args, 0, "if")
	if err != nil {
		return nil, err
	}
	curTest, curNot := test, not
	var body []Value
	for i < len(args) {
		a := args[i]
		if isElseSym(a) {
			t := curTest
			clauses = append(clauses, ifClause{test: &t, not: curNot, body: body})
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
			clauses = append(clauses, ifClause{body: args[i:]})
			return clauses, nil
		}
		body = append(body, a)
		i++
	}
	t := curTest
	clauses = append(clauses, ifClause{test: &t, not: curNot, body: body})
	return clauses, nil
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func makeFnVal(clauses []clause, env *env) Value {
	return Value{k: KindFn, fn: &fnVal{clauses: clauses, keys: unionKeys(clauses), env: env}}
}

func installFns(fns []namedFn, env *env) {
	for _, f := range fns {
		env.set(f.Name, makeFnVal(f.Clauses, env))
	}
}

func toMacroTable(macros []namedFn) map[string][]clause {
	m := map[string][]clause{}
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
