package types

import (
	"sort"
	"strconv"
	"strings"

	"deedles.dev/writ/runtime"
	"deedles.dev/writ/scanner"
)

// Diagnostic is a parse or type error.
type Diagnostic struct {
	Start   int
	End     int
	Message string
}

// TypeHint is an editor hover for a span.
type TypeHint struct {
	Start int
	End   int
	Text  string
}

// CheckResult is the output of Check.
type CheckResult struct {
	Diagnostics []Diagnostic
	Hints       []TypeHint
	Export      Type
}

// PayloadKey is one event payload field.
type PayloadKey struct {
	Name string
	Type Type
}

// Config is host type information for Check.
type Config struct {
	Events  map[string][]PayloadKey
	Aliases []Alias
	Extra   map[string][]Arrow
	File    string
	Import  func(spec, fromFile string) (Type, []Diagnostic, error)
}

type typeEnv map[string]Type

func (e typeEnv) clone() typeEnv {
	out := make(typeEnv, len(e))
	for k, v := range e {
		out[k] = v
	}
	return out
}

type checker struct {
	diags  []Diagnostic
	hints  []TypeHint
	hintAt map[string]TypeHint
	props  map[string][]Type
	fns    map[string]Type
	pass   int
	env    typeEnv
	cfg    Config
}

func newChecker(cfg Config) *checker {
	return &checker{
		hintAt: map[string]TypeHint{},
		props:  map[string][]Type{},
		fns:    map[string]Type{},
		env:    typeEnv{},
		cfg:    cfg,
	}
}

func spanOf(v runtime.Value) runtime.Span {
	s, _ := v.Span()
	return s
}

func (chk *checker) err(v runtime.Value, message string) {
	if chk.pass == 0 {
		return
	}
	s := spanOf(v)
	end := s.End
	if end <= s.Start {
		end = s.Start + 1
	}
	chk.diags = append(chk.diags, Diagnostic{Start: s.Start, End: end, Message: message})
}

func (chk *checker) note(v runtime.Value, t Type, name string) {
	if chk.pass == 0 || !v.HasSpan() || spanOf(v).End <= spanOf(v).Start {
		return
	}
	text := printType(t, chk.cfg.Aliases)
	if name != "" {
		text = name + " : " + text
	}
	key := strconv.Itoa(spanOf(v).Start) + ":" + strconv.Itoa(spanOf(v).End)
	if last, ok := chk.hintAt[key]; ok {
		last.Text = text
		chk.hintAt[key] = last
		for i := range chk.hints {
			if chk.hints[i].Start == last.Start && chk.hints[i].End == last.End {
				chk.hints[i].Text = text
			}
		}
		return
	}
	h := TypeHint{Start: spanOf(v).Start, End: spanOf(v).End, Text: text}
	chk.hintAt[key] = h
	chk.hints = append(chk.hints, h)
}

func (chk *checker) noteParams(form runtime.Value, env typeEnv) {
	if form.Kind() != runtime.KindList {
		return
	}
	for _, p := range form.Items() {
		if p.Kind() == runtime.KindSplice && p.Inner().Kind() == runtime.KindSymbol {
			name := p.Inner().Name()
			if t, ok := env[name]; ok {
				chk.note(p.Inner(), t, name)
			}
			continue
		}
		if p.Kind() != runtime.KindSymbol {
			continue
		}
		name := p.Name()
		if strings.HasSuffix(name, ":") {
			name = name[:len(name)-1]
		}
		if t, ok := env[name]; ok {
			chk.note(p, t, name)
		}
	}
}

func (chk *checker) noteLetKeys(m runtime.Value, env typeEnv) {
	if m.Kind() != runtime.KindMap || m.KeySpans() == nil {
		return
	}
	for k, span := range m.KeySpans() {
		if t, ok := env[k]; ok {
			chk.note(runtime.Symbol(k).WithSpan(span.Start, span.End), t, k)
		}
	}
}

func (chk *checker) expect(got, want Type, at runtime.Value, ctx string) Type {
	ok := isSubtype(got, want)
	if got.k == tyDyn {
		ok = overlaps(got, want)
	}
	if !ok {
		chk.err(at, ctx+": got "+chk.pt(got)+", need "+chk.pt(want))
		return tDyn(want)
	}
	if at.Kind() == runtime.KindSymbol {
		chk.refine(chk.env, at.Name(), unwrap(want))
	}
	hit := intersect(unwrap(got), unwrap(want))
	if got.k == tyDyn {
		return tDyn(hit)
	}
	return got
}

func (chk *checker) pt(t Type) string {
	return printType(t, chk.cfg.Aliases)
}

func (chk *checker) lookup(env typeEnv, name string) (Type, bool) {
	if t, ok := env[name]; ok {
		return t, true
	}
	if t, ok := chk.fns[name]; ok {
		return t, true
	}
	return Type{}, false
}

func (chk *checker) refine(env typeEnv, name string, t Type) {
	cur, ok := env[name]
	if !ok {
		return
	}
	next := intersect(cur, t)
	if !isNone(next) {
		env[name] = next
	}
}

func (chk *checker) typeForm(v runtime.Value, env typeEnv) Type {
	prev := chk.env
	chk.env = env
	defer func() { chk.env = prev }()
	t := chk.infer(v, env)
	if v.Kind() == runtime.KindComment {
		return t
	}
	name := ""
	if v.Kind() == runtime.KindSymbol {
		name = v.Name()
	}
	chk.note(v, t, name)
	return t
}

