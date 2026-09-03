package runtime

import (
	"time"

	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

type ctx struct {
	rt       *Machine
	macros   map[string][]Clause
	macroEnv *env
	file     string
	depth    int
}

const maxEvalDepth = 8000

func newCtx(rt *Machine, env *env, macros map[string][]Clause) *ctx {
	file := ""
	if rt != nil {
		file = rt.file
	}
	if macros == nil {
		macros = map[string][]Clause{}
	}
	return &ctx{rt: rt, macros: macros, macroEnv: env, file: file}
}

func (c *ctx) tick() error {
	if c.rt != nil && c.rt.evalLimit > 0 {
		c.rt.budget--
		if c.rt.budget < 0 {
			return errMsg("script ran too long")
		}
	}
	return nil
}

func (c *ctx) push() error {
	c.depth++
	if c.depth > maxEvalDepth {
		return errMsg("script ran too long")
	}
	return c.tick()
}

func (c *ctx) pop() {
	if c.depth > 0 {
		c.depth--
	}
}

func (c *ctx) cloneBudget() *ctx {
	cp := *c
	cp.depth = 0
	return &cp
}

type callParts struct {
	pos  []Value
	keys map[string]Value
}

func evalCallRaw(raw []syntax.Form, env *env, c *ctx) (callParts, error) {
	out := callParts{keys: map[string]Value{}}
	keyed := false
	for i := 0; i < len(raw); {
		a := raw[i]
		if a.Kind() == syntax.KindComment {
			i++
			continue
		}
		if a.Kind() == syntax.KindSplice {
			if keyed {
				return callParts{}, errMsg("positional argument after key:")
			}
			inner, err := spliceInner(a.Inner(), env, c, false)
			if err != nil {
				return callParts{}, err
			}
			xs, err := asSpliceList(inner, a)
			if err != nil {
				return callParts{}, err
			}
			out.pos = append(out.pos, xs...)
			i++
			continue
		}
		if a.IsKey() {
			keyed = true
			name := a.KeyName()
			if i+1 >= len(raw) {
				return callParts{}, errf("missing value for %s", a.Name())
			}
			if _, ok := out.keys[name]; ok {
				return callParts{}, errf("duplicate %s", a.Name())
			}
			val, err := evalVal(raw[i+1], env, c)
			if err != nil {
				return callParts{}, err
			}
			out.keys[name] = val
			i += 2
			continue
		}
		if keyed {
			return callParts{}, errMsg("positional argument after key:")
		}
		val, err := evalVal(a, env, c)
		if err != nil {
			return callParts{}, err
		}
		out.pos = append(out.pos, val)
		i++
	}
	return out, nil
}

func evalVal(v syntax.Form, env *env, c *ctx) (Value, error) {
	if err := c.push(); err != nil {
		return Value{}, err
	}
	defer c.pop()
	switch v.Kind() {
	case syntax.KindComment:
		return Nil, nil
	case syntax.KindInt, syntax.KindFloat, syntax.KindString:
		return ValueFromLiteralForm(v), nil
	case syntax.KindQuote:
		return evalQuote(v.Inner(), env, c, 1)
	case syntax.KindUnquote:
		return Value{}, errForm(v, "comma not inside quote")
	case syntax.KindSplice:
		return Value{}, errForm(v, "@ needs a list to insert into")
	case syntax.KindSymbol:
		if syntax.ReservedLit(v) {
			return Symbol(v.Name()), nil
		}
		return lookup(env, v.Name())
	case syntax.KindMap:
		m := newMap()
		for _, pair := range v.Pairs() {
			val, err := evalVal(pair.Value, env, c)
			if err != nil {
				return Value{}, err
			}
			m.put(Symbol(pair.Key.Name()), val)
		}
		return Value{k: KindMap, p: m}, nil
	case syntax.KindList:
		if len(v.Items()) == 0 {
			if v.IsVec() {
				return List(), nil
			}
			return CallList(), nil
		}
		if v.IsVec() {
			xs, err := evalSpread(v.Items(), env, c, false)
			if err != nil {
				return Value{}, err
			}
			return List(xs...), nil
		}
		xs := syntax.FilterComments(v.Items())
		if len(xs) == 0 {
			return Nil, nil
		}
		head := xs[0]
		if head.Kind() != syntax.KindSplice && head.Kind() == syntax.KindSymbol {
			if sf, handled, err := special(head.Name(), xs[1:], env, c); handled {
				return sf, err
			}
			if _, ok := c.macros[head.Name()]; ok {
				ex, err := expandVal(v, env, c)
				if err != nil {
					return Value{}, err
				}
				return evalVal(ex, env, c)
			}
			if scanner.IsCoreBuiltin(head.Name()) {
				call, err := evalCallRaw(xs[1:], env, c)
				if err != nil {
					return Value{}, err
				}
				return callBuiltin(head.Name(), call, c)
			}
			if c.rt != nil {
				if b, ok := c.rt.extra[head.Name()]; ok {
					call, err := evalCallRaw(xs[1:], env, c)
					if err != nil {
						return Value{}, err
					}
					if len(call.keys) > 0 {
						return Value{}, errf("%s does not take keyword arguments", head.Name())
					}
					return c.rt.hostCall(func() (Value, error) { return b(call.pos) })
				}
			}
		}
		if head.Kind() == syntax.KindSplice {
			parts, err := evalSpread(xs, env, c, false)
			if err != nil {
				return Value{}, err
			}
			if len(parts) == 0 {
				return Nil, nil
			}
			return applyFn(parts[0], callParts{pos: parts[1:], keys: map[string]Value{}}, env, c)
		}
		fn, err := evalVal(head, env, c)
		if err != nil {
			return Value{}, err
		}
		if fn.k == KindMacro {
			return evalMacroCall(fn, xs[1:], env, c, v)
		}
		call, err := evalCallRaw(xs[1:], env, c)
		if err != nil {
			return Value{}, err
		}
		return applyFn(fn, call, env, c)
	default:
		return ValueFromLiteralForm(v), nil
	}
}

func spliceInner(v syntax.Form, env *env, c *ctx, inQuote bool) (Value, error) {
	if v.Kind() == syntax.KindUnquote {
		if !inQuote {
			return Value{}, errForm(v, "comma not inside quote")
		}
		return evalVal(v.Inner(), env, c)
	}
	return evalVal(v, env, c)
}

func asSpliceList(v Value, at syntax.Form) ([]Value, error) {
	if v.k != KindList {
		return nil, errForm(at, "@ needs a list")
	}
	return v.Items(), nil
}

func evalSpread(items []syntax.Form, env *env, c *ctx, inQuote bool) ([]Value, error) {
	var out []Value
	for _, a := range items {
		if a.Kind() == syntax.KindComment {
			continue
		}
		if a.Kind() == syntax.KindSplice {
			inner, err := spliceInner(a.Inner(), env, c, inQuote)
			if err != nil {
				return nil, err
			}
			xs, err := asSpliceList(inner, a)
			if err != nil {
				return nil, err
			}
			out = append(out, xs...)
			continue
		}
		val, err := evalVal(a, env, c)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
	return out, nil
}

func evalQuote(v syntax.Form, env *env, c *ctx, depth int) (Value, error) {
	switch v.Kind() {
	case syntax.KindUnquote:
		if depth == 1 {
			return evalVal(v.Inner(), env, c)
		}
		inner, err := evalQuote(v.Inner(), env, c, depth-1)
		if err != nil {
			return Value{}, err
		}
		return CallList(Symbol("unquote"), inner), nil
	case syntax.KindQuote:
		inner, err := evalQuote(v.Inner(), env, c, depth+1)
		if err != nil {
			return Value{}, err
		}
		return CallList(Symbol("quote"), inner), nil
	case syntax.KindSplice:
		if depth != 1 {
			inner, err := evalQuote(v.Inner(), env, c, depth)
			if err != nil {
				return Value{}, err
			}
			return CallList(Symbol("splice"), inner), nil
		}
		inner, err := spliceInner(v.Inner(), env, c, true)
		if err != nil {
			return Value{}, err
		}
		xs, err := asSpliceList(inner, v)
		if err != nil {
			return Value{}, err
		}
		return List(xs...), nil
	case syntax.KindList:
		var out []Value
		for _, item := range v.Items() {
			if item.Kind() == syntax.KindComment {
				continue
			}
			if item.Kind() == syntax.KindSplice && depth == 1 {
				inner, err := spliceInner(item.Inner(), env, c, true)
				if err != nil {
					return Value{}, err
				}
				xs, err := asSpliceList(inner, item)
				if err != nil {
					return Value{}, err
				}
				out = append(out, xs...)
				continue
			}
			q, err := evalQuote(item, env, c, depth)
			if err != nil {
				return Value{}, err
			}
			out = append(out, q)
		}
		if v.IsVec() {
			return List(out...), nil
		}
		return CallList(out...), nil
	case syntax.KindMap:
		m := newMap()
		for _, pair := range v.Pairs() {
			val, err := evalQuote(pair.Value, env, c, depth)
			if err != nil {
				return Value{}, err
			}
			m.put(Symbol(pair.Key.Name()), val)
		}
		return Value{k: KindMap, p: m}, nil
	case syntax.KindSymbol:
		return Symbol(v.Name()), nil
	default:
		return ValueFromLiteralForm(v), nil
	}
}

func special(name string, args []syntax.Form, env *env, c *ctx) (Value, bool, error) {
	switch name {
	case "if":
		clauses, err := parseIfArgs(args)
		if err != nil {
			return Value{}, true, err
		}
		for _, cl := range clauses {
			pass := cl.Test == nil
			if !pass {
				tv, err := evalVal(*cl.Test, env, c)
				if err != nil {
					return Value{}, true, err
				}
				if cl.Not {
					pass = !tv.Truthy()
				} else {
					pass = tv.Truthy()
				}
			}
			if pass {
				last := Nil
				for _, a := range cl.Body {
					var err error
					last, err = evalVal(a, env, c)
					if err != nil {
						return Value{}, true, err
					}
				}
				return last, true, nil
			}
		}
		return Nil, true, nil
	case "and":
		last := Bool(true)
		for _, a := range args {
			var err error
			last, err = evalVal(a, env, c)
			if err != nil {
				return Value{}, true, err
			}
			if !last.Truthy() {
				return last, true, nil
			}
		}
		return last, true, nil
	case "or":
		last := Bool(false)
		for _, a := range args {
			var err error
			last, err = evalVal(a, env, c)
			if err != nil {
				return Value{}, true, err
			}
			if last.Truthy() {
				return last, true, nil
			}
		}
		return last, true, nil
	case "not":
		x := Nil
		if len(args) > 0 {
			var err error
			x, err = evalVal(args[0], env, c)
			if err != nil {
				return Value{}, true, err
			}
		}
		return Bool(!x.Truthy()), true, nil
	case "def":
		return Value{}, true, errMsg("(def ...) only works at the top of a script")
	case "fn":
		v, err := makeFn(args, env)
		return v, true, err
	case "let", "let!":
		v, err := evalLet(args, env, c)
		return v, true, err
	case "defm":
		return Value{}, true, errMsg("(defm ...) only works at the top of a script")
	case "pipe":
		v, err := evalPipe(args, env, c)
		return v, true, err
	case "eval":
		if len(args) != 1 {
			return Value{}, true, errMsg("eval needs one form")
		}
		inner, err := evalVal(args[0], env, c)
		if err != nil {
			return Value{}, true, err
		}
		v, err := evalVal(FormFromValue(inner), env, c)
		return v, true, err
	case "after":
		secV := Int64(0)
		if len(args) > 0 {
			var err error
			secV, err = evalVal(args[0], env, c)
			if err != nil {
				return Value{}, true, err
			}
		}
		d, err := asDuration(secV)
		if err != nil {
			return Value{}, true, err
		}
		rt := c.rt
		if rt != nil && rt.checking {
			return Nil, true, nil
		}
		body := args[1:]
		later := c.cloneBudget()
		sched := defaultSched
		if rt != nil && rt.sched != nil {
			sched = rt.sched
		}
		if rt != nil {
			if rt.pendingAfter >= maxPendingAfter {
				return Value{}, true, errMsg("too many after callbacks")
			}
			rt.pendingAfter++
		}
		envCap, bodyCap := env, append([]syntax.Form(nil), body...)
		sched(d, func() {
			if rt != nil {
				rt.mu.Lock()
				defer rt.mu.Unlock()
				if rt.pendingAfter > 0 {
					rt.pendingAfter--
				}
			}
			for _, b := range bodyCap {
				_, err := evalVal(b, envCap, later)
				if err != nil && rt != nil && rt.onAfterErr != nil {
					hook := rt.onAfterErr
					rt.mu.Unlock()
					func() {
						defer rt.mu.Lock()
						hook(err)
					}()
				}
			}
		})
		return Nil, true, nil
	case "on":
		return Value{}, true, errMsg("(on ...) only works at the top of a script")
	case "import":
		v, err := evalImport(args, env, c)
		return v, true, err
	case ".":
		v, err := evalDot(args, env, c)
		return v, true, err
	default:
		return Value{}, false, nil
	}
}

func evalDot(args []syntax.Form, env *env, c *ctx) (Value, error) {
	args = syntax.FilterComments(args)
	if len(args) < 2 {
		return Value{}, errMsg(". needs a map and a name")
	}
	cur, err := evalVal(args[0], env, c)
	if err != nil {
		return Value{}, err
	}
	for _, k := range args[1:] {
		if k.Kind() != syntax.KindSymbol || k.IsKey() {
			return Value{}, errForm(k, ". key must be a name")
		}
		if cur.k != KindMap {
			return Value{}, errForm(k, ". needs a map")
		}
		name := k.Name()
		v, ok := cur.mapData().get(name)
		if !ok {
			return Value{}, errFormf(k, "unknown field %s", name)
		}
		cur = v
	}
	return cur, nil
}

func makeFn(args []syntax.Form, env *env) (Value, error) {
	parsed, err := parseFn(args)
	if err != nil {
		return Value{}, err
	}
	clauses := make([]Clause, len(parsed.clauses))
	for i, cl := range parsed.clauses {
		clauses[i] = Clause{Params: cl.Params, Body: cl.Body}
	}
	return makeFnVal(clauses, env), nil
}

func evalLet(args []syntax.Form, env *env, c *ctx) (Value, error) {
	if len(args) == 0 {
		return Value{}, errMsg("(let map body...)")
	}
	m, err := evalVal(args[0], env, c)
	if err != nil {
		return Value{}, err
	}
	if m.k != KindMap {
		return Value{}, errMsg("let needs a map")
	}
	child := makeEnv(env)
	if m.mapData() != nil {
		for i, k := range m.mapData().keys {
			child.set(k.Name(), m.mapData().vals[i])
		}
	}
	last := Nil
	for _, a := range args[1:] {
		last, err = evalVal(a, child, c)
		if err != nil {
			return Value{}, err
		}
	}
	return last, nil
}

func pipeStepForm(step syntax.Form, cur Value) (syntax.Form, error) {
	q := syntax.Quote(FormFromValue(cur))
	if step.Kind() == syntax.KindSymbol {
		return syntax.CallList(step, q), nil
	}
	if step.Kind() != syntax.KindList || step.IsVec() {
		return syntax.Form{}, errMsg("pipe step must be a call or a name")
	}
	xs := syntax.FilterComments(step.Items())
	if len(xs) == 0 {
		return syntax.Form{}, errMsg("pipe step must be a call or a name")
	}
	for _, a := range xs[1:] {
		if a.IsKey() {
			return syntax.Form{}, errMsg("pipe steps cannot use name: arguments")
		}
	}
	out := make([]syntax.Form, 0, len(xs)+1)
	out = append(out, xs[0], q)
	out = append(out, xs[1:]...)
	return syntax.CallList(out...), nil
}

func evalPipe(args []syntax.Form, env *env, c *ctx) (Value, error) {
	steps := syntax.FilterComments(args)
	if len(steps) < 2 {
		return Value{}, errMsg("pipe needs a value and at least one step")
	}
	cur, err := evalVal(steps[0], env, c)
	if err != nil {
		return Value{}, err
	}
	for _, step := range steps[1:] {
		form, err := pipeStepForm(step, cur)
		if err != nil {
			return Value{}, err
		}
		cur, err = evalVal(form, env, c)
		if err != nil {
			return Value{}, err
		}
	}
	return cur, nil
}

func evalMacroCall(mac Value, raw []syntax.Form, env *env, c *ctx, call syntax.Form) (Value, error) {
	f := mac.fnData()
	if f == nil {
		return Value{}, errMsg("not a macro")
	}
	name := f.name
	if name == "" {
		name = "macro"
	}
	frags, err := applyMacro(name, f, raw, c, call)
	if err != nil {
		return Value{}, err
	}
	expanded, err := expandForms(frags, env, c)
	if err != nil {
		return Value{}, err
	}
	return evalVal(packExpr(expanded, call), env, c)
}

func applyFn(fn Value, call callParts, env *env, c *ctx) (Value, error) {
	if fn.k == KindMacro {
		who := "macro"
		if f := fn.fnData(); f != nil && f.name != "" {
			who = f.name
		}
		return Value{}, errf("%s is a macro", who)
	}
	if fn.k == KindFn && fn.fnData() != nil {
		f := fn.fnData()
		if f.native != nil {
			if len(call.keys) > 0 {
				return Value{}, errMsg("this function does not take keyword arguments")
			}
			callNative := func() (Value, error) { return f.native(call.pos) }
			if c != nil && c.rt != nil {
				return c.rt.hostCall(callNative)
			}
			return callNative()
		}
		if len(f.keys) > 0 {
			allow := map[string]struct{}{}
			for _, k := range f.keys {
				allow[k] = struct{}{}
			}
			for k := range call.keys {
				if _, ok := allow[k]; !ok {
					return Value{}, errf("unknown %s:", k)
				}
			}
		}
		allPos, allKey := true, true
		for _, cl := range f.clauses {
			if cl.Params.Key {
				allPos = false
			} else {
				allKey = false
			}
		}
		if allPos && len(call.keys) > 0 {
			return Value{}, errMsg("this function does not take keyword arguments")
		}
		if allKey && len(call.pos) > 0 {
			return Value{}, errMsg("this function needs key: arguments")
		}
		for _, clause := range f.clauses {
			child := makeEnv(f.env)
			if !tryBind(clause.Params, call, child) {
				continue
			}
			last := Nil
			for _, b := range clause.Body {
				var err error
				last, err = evalVal(b, child, c)
				if err != nil {
					return Value{}, err
				}
			}
			return last, nil
		}
		return Value{}, errMsg("no matching clause")
	}
	if fn.k == KindSymbol {
		return callBuiltin(fn.Name(), call, c)
	}
	return Value{}, errMsg("not a function")
}

func bindPat(pat Pattern, val Value, env *env) bool {
	if !pat.Bind {
		return pat.Value.Equal(val)
	}
	env.set(pat.Name, val)
	return true
}

func tryBind(params Params, call callParts, env *env) bool {
	if !params.Key {
		if len(call.keys) > 0 {
			return false
		}
		if params.Rest != "" {
			if len(call.pos) < len(params.Pats) {
				return false
			}
			for i, p := range params.Pats {
				if !bindPat(p, call.pos[i], env) {
					return false
				}
			}
			env.set(params.Rest, List(call.pos[len(params.Pats):]...))
			return true
		}
		if len(call.pos) != len(params.Pats) {
			return false
		}
		for i, p := range params.Pats {
			if !bindPat(p, call.pos[i], env) {
				return false
			}
		}
		return true
	}
	if len(call.pos) > 0 {
		return false
	}
	declared := map[string]struct{}{}
	for _, kp := range params.Keys {
		declared[kp.Name] = struct{}{}
	}
	for k := range call.keys {
		if _, ok := declared[k]; !ok {
			return false
		}
	}
	for _, kp := range params.Keys {
		val, ok := call.keys[kp.Name]
		if !ok {
			val = Nil
		}
		if !bindPat(kp.Pat, val, env) {
			return false
		}
		if !kp.Pat.Bind {
			env.set(kp.Name, val)
		} else if kp.Pat.Name != kp.Name {
			env.set(kp.Name, val)
		}
	}
	return true
}

func evalForms(forms []syntax.Form, env *env, c *ctx) (Value, error) {
	last := Nil
	for _, f := range forms {
		var err error
		last, err = evalVal(f, env, c)
		if err != nil {
			return Value{}, err
		}
	}
	return last, nil
}

func asDuration(v Value) (time.Duration, error) {
	f, ok := v.AsFloat64()
	if !ok {
		return 0, errMsg("after needs a number")
	}
	if f < 0 {
		f = 0
	}
	return time.Duration(f * float64(time.Second)), nil
}

func defaultSched(d time.Duration, fn func()) {
	time.AfterFunc(d, fn)
}

func callPos(fn Value, args []Value, c *ctx) (Value, error) {
	return applyFn(fn, callParts{pos: args, keys: map[string]Value{}}, makeEnv(nil), c)
}
