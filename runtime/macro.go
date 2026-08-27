package runtime

func collectImported(v Value, into map[*Value]struct{}, nodes *[]Value) {
	for _, n := range *nodes {
		if valuePtrEq(n, v) {
			return
		}
	}
	*nodes = append(*nodes, v)
	switch v.k {
	case KindList:
		for _, x := range v.xs {
			collectImported(x, into, nodes)
		}
	case KindMap:
		if v.mp == nil {
			return
		}
		for _, x := range v.mp.vals {
			collectImported(x, into, nodes)
		}
	case KindQuote, KindUnquote, KindSplice:
		collectImported(v.innerVal(), into, nodes)
	}
}

func valuePtrEq(a, b Value) bool {
	if a.k != b.k {
		return false
	}
	switch a.k {
	case KindList:
		if len(a.xs) != len(b.xs) {
			return false
		}
		if len(a.xs) > 0 && len(b.xs) > 0 && &a.xs[0] == &b.xs[0] && a.vec == b.vec {
			return true
		}
		return false
	case KindMap:
		return a.mp != nil && a.mp == b.mp
	case KindQuote, KindUnquote, KindSplice:
		return a.inner != nil && a.inner == b.inner
	case KindSymbol:
		return a.sym == b.sym && a.hasSpan == b.hasSpan && a.span == b.span
	case KindString, KindComment:
		return a.s == b.s && a.hasSpan == b.hasSpan && a.span == b.span
	case KindInt:
		return a.Equal(b) && a.hasSpan == b.hasSpan && a.span == b.span
	case KindFloat:
		return a.f == b.f && a.hasSpan == b.hasSpan && a.span == b.span
	default:
		return false
	}
}

func importedHas(nodes []Value, v Value) bool {
	for _, n := range nodes {
		if valuePtrEq(n, v) {
			return true
		}
	}
	return false
}

type hygState struct{ seq int }