func (chk *checker) infer(v runtime.Value, env typeEnv) Type {
	switch v.Kind() {
	case runtime.KindComment:
		return NilType()
	case runtime.KindInt:
		return IntType()
	case runtime.KindFloat:
		return FloatType()
	case runtime.KindString:
		return ExactString(v.Text())
	case runtime.KindFn:
		return FnType()
	case runtime.KindNative:
		return kindOf(v)
	case runtime.KindQuote:
		return chk.typeQuote(v.Inner(), env, 1)
	case runtime.KindUnquote:
		chk.err(v, "comma not inside quote")
		return tDyn(Any())
	case runtime.KindSplice:
		chk.err(v, "@ needs a list to insert into")
		return tDyn(Any())
	case runtime.KindSymbol:
		if v.Name() == "true" {
			return TrueType()
		}
		if v.Name() == "false" {
			return FalseType()
		}
		if v.Name() == "nil" {
			return NilType()
		}
		if found, ok := chk.lookup(env, v.Name()); ok {
			return found
		}
		if scanner.IsKeyword(v.Name()) || scanner.IsBuiltin(v.Name()) || chk.hostBuiltin(v.Name()) {
			return Any()
		}
		chk.err(v, "unknown name "+v.Name())
		return tDyn(Any())
	case runtime.KindMap:
		if len(v.Pairs()) == 0 {
			return EmptyMapType()
		}
		var fields []mapField
		for _, pair := range v.Pairs() {
			fields = append(fields, mapField{name: pair.Key, t: chk.typeForm(pair.Value, env)})
		}
		if v.KeySpans() != nil {
			for k, span := range v.KeySpans() {
				for _, f := range fields {
					if f.name == k {
						chk.note(runtime.Symbol(k).WithSpan(span.Start, span.End), f.t, k)
						break
					}
				}
			}
		}
		return tMap(fields, nil)
	case runtime.KindList:
		if v.IsVec() {
			types, _ := chk.typeSpread(v.Items(), env, v, false)
			if len(types) == 0 {
				return EmptyList()
			}
			return tTuple(types)
		}
		xs := runtime.FilterComments(v.Items())
		if len(xs) == 0 {
			return NilType()
		}
		head := xs[0]
		if head.Kind() == runtime.KindSplice {
			types, _ := chk.typeSpread(xs, env, v, false)
			if len(types) == 0 {
				return NilType()
			}
			return chk.applyFnType(types[0], types[1:], v)
		}
		if head.Kind() == runtime.KindSymbol {
			if t, handled := chk.special(head.Name(), xs[1:], env, v); handled {
				return t
			}
			if scanner.IsCoreBuiltin(head.Name()) {
				return chk.callBuiltin(head.Name(), xs[1:], env, v)
			}
			if chk.hostBuiltin(head.Name()) {
				return chk.callHost(head.Name(), xs[1:], env, v)
			}
		}
		fnT := chk.typeForm(head, env)
		return chk.apply(fnT, xs[1:], env, v)
	default:
		return Any()
	}
}

func (chk *checker) hostBuiltin(name string) bool {
	_, ok := chk.cfg.Extra[name]
	return ok
}

func (chk *checker) forms(body []runtime.Value, env typeEnv) Type {
	last := NilType()
	for _, a := range body {
		last = chk.typeForm(a, env)
	}
	return last
}

func (chk *checker) special(name string, args []runtime.Value, env typeEnv, form runtime.Value) (Type, bool) {
	switch name {
	case "if":
		return chk.typeIf(args, env, form), true
	case "and":
		last := TrueType()
		for _, a := range args {
			last = chk.typeForm(a, env)
		}
		return tDyn(tOr([]Type{last, FalseType(), NilType()})), true
	case "or":
		last := FalseType()
		for _, a := range args {
			last = chk.typeForm(a, env)
		}
		return tDyn(tOr([]Type{last, FalseType(), NilType()})), true
	case "not":
		x := runtime.Symbol("nil")
		if len(args) > 0 {
			x = args[0]
		}
		chk.typeForm(x, env)
		return BoolType(), true
	case "fn":
		return chk.typeFn(args, env, form), true
	case "def":
		chk.err(form, "(def ...) only works at the top of a script")
		return NilType(), true
	case "let", "let!":
		return chk.typeLet(args, env, form), true
	case "defm":
		chk.err(form, "(defm ...) only works at the top of a script")
		return NilType(), true
	case "pipe":
		return chk.typePipe(args, env, form), true
	case "eval":
		if len(args) != 1 {
			chk.err(form, "eval needs one form")
			return tDyn(Any()), true
		}
		chk.typeForm(args[0], env)
		return tDyn(Any()), true
	case "after":
		sec := runtime.Symbol("nil")
		if len(args) > 0 {
			sec = args[0]
		}
		chk.expect(chk.typeForm(sec, env), numType(), sec, "after")
		chk.forms(args[1:], env)
		return NilType(), true
	case "on":
		chk.err(form, "(on ...) only works at the top of a script")
		return NilType(), true
	case "import":
		return chk.typeImport(args, env, form), true
	default:
		return Type{}, false
	}
}

func (chk *checker) typeImport(args []runtime.Value, env typeEnv, form runtime.Value) Type {
	if len(args) != 1 {
		chk.err(form, "import needs one path")
		return tDyn(Any())
	}
	t := chk.typeForm(args[0], env)
	u := unwrap(t)
	if u.k == tyStr && u.has && chk.cfg.Import != nil {
		mt, diags, err := chk.cfg.Import(u.s, chk.cfg.File)
		if chk.pass == 1 {
			for _, d := range diags {
				chk.err(args[0], d.Message)
			}
			if err != nil {
				chk.err(args[0], err.Error())
			}
		}
		if err == nil {
			return mt
		}
	}
	chk.expect(t, stringyType(), args[0], "import")
	anyT := Any()
	return tDyn(tMap(nil, &anyT))
}

func (chk *checker) typeIf(args []runtime.Value, env typeEnv, form runtime.Value) Type {
	clauses, err := runtime.ParseIfArgs(args)
	if err != nil {
		chk.err(form, err.Error())
		return tDyn(Any())
	}
	var results []Type
	for _, c := range clauses {
		child := env.clone()
		if c.Test != nil {
			chk.typeForm(*c.Test, env)
			chk.narrow(*c.Test, c.Not, child)
		}
		results = append(results, chk.forms(c.Body, child))
	}
	return tOr(results)
}

