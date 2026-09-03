package runtime

import (
	"time"

	"deedles.dev/writ/scanner"
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

func evalCallRaw(raw []Value, env *env, c *ctx) (callParts, error) {
	out := callParts{keys: map[string]Value{}}
	keyed := false
	for i := 0; i < len(raw); {
		a := raw[i]
		if a.k == KindComment {
			i++
			continue
		}
		if a.k == KindSplice {
			if keyed {
				return callParts{}, errMsg("positional argument after key:")
			}
			inner, err := spliceInner(a.innerVal(), env, c, false)
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
		if a.isKeySym() {
			keyed = true
			name := a.keyName()
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

func evalVal(v Value, env *env, c *ctx) (Value, error) {
	if err := c.push(); err != nil {
		return Value{}, err
	}
	defer c.pop()
	switch v.k {
	case KindComment:
		return Nil, nil
	case KindInt, KindFloat, KindString, KindFn, KindMacro, KindNative:
		return v, nil
	case KindQuote:
		return evalQuote(v.innerVal(), env, c, 1)
	case KindUnquote:
		return Value{}, errVal(v, "comma not inside quote")
	case KindSplice:
		return Value{}, errVal(v, "@ needs a list to insert into")
	case KindSymbol:
		if reservedLit(v) {
			return Symbol(v.Name()), nil
		}
		return lookup(env, v.Name())
	case KindMap:
		m := newMap()
		if v.mapData() != nil {
			for i, k := range v.mapData().keys {
				val, err := evalVal(v.mapData().vals[i], env, c)
				if err != nil {
					return Value{}, err
				}
				m.put(k, val)
			}
		}
		return Value{k: KindMap, p: m}, nil
	case KindList:
		if len(v.Items()) == 0 {
			return v, nil
		}
		if v.IsVec() {
			xs, err := evalSpread(v.Items(), env, c, false)
			if err != nil {
				return Value{}, err
			}
			return List(xs...), nil
		}
		xs := filterComments(v.Items())
		if len(xs) == 0 {
			return Nil, nil
		}
		head := xs[0]
		if head.k != KindSplice && head.k == KindSymbol {
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
		if head.k == KindSplice {
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
		return v, nil
	}
}

func spliceInner(v Value, env *env, c *ctx, inQuote bool) (Value, error) {
	if v.k == KindUnquote {
		if !inQuote {
			return Value{}, errVal(v, "comma not inside quote")
		}
		return evalVal(v.innerVal(), env, c)
	}
	return evalVal(v, env, c)
}

func asSpliceList(v Value, at Value) ([]Value, error) {
	if v.k != KindList {
		return nil, errVal(at, "@ needs a list")
	}
	return filterComments(v.Items()), nil
}

func evalSpread(items []Value, env *env, c *ctx, inQuote bool) ([]Value, error) {
	var out []Value
	for _, a := range items {
		if a.k == KindComment {
			continue
		}
		if a.k == KindSplice {
			inner, err := spliceInner(a.innerVal(), env, c, inQuote)
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

func evalQuote(v Value, env *env, c *ctx, depth int) (Value, error) {
	switch v.k {
	case KindUnquote:
		if depth == 1 {
			return evalVal(v.innerVal(), env, c)
		}
		inner, err := evalQuote(v.innerVal(), env, c, depth-1)
		if err != nil {
			return Value{}, err
		}
		return Unquote(inner), nil
	case KindQuote:
		inner, err := evalQuote(v.innerVal(), env, c, depth+1)
		if err != nil {
			return Value{}, err
		}
		return Quote(inner), nil
	case KindSplice:
		if depth != 1 {
			inner, err := evalQuote(v.innerVal(), env, c, depth)
			if err != nil {
				return Value{}, err
			}
			return Splice(inner), nil
		}
		inner, err := spliceInner(v.innerVal(), env, c, true)
		if err != nil {
			return Value{}, err
		}
		xs, err := asSpliceList(inner, v)
		if err != nil {
			return Value{}, err
		}
		return List(xs...), nil
	case KindList:
		var out []Value
		for _, item := range v.Items() {
			if item.k == KindComment {
				out = append(out, item)
				continue
			}
			if item.k == KindSplice && depth == 1 {
				inner, err := spliceInner(item.innerVal(), env, c, true)
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
	case KindMap:
		m := newMap()
		if v.mapData() != nil {
			for i, k := range v.mapData().keys {
				val, err := evalQuote(v.mapData().vals[i], env, c, depth)
				if err != nil {
					return Value{}, err
				}
				m.put(k, val)
			}
		}
		return Value{k: KindMap, p: m}, nil
	case KindSymbol:
		return Symbol(v.Name()), nil
	default:
		return v, nil
	}
}

func special(name string, args []Value, env *env, c *ctx) (Value, bool, error) {
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
		v, err := evalVal(inner, env, c)
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
		envCap, bodyCap := env, append([]Value(nil), body...)
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

func evalDot(args []Value, env *env, c *ctx) (Value, error) {
	args = filterComments(args)
	if len(args) < 2 {
		return Value{}, errMsg(". needs a map and a name")
	}
	cur, err := evalVal(args[0], env, c)
	if err != nil {
		return Value{}, err
	}
	for _, k := range args[1:] {
		if k.k != KindSymbol || k.isKeySym() {
			return Value{}, errVal(k, ". key must be a name")
		}
		if cur.k != KindMap {
			return Value{}, errVal(k, ". needs a map")
		}
		name := k.Name()
		v, ok := cur.mapData().get(name)
		if !ok {
			return Value{}, errValf(k, "unknown field %s", name)
		}
		cur = v
	}
	return cur, nil
}

func makeFn(args []Value, env *env) (Value, error) {
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

func evalLet(args []Value, env *env, c *ctx) (Value, error) {
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

func quotedVal(v Value) Value { return Quote(v) }

func pipeStepForm(step, cur Value) (Value, error) {
	q := quotedVal(cur)
	if step.k == KindSymbol {
		return CallList(step, q), nil
	}
	if step.k != KindList || step.IsVec() {
		return Value{}, errMsg("pipe step must be a call or a name")
	}
	xs := filterComments(step.Items())
	if len(xs) == 0 {
		return Value{}, errMsg("pipe step must be a call or a name")
	}
	for _, a := range xs[1:] {
		if a.isKeySym() {
			return Value{}, errMsg("pipe steps cannot use name: arguments")
		}
	}
	out := make([]Value, 0, len(xs)+1)
	out = append(out, xs[0], q)
	out = append(out, xs[1:]...)
	return CallList(out...), nil
}

func evalPipe(args []Value, env *env, c *ctx) (Value, error) {
	steps := filterComments(args)
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

func evalMacroCall(mac Value, raw []Value, env *env, c *ctx, call Value) (Value, error) {
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

func evalForms(forms []Value, env *env, c *ctx) (Value, error) {
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