func (h *hygState) fresh(name string) string {
	h.seq++
	return name + "#m" + itoa(h.seq)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func hygienic(v Value, imported []Value, subst map[string]string, h *hygState) Value {
	if importedHas(imported, v) {
		return v
	}
	switch v.k {
	case KindSymbol:
		if n, ok := subst[v.Name()]; ok {
			return v.withSymName(n)
		}
		return v
	case KindQuote, KindUnquote, KindSplice:
		return v.setInner(hygienic(v.innerVal(), imported, subst, h))
	case KindMap:
		if v.mp == nil {
			return v
		}
		m := newMap()
		for i, k := range v.mp.keys {
			m.put(k, hygienic(v.mp.vals[i], imported, subst, h))
		}
		out := v
		out.mp = m
		return out
	case KindList:
		name := ""
		if len(v.xs) > 0 && v.xs[0].k == KindSymbol {
			name = v.xs[0].Name()
		}
		if (name == "let" || name == "let!") && len(v.xs) >= 1 {
			exported := name == "let!"
			var binds Value
			if len(v.xs) > 1 {
				binds = v.xs[1]
			}
			next := subst
			newBinds := binds
			hasBinds := len(v.xs) > 1
			if hasBinds && !importedHas(imported, binds) && binds.k == KindMap && !exported {
				next = copySubst(subst)
				m := newMap()
				if binds.mp != nil {
					for i, k := range binds.mp.keys {
						nk := h.fresh(k)
						next[k] = nk
						m.put(nk, hygienic(binds.mp.vals[i], imported, subst, h))
					}
				}
				newBinds = binds
				newBinds.mp = m
			} else if hasBinds {
				newBinds = hygienic(binds, imported, subst, h)
			}
			body := make([]Value, 0, len(v.xs))
			head := v.xs[0]
			if head.k == KindSymbol {
				head = head.withSymName("let")
			} else {
				head = Symbol("let")
			}
			body = append(body, head)
			if hasBinds {
				body = append(body, newBinds)
			}
			for _, x := range v.xs[2:] {
				body = append(body, hygienic(x, imported, next, h))
			}
			out := v
			out.xs = body
			return out
		}
		if name == "fn" || name == "def" || name == "defm" || name == "on" {
			return hygienicFnLike(v, imported, subst, name, h)
		}
		xs := make([]Value, len(v.xs))
		for i, x := range v.xs {
			xs[i] = hygienic(x, imported, subst, h)
		}
		out := v
		out.xs = xs
		return out
	default:
		return v
	}
}

func copySubst(s map[string]string) map[string]string {
	out := make(map[string]string, len(s)+4)
	for k, v := range s {
		out[k] = v
	}
	return out
}

func renameParamForm(form Value, imported []Value, subst map[string]string, h *hygState) (Value, map[string]string) {
	if importedHas(imported, form) || form.k != KindList {
		return hygienic(form, imported, subst, h), subst
	}
	next := copySubst(subst)
	out := make([]Value, 0, len(form.xs))
	for _, p := range form.xs {
		if p.k == KindSplice && p.innerVal().k == KindSymbol {
			old := p.innerVal().Name()
			nk := h.fresh(old)
			next[old] = nk
			out = append(out, p.setInner(p.innerVal().withSymName(nk)))
			continue
		}
		if p.k == KindSymbol && !p.isKeySym() {
			if reservedLit(p) {
				out = append(out, p)
				continue
			}
			nk := h.fresh(p.Name())
			next[p.Name()] = nk
			out = append(out, p.withSymName(nk))
			continue
		}
		if p.isKeySym() {
			raw := p.keyName()
			nk := h.fresh(raw)
			next[raw] = nk
			out = append(out, p.withSymName(nk+":"))
			continue
		}
		out = append(out, hygienic(p, imported, subst, h))
	}
	form.xs = out
	return form, next
}

func hygienicFnLike(v Value, imported []Value, subst map[string]string, name string, h *hygState) Value {
	xs := v.xs
	if name == "fn" {
		parsed, err := parseFn(xs[1:])
		if err != nil {
			out := make([]Value, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v.xs = out
			return v
		}
		rebuilt := []Value{xs[0]}
		for _, c := range parsed.clauses {
			if c.ParamsForm != nil {
				form, next := renameParamForm(*c.ParamsForm, imported, subst, h)
				rebuilt = append(rebuilt, form)
				for _, b := range c.Body {
					rebuilt = append(rebuilt, hygienic(b, imported, next, h))
				}
			} else {
				for _, b := range c.Body {
					rebuilt = append(rebuilt, hygienic(b, imported, subst, h))
				}
			}
			rebuilt = append(rebuilt, Symbol("fn"))
		}
		if n := len(rebuilt); n > 0 && isSymName(rebuilt[n-1], "fn") {
			rebuilt = rebuilt[:n-1]
		}
		v.xs = rebuilt
		return v
	}
	var nm Value
	if len(xs) > 1 {
		nm = xs[1]
	}
	head := xs[0]
	if len(xs) > 0 {
		head = hygienic(xs[0], imported, subst, h)
	}
	if name == "def" || name == "defm" {
		if nm.k != KindList || len(nm.xs) == 0 {
			out := make([]Value, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v.xs = out
			return v
		}
		nameForm := nm.xs[0]
		paramsOnly := CallList(nm.xs[1:]...)
		paramsOnly.xs = nm.xs[1:]
		if nm.hasSpan {
			paramsOnly = paramsOnly.withSpan(nm.span.Start, nm.span.End)
		}
		form, next := renameParamForm(paramsOnly, imported, subst, h)
		renamed := form.xs
		newHead := nm
		newHead.xs = append([]Value{nameForm}, renamed...)
		body := make([]Value, 0, len(xs)-1)
		body = append(body, head, newHead)
		for _, b := range xs[2:] {
			body = append(body, hygienic(b, imported, next, h))
		}
		v.xs = body
		return v
	}
	var params Value
	hasParams := false
	if name == "on" && len(xs) > 2 {
		params = xs[2]
		hasParams = true
	}
	if !hasParams {
		out := make([]Value, len(xs))
		for i, x := range xs {
			out[i] = hygienic(x, imported, subst, h)
		}
		v.xs = out
		return v
	}
	form, next := renameParamForm(params, imported, subst, h)
	body := []Value{head, nm, form}
	for _, b := range xs[3:] {
		body = append(body, hygienic(b, imported, next, h))
	}
	v.xs = body
	return v
}

func applyMacro(name string, clauses []Clause, raw []Value, c *ctx, call Value) (Value, error) {
	parsed, err := parseCallRaw(raw)
	if err != nil {
		e := asError(err)
		if e.Start == 0 && e.End == 0 && call.hasSpan {
			e.Start, e.End = call.span.Start, call.span.End
		}
		return Value{}, e
	}
	parts := callParts{pos: parsed.pos, keys: map[string]Value{}}
	for _, k := range parsed.keys {
		parts.keys[k.name] = k.raw
	}
	allPos, allKey := true, true
	for _, cl := range clauses {
		if cl.Params.Key {
			allPos = false
		} else {
			allKey = false
		}
	}
	if allPos && len(parts.keys) > 0 {
		return Value{}, errVal(call, "this macro does not take keyword arguments")
	}
	if allKey && len(parts.pos) > 0 {
		return Value{}, errVal(call, "this macro needs key: arguments")
	}
	for _, clause := range clauses {
		child := makeEnv(c.macroEnv)
		if !tryBind(clause.Params, parts, child) {
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
		var imported []Value
		for _, a := range raw {
			collectImported(a, nil, &imported)
		}
		out := hygienic(last, imported, map[string]string{}, &hygState{})
		if !out.hasSpan && call.hasSpan {
			out = out.withSpan(call.span.Start, call.span.End)
		}
		return out, nil
	}
	return Value{}, errValf(call, "no matching clause for %s", name)
}

func expandVal(v Value, env *env, c *ctx) (Value, error) {
	if err := c.push(); err != nil {
		return Value{}, err
	}
	defer c.pop()
	switch v.k {
	case KindComment, KindInt, KindFloat, KindString, KindFn, KindQuote, KindUnquote, KindSplice, KindSymbol:
		return v, nil
	case KindMap:
		if v.mp == nil {
			return v, nil
		}
		m := newMap()
		for i, k := range v.mp.keys {
			val, err := expandVal(v.mp.vals[i], env, c)
			if err != nil {
				return Value{}, err
			}
			m.put(k, val)
		}
		out := v
		out.mp = m
		return out, nil
	case KindList:
		if v.vec {
			xs := make([]Value, len(v.xs))
			for i, x := range v.xs {
				var err error
				xs[i], err = expandVal(x, env, c)
				if err != nil {
					return Value{}, err
				}
			}
			out := v
			out.xs = xs
			return out, nil
		}
		xs := filterComments(v.xs)
		if len(xs) == 0 {
			return v, nil
		}
		head := xs[0]
		if head.k == KindSymbol {
			if clauses, ok := c.macros[head.Name()]; ok {
				expanded, err := applyMacro(head.Name(), clauses, xs[1:], c, v)
				if err != nil {
					return Value{}, err
				}
				return expandVal(expanded, env, c)
			}
		}
		outxs := make([]Value, len(v.xs))
		for i, x := range v.xs {
			var err error
			outxs[i], err = expandVal(x, env, c)
			if err != nil {
				return Value{}, err
			}
		}
		out := v
		out.xs = outxs
		return out, nil
	default:
		return v, nil
	}
}