func (chk *checker) narrow(test runtime.Value, not bool, env typeEnv) {
	apply := func(name string, whenTrue, whenFalse Type) {
		if _, ok := env[name]; !ok {
			return
		}
		if not {
			env[name] = whenFalse
		} else {
			env[name] = whenTrue
		}
	}
	if test.Kind() == runtime.KindSymbol {
		cur, ok := env[test.Name()]
		if !ok {
			return
		}
		apply(test.Name(), withoutFalsy(cur), onlyFalsy(cur))
		return
	}
	if test.Kind() != runtime.KindList || test.IsVec() {
		return
	}
	xs := runtime.FilterComments(test.Items())
	if len(xs) < 2 || xs[0].Kind() != runtime.KindSymbol || xs[1].Kind() != runtime.KindSymbol {
		return
	}
	cur, ok := env[xs[1].Name()]
	if !ok {
		return
	}
	var want Type
	switch xs[0].Name() {
	case "str?":
		want = stringyType()
	case "num?":
		want = numType()
	case "int?":
		want = IntType()
	case "float?":
		want = FloatType()
	case "bool?":
		want = BoolType()
	case "nil?":
		want = NilType()
	case "symbol?":
		want = SymbolType()
	case "list?":
		want = listyType()
	case "map?":
		want = mapyType()
	default:
		return
	}
	yes := fromUsage(intersect(cur, want))
	if isNone(yes) {
		yes = cur
	}
	apply(xs[1].Name(), yes, cur)
}

func (chk *checker) typeLet(args []runtime.Value, env typeEnv, form runtime.Value) Type {
	if len(args) == 0 {
		chk.err(form, "let needs a map")
		return NilType()
	}
	mt := chk.typeForm(args[0], env)
	inner := unwrap(mt)
	mapWant := mapyType()
	if mt.k == tyDyn {
		if !overlaps(mt, mapWant) {
			chk.err(args[0], "let needs a map, got "+chk.pt(mt))
		}
	} else if inner.k != tyMap && inner.k != tyEmptyMap {
		chk.err(args[0], "let needs a map, got "+chk.pt(mt))
	}
	child := env.clone()
	letNames := map[string]struct{}{}
	if inner.k == tyMap {
		for _, f := range inner.fields {
			child[f.name] = f.t
			letNames[f.name] = struct{}{}
		}
	}
	body := args[1:]
	saved := chk.pass
	chk.pass = 0
	chk.forms(body, child)
	chk.pass = saved
	for k, t := range child {
		if _, local := letNames[k]; !local {
			if _, ok := env[k]; ok {
				env[k] = t
			}
		}
	}
	for k := range letNames {
		if t, ok := child[k]; ok {
			child[k] = fromUsage(t)
		}
	}
	aliases := map[string]string{}
	collectAliases(args[0], aliases)
	for k, src := range aliases {
		if _, ok := letNames[k]; !ok {
			continue
		}
		t, ok := child[k]
		if !ok {
			continue
		}
		cur, ok := child[src]
		if !ok {
			cur, ok = env[src]
		}
		if !ok {
			continue
		}
		next := fromUsage(intersect(cur, t))
		if isNone(next) {
			continue
		}
		child[src] = next
		if _, ok := env[src]; ok {
			env[src] = next
		}
	}
	ret := chk.forms(body, child)
	chk.noteLetKeys(args[0], child)
	return ret
}

func (chk *checker) typeQuote(v runtime.Value, env typeEnv, depth int) Type {
	switch v.Kind() {
	case runtime.KindUnquote:
		if depth == 1 {
			return chk.typeForm(v.Inner(), env)
		}
		return chk.typeQuote(v.Inner(), env, depth-1)
	case runtime.KindQuote:
		return chk.typeQuote(v.Inner(), env, depth+1)
	case runtime.KindSplice:
		if depth != 1 {
			return Any()
		}
		inner := chk.typeForm(v.Inner(), env)
		chk.expectListish(inner, v)
		return inner
	case runtime.KindList:
		if depth != 1 {
			return kindOf(v)
		}
		types, _ := chk.typeSpread(v.Items(), env, v, true)
		if len(types) == 0 {
			return EmptyList()
		}
		return tTuple(types)
	case runtime.KindMap:
		if len(v.Pairs()) == 0 {
			return EmptyMapType()
		}
		var fields []mapField
		for _, pair := range v.Pairs() {
			fields = append(fields, mapField{name: pair.Key, t: chk.typeQuote(pair.Value, env, depth)})
		}
		return tMap(fields, nil)
	case runtime.KindSymbol:
		return ExactSymbol(v.Name())
	default:
		return kindOf(v)
	}
}

func (chk *checker) expectListish(t Type, at runtime.Value) {
	u := unwrap(t)
	if u.k == tyList || u.k == tyTuple || u.k == tyEmptyList || u.k == tyAny || t.k == tyDyn {
		if t.k == tyDyn && u.k != tyList && u.k != tyTuple && u.k != tyEmptyList && u.k != tyAny {
			chk.expect(t, tOr([]Type{EmptyList(), tList(Any())}), at, "@")
		}
		return
	}
	chk.expect(t, tOr([]Type{EmptyList(), tList(Any())}), at, "@")
}

func (chk *checker) typeSpread(items []runtime.Value, env typeEnv, at runtime.Value, quoteSplice bool) ([]Type, bool) {
	var types []Type
	open := false
	for _, a := range items {
		if a.Kind() == runtime.KindComment {
			continue
		}
		if a.Kind() == runtime.KindSplice {
			var inner Type
			if quoteSplice {
				if a.Inner().Kind() == runtime.KindUnquote {
					inner = chk.typeForm(a.Inner().Inner(), env)
				} else {
					inner = chk.typeForm(a.Inner(), env)
				}
			} else {
				inner = chk.typeForm(a.Inner(), env)
			}
			chk.expectListish(inner, a)
			u := unwrap(inner)
			switch u.k {
			case tyTuple:
				types = append(types, u.items...)
			case tyEmptyList:
			case tyList:
				types = append(types, *u.inner)
				open = true
			default:
				open = true
			}
			continue
		}
		if quoteSplice {
			types = append(types, chk.typeQuote(a, env, 1))
		} else {
			types = append(types, chk.typeForm(a, env))
		}
	}
	return types, open
}

type typedCall struct {
	pos    []Type
	keys   map[string]Type
	rawPos []runtime.Value
	keyRaw []struct {
		name string
		raw  runtime.Value
	}
	open bool
}

func (chk *checker) typeCallParts(raw []runtime.Value, env typeEnv, form runtime.Value) typedCall {
	out := typedCall{keys: map[string]Type{}}
	keyed := false
	for i := 0; i < len(raw); {
		a := raw[i]
		if a.Kind() == runtime.KindComment {
			i++
			continue
		}
		if a.Kind() == runtime.KindSplice {
			if keyed {
				chk.err(a, "positional argument after key:")
				break
			}
			types, more := chk.typeSpread([]runtime.Value{a}, env, form, false)
			out.pos = append(out.pos, types...)
			for range types {
				out.rawPos = append(out.rawPos, a)
			}
			out.open = out.open || more
			i++
			continue
		}
		if a.IsKey() {
			keyed = true
			name := a.KeyName()
			if i+1 >= len(raw) {
				chk.err(a, "missing value for "+a.Name())
				break
			}
			val := raw[i+1]
			out.keys[name] = chk.typeForm(val, env)
			out.keyRaw = append(out.keyRaw, struct {
				name string
				raw  runtime.Value
			}{name, val})
			i += 2
			continue
		}
		if keyed {
			chk.err(a, "positional argument after key:")
			break
		}
		out.pos = append(out.pos, chk.typeForm(a, env))
		out.rawPos = append(out.rawPos, a)
		i++
	}
	return out
}

func (chk *checker) typeFn(args []runtime.Value, env typeEnv, form runtime.Value) Type {
	_, clauses, err := runtime.ParseFn(args)
	if err != nil {
		chk.err(form, err.Error())
		return tDyn(Any())
	}
	arrows := make([]Arrow, len(clauses))
	for i, c := range clauses {
		var pf runtime.Value
		if c.ParamsForm != nil {
			pf = *c.ParamsForm
		}
		arrows[i] = chk.typeClause(c.Params, c.Body, env, pf)
	}
	return FnType(arrows...)
}

func (chk *checker) typePipe(args []runtime.Value, env typeEnv, form runtime.Value) Type {
	steps := runtime.FilterComments(args)
	if len(steps) < 2 {
		chk.err(form, "pipe needs a value and at least one step")
		return tDyn(Any())
	}
	cur := chk.typeForm(steps[0], env)
	for _, step := range steps[1:] {
		if step.Kind() == runtime.KindSymbol {
			if fnT, ok := chk.lookup(env, step.Name()); ok {
				chk.note(step, fnT, step.Name())
			}
			cur = chk.applyTypeToCall(step.Name(), []Type{cur}, map[string]Type{}, step, env)
			continue
		}
		if step.Kind() != runtime.KindList || step.IsVec() {
			chk.err(step, "pipe step must be a call or a name")
			continue
		}
		xs := runtime.FilterComments(step.Items())
		if len(xs) == 0 || xs[0].Kind() != runtime.KindSymbol {
			chk.err(step, "pipe step must be a call or a name")
			continue
		}
		rest := make([]Type, len(xs)-1)
		for i, a := range xs[1:] {
			rest[i] = chk.typeForm(a, env)
		}
		pos := append([]Type{cur}, rest...)
		cur = chk.applyTypeToCall(xs[0].Name(), pos, map[string]Type{}, step, env)
	}
	return cur
}

func (chk *checker) bindParams(params runtime.Params, env typeEnv) {
	if !params.Key {
		for _, p := range params.Pats {
			if p.Bind {
				env[p.Name] = tDyn(Any())
			}
		}
		if params.Rest != "" {
			env[params.Rest] = tList(Any())
		}
		return
	}
	for _, kp := range params.Keys {
		if kp.Pat.Bind {
			env[kp.Pat.Name] = tDyn(Any())
			if kp.Name != kp.Pat.Name {
				env[kp.Name] = tDyn(Any())
			}
		} else {
			env[kp.Name] = kindOf(kp.Pat.Value)
		}
	}
}

func (chk *checker) promoteBinds(params runtime.Params, env typeEnv) {
	bump := func(name string) {
		if t, ok := env[name]; ok {
			env[name] = fromUsage(t)
		}
	}
	if !params.Key {
		for _, p := range params.Pats {
			if p.Bind {
				bump(p.Name)
			}
		}
		if params.Rest != "" {
			bump(params.Rest)
		}
		return
	}
	for _, kp := range params.Keys {
		if kp.Pat.Bind {
			bump(kp.Pat.Name)
			if kp.Name != kp.Pat.Name {
				bump(kp.Name)
			}
		}
	}
}

func (chk *checker) typeClause(params runtime.Params, body []runtime.Value, parent typeEnv, paramsForm runtime.Value) Arrow {
	e := parent.clone()
	chk.bindParams(params, e)
	saved := chk.pass
	chk.pass = 0
	chk.forms(body, e)
	chk.pass = saved
	chk.promoteBinds(params, e)
	ret := chk.forms(body, e)
	chk.noteParams(paramsForm, e)
	return chk.arrowFrom(params, e, ret)
}

func (chk *checker) typeMacroClause(params runtime.Params, body []runtime.Value, parent typeEnv, paramsForm runtime.Value) {
	e := parent.clone()
	chk.bindParams(params, e)
	saved := chk.pass
	chk.pass = 0
	chk.macroForms(body, e)
	chk.pass = saved
	chk.promoteBinds(params, e)
	chk.macroForms(body, e)
	chk.noteParams(paramsForm, e)
}

func (chk *checker) macroForms(body []runtime.Value, env typeEnv) Type {
	last := NilType()
	for _, a := range body {
		if a.Kind() == runtime.KindSplice {
			t := chk.typeForm(a.Inner(), env)
			chk.expectListish(t, a)
			last = t
			continue
		}
		last = chk.typeForm(a, env)
	}
	return last
}

func (chk *checker) arrowFrom(params runtime.Params, env typeEnv, ret Type) Arrow {
	if !params.Key {
		args := make([]Type, len(params.Pats))
		for i, p := range params.Pats {
			if !p.Bind {
				args[i] = kindOf(p.Value)
			} else if t, ok := env[p.Name]; ok {
				args[i] = t
			} else {
				args[i] = Any()
			}
		}
		return Arrow{Args: args, Result: ret, Rest: params.Rest != ""}
	}
	keys := make([]ArrowKey, len(params.Keys))
	for i, kp := range params.Keys {
		var t Type
		if !kp.Pat.Bind {
			t = kindOf(kp.Pat.Value)
		} else if got, ok := env[kp.Pat.Name]; ok {
			t = got
		} else {
			t = Any()
		}
		keys[i] = ArrowKey{Name: kp.Name, Type: t}
	}
	return Arrow{Key: true, Keys: keys, Result: ret}
}

func (chk *checker) apply(fnT Type, raw []runtime.Value, env typeEnv, form runtime.Value) Type {
	parts := chk.typeCallParts(raw, env, form)
	inner := unwrap(fnT)
	if fnT.k == tyDyn {
		return tDyn(Any())
	}
	if inner.k == tyAny {
		return tDyn(Any())
	}
	if inner.k != tyFn {
		chk.err(form, "not a function: "+chk.pt(fnT))
		return tDyn(Any())
	}
	before := len(chk.diags)
	ret := chk.applyArrows(inner.arrows, parts.pos, parts.keys, form)
	if parts.open && len(chk.diags) > before {
		chk.diags = chk.diags[:before]
		return tDyn(Any())
	}
	chk.refineCall(inner.arrows, parts.rawPos, parts.keyRaw, env)
	return ret
}

func (chk *checker) applyFnType(fnT Type, pos []Type, form runtime.Value) Type {
	if fnT.k == tyDyn {
		return tDyn(Any())
	}
	inner := unwrap(fnT)
	if inner.k == tyAny {
		return tDyn(Any())
	}
	if inner.k != tyFn {
		chk.err(form, "not a function: "+chk.pt(fnT))
		return tDyn(Any())
	}
	return chk.applyArrows(inner.arrows, pos, map[string]Type{}, form)
}

func (chk *checker) applyArrows(arrows []Arrow, pos []Type, keys map[string]Type, form runtime.Value) Type {
	var rets []Type
	for _, ar := range arrows {
		if !ar.Key {
			if len(keys) > 0 {
				continue
			}
			if ar.Rest {
				if len(pos) < len(ar.Args) {
					continue
				}
				ok := true
				for i, t := range ar.Args {
					if !argFits(pos[i], t) {
						ok = false
						break
					}
				}
				if ok {
					rets = append(rets, ar.Result)
				}
				continue
			}
			if len(ar.Args) != len(pos) {
				continue
			}
			ok := true
			for i, t := range ar.Args {
				if !argFits(pos[i], t) {
					ok = false
					break
				}
			}
			if ok {
				rets = append(rets, ar.Result)
			}
			continue
		}
		if len(pos) > 0 {
			continue
		}
		ok := true
		declared := map[string]struct{}{}
		for _, k := range ar.Keys {
			declared[k.Name] = struct{}{}
			got, has := keys[k.Name]
			if !has {
				got = NilType()
			}
			if !argFits(got, tOr([]Type{k.Type, NilType()})) {
				ok = false
				break
			}
		}
		if ok {
			for name := range keys {
				if _, has := declared[name]; !has {
					ok = false
					break
				}
			}
		}
		if ok {
			rets = append(rets, ar.Result)
		}
	}
	if len(rets) == 0 {
		chk.err(form, "no matching clause")
		return tDyn(Any())
	}
	u := tOr(rets)
	if len(rets) > 1 {
		return tDyn(u)
	}
	return u
}

func (chk *checker) refineCall(arrows []Arrow, posRaw []runtime.Value, keyRaw []struct {
	name string
	raw  runtime.Value
}, env typeEnv) {
	var posAr []Arrow
	for _, a := range arrows {
		if a.Key {
			continue
		}
		if len(a.Args) == len(posRaw) || (a.Rest && len(a.Args) <= len(posRaw)) {
			posAr = append(posAr, a)
		}
	}
	for i, arg := range posRaw {
		if arg.Kind() != runtime.KindSymbol {
			continue
		}
		var wants []Type
		for _, a := range posAr {
			if i < len(a.Args) {
				wants = append(wants, a.Args[i])
			}
		}
		skip := len(wants) == 0
		for _, t := range wants {
			if unwrap(t).k == tyAny {
				skip = true
			}
		}
		if skip {
			continue
		}
		un := make([]Type, len(wants))
		for i, t := range wants {
			un[i] = unwrap(t)
		}
		chk.refine(env, arg.Name(), tOr(un))
	}
	if len(posRaw) > 0 {
		return
	}
	var keyAr []Arrow
	for _, a := range arrows {
		if a.Key {
			keyAr = append(keyAr, a)
		}
	}
	for _, kr := range keyRaw {
		if kr.raw.Kind() != runtime.KindSymbol {
			continue
		}
		var wants []Type
		for _, a := range keyAr {
			for _, k := range a.Keys {
				if k.Name == kr.name {
					wants = append(wants, k.Type)
				}
			}
		}
		skip := len(wants) == 0
		for _, t := range wants {
			if unwrap(t).k == tyAny {
				skip = true
			}
		}
		if skip {
			continue
		}
		un := make([]Type, len(wants))
		for i, t := range wants {
			un[i] = unwrap(t)
		}
		chk.refine(env, kr.raw.Name(), tOr(un))
	}
}

func (chk *checker) applyTypeToCall(name string, pos []Type, keys map[string]Type, at runtime.Value, env typeEnv) Type {
	if scanner.IsCoreBuiltin(name) {
		return chk.callBuiltinTyped(name, pos, at, env, nil)
	}
	if chk.hostBuiltin(name) {
		return chk.callHostTyped(name, pos, keys, at)
	}
	fnT, ok := chk.lookup(env, name)
	if !ok {
		chk.err(at, "unknown name "+name)
		return tDyn(Any())
	}
	inner := unwrap(fnT)
	if inner.k != tyFn {
		chk.err(at, "not a function: "+chk.pt(fnT))
		return tDyn(Any())
	}
	return chk.applyArrows(inner.arrows, pos, keys, at)
}

func (chk *checker) callBuiltin(name string, raw []runtime.Value, env typeEnv, form runtime.Value) Type {
	parts := chk.typeCallParts(raw, env, form)
	if len(parts.keys) > 0 {
		chk.err(form, name+" does not take keyword arguments")
	}
	return chk.callBuiltinTyped(name, parts.pos, form, env, parts.rawPos)
}

func (chk *checker) callHost(name string, raw []runtime.Value, env typeEnv, form runtime.Value) Type {
	parts := chk.typeCallParts(raw, env, form)
	return chk.callHostTyped(name, parts.pos, parts.keys, form)
}

func (chk *checker) callHostTyped(name string, pos []Type, keys map[string]Type, form runtime.Value) Type {
	b := chk.cfg.Extra[name]
	if len(b) == 0 {
		return tDyn(Any())
	}
	return chk.applyArrows(b, pos, keys, form)
}

func (chk *checker) expectNum(pos []Type, raw []runtime.Value, form runtime.Value, name string) {
	for i, p := range pos {
		at := form
		if i < len(raw) {
			at = raw[i]
		}
		chk.expect(p, numType(), at, name)
		if at.Kind() == runtime.KindSymbol {
			chk.refine(chk.env, at.Name(), unwrap(numType()))
		}
	}
}

func (chk *checker) arithResult(pos []Type) Type {
	allInt := true
	anyFloat := false
	for _, p := range pos {
		u := unwrap(p)
		if !isSubtype(u, IntType()) {
			allInt = false
		}
		if isSubtype(u, FloatType()) && !isSubtype(u, IntType()) {
			anyFloat = true
		}
	}
	if allInt {
		return IntType()
	}
	if anyFloat {
		return FloatType()
	}
	return numType()
}

func (chk *checker) callBuiltinTyped(name string, pos []Type, form runtime.Value, env typeEnv, raw []runtime.Value) Type {
	at := func(i int) runtime.Value {
		if i < len(raw) {
			return raw[i]
		}
		return form
	}
	switch name {
	case "+", "*", "min", "max":
		chk.expectNum(pos, raw, form, name)
		if len(pos) == 0 {
			if name == "*" {
				return IntType()
			}
			return IntType()
		}
		return chk.arithResult(pos)
	case "-", "/":
		if len(pos) == 0 {
			chk.err(form, name+" needs a number")
			return tDyn(numType())
		}
		chk.expectNum(pos, raw, form, name)
		if name == "/" && len(pos) >= 2 {
			allInt := true
			for _, p := range pos {
				if !isSubtype(unwrap(p), IntType()) {
					allInt = false
					break
				}
			}
			if allInt {
				return numType()
			}
		}
		return chk.arithResult(pos)
	case "mod":
		if len(pos) < 2 {
			chk.err(form, "mod needs two integers")
			return IntType()
		}
		chk.expect(pos[0], IntType(), at(0), name)
		chk.expect(pos[1], IntType(), at(1), name)
		return IntType()
	case "abs", "floor", "ceil":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, numType(), at(0), name)
		if at(0).Kind() == runtime.KindSymbol {
			chk.refine(env, at(0).Name(), unwrap(numType()))
		}
		u := unwrap(arg)
		if isSubtype(u, IntType()) {
			return IntType()
		}
		if isSubtype(u, FloatType()) && !isSubtype(u, IntType()) {
			return FloatType()
		}
		return numType()
	case "=", "/=":
		return BoolType()
	case "<", ">", "<=", ">=":
		a, b := NilType(), NilType()
		if len(pos) > 0 {
			a = pos[0]
		}
		if len(pos) > 1 {
			b = pos[1]
		}
		chk.expect(a, numType(), at(0), name)
		chk.expect(b, numType(), at(1), name)
		if at(0).Kind() == runtime.KindSymbol {
			chk.refine(env, at(0).Name(), unwrap(numType()))
		}
		if at(1).Kind() == runtime.KindSymbol {
			chk.refine(env, at(1).Name(), unwrap(numType()))
		}
		return BoolType()
	case "str":
		var texts []string
		allLit := true
		for _, p := range pos {
			t := unwrap(p)
			switch {
			case t.k == tyStr && t.has:
				texts = append(texts, t.s)
			case t.k == tySym && t.has:
				texts = append(texts, t.s)
			case t.k == tyBool && t.has:
				if t.b {
					texts = append(texts, "true")
				} else {
					texts = append(texts, "false")
				}
			case t.k == tyNil:
				texts = append(texts, "nil")
			default:
				allLit = false
			}
		}
		if allLit {
			return ExactString(strings.Join(texts, ""))
		}
		return UnknownString()
	case "len":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, tOr([]Type{stringyType(), listyType(), mapyType()}), at(0), name)
		return IntType()
	case "cons":
		el := Any()
		if len(pos) > 0 {
			el = pos[0]
		}
		xsT := EmptyList()
		if len(pos) > 1 {
			xsT = unwrap(pos[1])
		} else {
			xsT = EmptyList()
		}
		switch xsT.k {
		case tyEmptyList:
			return tTuple([]Type{el})
		case tyTuple:
			return tTuple(append([]Type{el}, xsT.items...))
		case tyList:
			return tList(tOr([]Type{el, *xsT.inner}))
		default:
			return tList(el)
		}
	case "first":
		xs := NilType()
		if len(pos) > 0 {
			xs = unwrap(pos[0])
		}
		switch xs.k {
		case tyTuple:
			if len(xs.items) == 0 {
				return NilType()
			}
			return xs.items[0]
		case tyList:
			return tOr([]Type{*xs.inner, NilType()})
		case tyEmptyList:
			return NilType()
		default:
			return tDyn(Any())
		}
	case "rest":
		xs := NilType()
		if len(pos) > 0 {
			xs = unwrap(pos[0])
		}
		switch xs.k {
		case tyTuple:
			return tTuple(xs.items[1:])
		case tyList:
			return xs
		case tyEmptyList:
			return EmptyList()
		default:
			return tDyn(tList(Any()))
		}
	case "nth":
		if len(pos) > 1 {
			chk.expect(pos[1], numType(), at(1), "nth")
		}
		xs := NilType()
		if len(pos) > 0 {
			xs = unwrap(pos[0])
		}
		switch xs.k {
		case tyList:
			return tOr([]Type{*xs.inner, NilType()})
		case tyTuple:
			return tDyn(tOr(append(append([]Type{}, xs.items...), NilType())))
		default:
			return tDyn(Any())
		}
	case "append":
		return tList(Any())
	case "map", "filter":
		return tList(tDyn(Any()))
	case "reduce":
		init := Any()
		if len(pos) > 1 {
			init = pos[1]
		}
		return tDyn(init)
	case "pairs":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, mapyType(), at(0), name)
		return tList(Tuple(StringType(), Any()))
	case "from-pairs":
		anyT := Any()
		return tDyn(tMap(nil, &anyT))
	case "keys":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, mapyType(), at(0), name)
		return tList(stringyType())
	case "vals":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, mapyType(), at(0), name)
		return tList(Any())
	case "empty?", "list?", "map?", "num?", "int?", "float?", "str?", "bool?", "nil?", "symbol?":
		var got Type
		if len(pos) > 0 {
			got = pos[0]
		}
		chk.typePred(name, got, at(0), env)
		return BoolType()
	case "symbol":
		if len(pos) == 0 {
			chk.err(form, "symbol needs a string")
			return UnknownSymbol()
		}
		a := unwrap(pos[0])
		switch {
		case a.k == tySym:
			if a.has {
				return ExactSymbol(a.s)
			}
			return SymbolType()
		case a.k == tyUSym:
			return UnknownSymbol()
		case a.k == tyStr && a.has:
			return ExactSymbol(a.s)
		case a.k == tyStr || a.k == tyUStr:
			return UnknownSymbol()
		case a.k == tyBool && a.has && a.b:
			return TrueType()
		case a.k == tyBool && a.has && !a.b:
			return FalseType()
		case a.k == tyNil:
			return NilType()
		}
		chk.expect(pos[0], tOr([]Type{stringyType(), SymbolType(), UnknownSymbol(), BoolType(), NilType()}), at(0), "symbol")
		return UnknownSymbol()
	case "get":
		if len(pos) < 2 {
			chk.err(form, "get needs a map and a key")
			return tDyn(Any())
		}
		chk.expect(pos[0], mapyType(), at(0), "get")
		chk.expect(pos[1], pathWant(), at(1), "get")
		m := unwrap(pos[0])
		pk := pathKeys(pos[1])
		if pk.none {
			return NilType()
		}
		if pk.unknown {
			if m.k == tyMap {
				var vals []Type
				for _, f := range m.fields {
					vals = append(vals, f.t)
				}
				if m.rest != nil {
					vals = append(vals, *m.rest)
				}
				return tDyn(tOr(append(vals, NilType())))
			}
			return tDyn(Any())
		}
		if m.k == tyEmptyMap {
			return NilType()
		}
		if m.k == tyMap {
			return typeMapGet(m, pk.keys)
		}
		return tDyn(Any())
	case "set":
		if len(pos) < 3 {
			chk.err(form, "set needs a map, a key, and a value")
			return tDyn(Any())
		}
		chk.expect(pos[0], mapyType(), at(0), "set")
		chk.expect(pos[1], pathWant(), at(1), "set")
		pk := pathKeys(pos[1])
		val := pos[2]
		if pk.none {
			return pos[0]
		}
		if pk.unknown {
			anyT := Any()
			return tDyn(tMap(nil, &anyT))
		}
		return typeMapSet(pos[0], pk.keys, val)
	case "update":
		if len(pos) < 3 {
			chk.err(form, "update needs a map, a key, and a function")
			return tDyn(Any())
		}
		chk.expect(pos[0], mapyType(), at(0), "update")
		chk.expect(pos[1], pathWant(), at(1), "update")
		pk := pathKeys(pos[1])
		var cur Type
		switch {
		case pk.none:
			cur = NilType()
		case pk.unknown:
			cur = tDyn(Any())
		default:
			cur = typeMapGet(unwrap(pos[0]), pk.keys)
		}
		ret := chk.applyFnType(pos[2], []Type{cur}, form)
		if pk.none {
			return pos[0]
		}
		if pk.unknown {
			anyT := Any()
			return tDyn(tMap(nil, &anyT))
		}
		return typeMapSet(pos[0], pk.keys, ret)
	case "get-prop":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		chk.expect(arg, pathWant(), at(0), "get-prop")
		pk := pathKeys(arg)
		if pk.none {
			return NilType()
		}
		if pk.unknown {
			return tDyn(Any())
		}
		return propLeafType(chk.props[pk.keys[0]], pk.keys[1:])
	case "set-prop":
		arg := NilType()
		if len(pos) > 0 {
			arg = pos[0]
		}
		val := NilType()
		if len(pos) > 1 {
			val = pos[1]
		}
		chk.expect(arg, pathWant(), at(0), "set-prop")
		pk := pathKeys(arg)
		if !pk.none && !pk.unknown && chk.pass == 0 {
			root := pk.keys[0]
			list := chk.props[root]
			list = append(list, propWriteType(list, pk.keys[1:], val))
			chk.props[root] = list
		}
		return val
	case "update-prop":
		return chk.typeUpdateProp(pos, form, raw)
	case "merge":
		acc := EmptyMapType()
		for i, p := range pos {
			chk.expect(p, mapyType(), at(i), "merge")
			m := unwrap(p)
			if acc.k == tyEmptyMap && m.k == tyMap {
				acc = m
			} else if m.k == tyEmptyMap {
				continue
			} else if acc.k == tyMap && m.k == tyMap {
				fields := append([]mapField{}, acc.fields...)
				for _, f := range m.fields {
					replaced := false
					for i := range fields {
						if fields[i].name == f.name {
							fields[i] = f
							replaced = true
							break
						}
					}
					if !replaced {
						fields = append(fields, f)
					}
				}
				rest := acc.rest
				if m.rest != nil {
					rest = m.rest
				}
				acc = tMap(fields, rest)
			} else {
				anyT := Any()
				acc = tDyn(tMap(nil, &anyT))
			}
		}
		return acc
	default:
		chk.err(form, "unknown function: "+name)
		return tDyn(Any())
	}
}

func (chk *checker) typePred(name string, got Type, at runtime.Value, env typeEnv) {
	if at.Kind() == runtime.KindSymbol && got.k != 0 {
		chk.refine(env, at.Name(), Any())
	}
}

func (chk *checker) typeUpdateProp(pos []Type, form runtime.Value, raw []runtime.Value) Type {
	at := func(i int) runtime.Value {
		if i < len(raw) {
			return raw[i]
		}
		return form
	}
	if len(pos) < 2 {
		chk.err(form, "update-prop needs a name and a function")
		return tDyn(Any())
	}
	chk.expect(pos[0], pathWant(), at(0), "update-prop")
	pk := pathKeys(pos[0])
	cur := tDyn(Any())
	var root string
	var rest []string
	if pk.none {
		cur = NilType()
	} else if !pk.unknown {
		root = pk.keys[0]
		rest = pk.keys[1:]
		cur = propLeafType(chk.props[root], rest)
	}
	fnT := pos[1]
	ret := chk.applyFnType(fnT, []Type{cur}, form)
	if root != "" && chk.pass == 0 {
		inner := unwrap(fnT)
		written := ret
		if inner.k == tyFn && len(inner.arrows) > 0 {
			var rs []Type
			for _, a := range inner.arrows {
				rs = append(rs, a.Result)
			}
			written = tOr(rs)
		}
		list := chk.props[root]
		list = append(list, propWriteType(list, rest, written))
		chk.props[root] = list
	}
	return ret
}

func mergeDiags(diags []Diagnostic) []Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	sorted := append([]Diagnostic{}, diags...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].End < sorted[j].End
	})
	var out []Diagnostic
	for _, d := range sorted {
		if n := len(out); n > 0 && d.Start <= out[n-1].End {
			if d.End > out[n-1].End {
				out[n-1].End = d.End
			}
			if !strings.Contains(out[n-1].Message, d.Message) {
				out[n-1].Message += " · " + d.Message
			}
		} else {
			out = append(out, d)
		}
	}
	return out
}

// Check type-checks already-expanded forms and a compiled program.
func Check(forms []runtime.Value, prog runtime.Program, cfg Config) CheckResult {
	if len(forms) == 0 && len(prog.Boot) == 0 && len(prog.Fns) == 0 && len(prog.Handlers) == 0 && len(prog.Macros) == 0 {
		return CheckResult{}
	}
	chk := newChecker(cfg)
	env := typeEnv{}

	type fnForm struct {
		name       string
		nameForm   runtime.Value
		params     runtime.Params
		body       []runtime.Value
		paramsForm runtime.Value
	}
	var fnForms []fnForm
	type onForm struct {
		event      string
		form       runtime.Value
		params     runtime.Params
		body       []runtime.Value
		paramsForm runtime.Value
	}
	var onForms []onForm
	boot := append([]runtime.Value{}, prog.Boot...)

	for _, f := range prog.Fns {
		for _, c := range f.Clauses {
			pf := runtime.CallList()
			if c.ParamsForm != nil {
				pf = *c.ParamsForm
			}
			fnForms = append(fnForms, fnForm{
				name: f.Name, nameForm: f.NameForm, params: c.Params, body: c.Body, paramsForm: pf,
			})
		}
	}
	for _, h := range prog.Handlers {
		for _, c := range h.Clauses {
			pf := runtime.Symbol(h.Event)
			if c.ParamsForm != nil {
				pf = *c.ParamsForm
			}
			onForms = append(onForms, onForm{
				event: h.Event, form: pf, params: c.Params, body: c.Body, paramsForm: pf,
			})
		}
	}

	chk.pass = 1
	for _, form := range forms {
		if form.Kind() == runtime.KindComment {
			continue
		}
		if form.Kind() == runtime.KindList && len(form.Items()) > 0 && runtime.IsName(form.Items()[0], "defm") {
			d, ok, err := runtime.AsDefForm(form, "defm")
			if err != nil {
				chk.err(form, err.Error())
				continue
			}
			if !ok {
				continue
			}
			chk.typeMacroClause(d.Params, d.Body, env, d.ParamsForm)
			if d.NameForm.HasSpan() {
				chk.note(d.NameForm, tDyn(Any()), d.Name)
			}
		}
	}

	byName := map[string][]fnForm{}
	for _, f := range fnForms {
		byName[f.name] = append(byName[f.name], f)
	}
	for name, list := range byName {
		arrows := make([]Arrow, len(list))
		for i, f := range list {
			e := env.clone()
			chk.bindParams(f.params, e)
			arrows[i] = chk.arrowFrom(f.params, e, tDyn(Any()))
		}
		chk.fns[name] = FnType(arrows...)
	}

	typeFnBodies := func() {
		for name, list := range byName {
			arrows := make([]Arrow, len(list))
			for i, f := range list {
				arrows[i] = chk.typeClause(f.params, f.body, env, f.paramsForm)
			}
			chk.fns[name] = FnType(arrows...)
		}
	}

	runBodies := func() {
		for _, f := range boot {
			chk.typeForm(f, env)
		}
		typeFnBodies()
		if chk.pass == 1 {
			for _, f := range fnForms {
				if t, ok := chk.fns[f.name]; ok {
					chk.note(f.nameForm, t, f.name)
				}
			}
		}
		for _, o := range onForms {
			if len(chk.cfg.Events) > 0 {
				if _, ok := chk.cfg.Events[o.event]; !ok {
					chk.err(o.form, "unknown event "+o.event)
				}
			}
			e := env.clone()
			chk.bindParams(o.params, e)
			chk.bindEvent(o.event, o.params, e)
			chk.noteParams(o.paramsForm, e)
			chk.forms(o.body, e)
		}
	}
	chk.pass = 0
	runBodies()
	chk.pass = 1
	runBodies()
	diags := mergeDiags(chk.diags)
	return CheckResult{Diagnostics: diags, Hints: chk.hints, Export: exportCheckedType(prog, chk)}
}

func exportCheckedType(prog runtime.Program, chk *checker) Type {
	seen := map[string]struct{}{}
	var names []string
	types := map[string]Type{}
	for _, f := range prog.Fns {
		if _, ok := seen[f.Name]; ok {
			continue
		}
		seen[f.Name] = struct{}{}
		names = append(names, f.Name)
		if t, ok := chk.fns[f.Name]; ok {
			types[f.Name] = t
		} else {
			types[f.Name] = tDyn(Any())
		}
	}
	for _, m := range prog.Macros {
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		names = append(names, m.Name)
		types[m.Name] = tDyn(Any())
	}
	sort.Strings(names)
	var fields []mapField
	for _, n := range names {
		fields = append(fields, mapField{name: n, t: types[n]})
	}
	if len(fields) == 0 {
		return EmptyMapType()
	}
	return tMap(fields, nil)
}

func (chk *checker) bindEvent(event string, params runtime.Params, env typeEnv) {
	spec, ok := chk.cfg.Events[event]
	if !ok {
		return
	}
	by := map[string]Type{}
	for _, k := range spec {
		by[k.Name] = k.Type
	}
	if params.Key {
		for _, kp := range params.Keys {
			if kp.Pat.Bind {
				if t, ok := by[kp.Name]; ok {
					env[kp.Pat.Name] = t
					if kp.Name != kp.Pat.Name {
						env[kp.Name] = t
					}
				}
			}
		}
		return
	}
	for i, p := range params.Pats {
		if !p.Bind || i >= len(spec) {
			continue
		}
		env[p.Name] = spec[i].Type
	}
}
