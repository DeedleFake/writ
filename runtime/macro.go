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
		for _, x := range v.Items() {
			collectImported(x, into, nodes)
		}
	case KindMap:
		if v.mapData() == nil {
			return
		}
		for _, x := range v.mapData().vals {
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
		if len(a.Items()) != len(b.Items()) {
			return false
		}
		if len(a.Items()) > 0 && len(b.Items()) > 0 && &a.Items()[0] == &b.Items()[0] && a.IsVec() == b.IsVec() {
			return true
		}
		return false
	case KindMap:
		return a.mapData() != nil && a.mapData() == b.mapData()
	case KindQuote, KindUnquote, KindSplice:
		return a.innerPtr() != nil && a.innerPtr() == b.innerPtr()
	case KindSymbol:
		return a.h == b.h && a.HasSpan() == b.HasSpan() && a.srcSpan() == b.srcSpan()
	case KindString, KindComment:
		return a.s == b.s && a.HasSpan() == b.HasSpan() && a.srcSpan() == b.srcSpan()
	case KindInt:
		return a.Equal(b) && a.HasSpan() == b.HasSpan() && a.srcSpan() == b.srcSpan()
	case KindFloat:
		return a.floatVal() == b.floatVal() && a.HasSpan() == b.HasSpan() && a.srcSpan() == b.srcSpan()
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
		if v.mapData() == nil {
			return v
		}
		m := newMap()
		for i, k := range v.mapData().keys {
			m.put(k, hygienic(v.mapData().vals[i], imported, subst, h))
		}
		out := v
		out = out.withMap(m)
		return out
	case KindList:
		name := ""
		if len(v.Items()) > 0 && v.Items()[0].k == KindSymbol {
			name = v.Items()[0].Name()
		}
		if (name == "let" || name == "let!") && len(v.Items()) >= 1 {
			exported := name == "let!"
			var binds Value
			if len(v.Items()) > 1 {
				binds = v.Items()[1]
			}
			next := subst
			newBinds := binds
			hasBinds := len(v.Items()) > 1
			if hasBinds && !importedHas(imported, binds) && binds.k == KindMap && !exported {
				next = copySubst(subst)
				m := newMap()
				if binds.mapData() != nil {
					for i, k := range binds.mapData().keys {
						old := k.Name()
						nk := h.fresh(old)
						next[old] = nk
						m.put(Symbol(nk), hygienic(binds.mapData().vals[i], imported, subst, h))
					}
				}
				newBinds = binds
				newBinds = newBinds.withMap(m)
			} else if hasBinds {
				newBinds = hygienic(binds, imported, subst, h)
			}
			body := make([]Value, 0, len(v.Items()))
			head := v.Items()[0]
			if head.k == KindSymbol {
				head = head.withSymName("let")
			} else {
				head = Symbol("let")
			}
			body = append(body, head)
			if hasBinds {
				body = append(body, newBinds)
			}
			for _, x := range v.Items()[2:] {
				body = append(body, hygienic(x, imported, next, h))
			}
			out := v
			out = out.withItems(body)
			return out
		}
		if name == "fn" || name == "def" || name == "defm" || name == "on" {
			return hygienicFnLike(v, imported, subst, name, h)
		}
		xs := make([]Value, len(v.Items()))
		for i, x := range v.Items() {
			xs[i] = hygienic(x, imported, subst, h)
		}
		out := v
		out = out.withItems(xs)
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
	out := make([]Value, 0, len(form.Items()))
	for _, p := range form.Items() {
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
	form = form.withItems(out)
	return form, next
}

func hygienicFnLike(v Value, imported []Value, subst map[string]string, name string, h *hygState) Value {
	xs := v.Items()
	if name == "fn" {
		parsed, err := parseFn(xs[1:])
		if err != nil {
			out := make([]Value, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v = v.withItems(out)
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
		v = v.withItems(rebuilt)
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
		if nm.k != KindList || len(nm.Items()) == 0 {
			out := make([]Value, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v = v.withItems(out)
			return v
		}
		nameForm := nm.Items()[0]
		paramsOnly := CallList(nm.Items()[1:]...)
		paramsOnly = paramsOnly.withItems(nm.Items()[1:])
		if nm.HasSpan() {
			paramsOnly = paramsOnly.withSpan(nm.srcSpan().Start, nm.srcSpan().End)
		}
		form, next := renameParamForm(paramsOnly, imported, subst, h)
		renamed := form.Items()
		newHead := nm
		newHead = newHead.withItems(append([]Value{nameForm}, renamed...))
		body := make([]Value, 0, len(xs)-1)
		body = append(body, head, newHead)
		for _, b := range xs[2:] {
			body = append(body, hygienic(b, imported, next, h))
		}
		v = v.withItems(body)
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
		v = v.withItems(out)
		return v
	}
	form, next := renameParamForm(params, imported, subst, h)
	body := []Value{head, nm, form}
	for _, b := range xs[3:] {
		body = append(body, hygienic(b, imported, next, h))
	}
	v = v.withItems(body)
	return v
}

func applyMacro(name string, clauses []Clause, raw []Value, c *ctx, call Value) ([]Value, error) {
	parsed, err := parseCallRaw(raw)
	if err != nil {
		e := asError(err)
		if e.Start == 0 && e.End == 0 && call.HasSpan() {
			e.Start, e.End = call.srcSpan().Start, call.srcSpan().End
		}
		return nil, e
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
		return nil, errVal(call, "this macro does not take keyword arguments")
	}
	if allKey && len(parts.pos) > 0 {
		return nil, errVal(call, "this macro needs key: arguments")
	}
	for _, clause := range clauses {
		child := makeEnv(c.macroEnv)
		if !tryBind(clause.Params, parts, child) {
			continue
		}
		frags, err := evalSpread(clause.Body, child, c, false)
		if err != nil {
			return nil, err
		}
		var imported []Value
		for _, a := range raw {
			collectImported(a, nil, &imported)
		}
		h := &hygState{}
		out := make([]Value, len(frags))
		for i, f := range frags {
			out[i] = hygienic(f, imported, map[string]string{}, h)
			if !out[i].HasSpan() && call.HasSpan() {
				out[i] = out[i].withSpan(call.srcSpan().Start, call.srcSpan().End)
			}
		}
		return out, nil
	}
	return nil, errValf(call, "no matching clause for %s", name)
}

func asMacroCall(v Value, c *ctx) (name string, clauses []Clause, args []Value, ok bool) {
	if v.k != KindList || v.IsVec() {
		return "", nil, nil, false
	}
	xs := filterComments(v.Items())
	if len(xs) == 0 || xs[0].k != KindSymbol {
		return "", nil, nil, false
	}
	name = xs[0].Name()
	clauses, ok = c.macros[name]
	if !ok {
		return "", nil, nil, false
	}
	return name, clauses, xs[1:], true
}

func packExpr(forms []Value, call Value) Value {
	switch len(forms) {
	case 0:
		out := Nil
		if call.HasSpan() {
			out = out.withSpan(call.srcSpan().Start, call.srcSpan().End)
		}
		return out
	case 1:
		return forms[0]
	default:
		xs := make([]Value, 0, 2+len(forms))
		xs = append(xs, Symbol("let"), EmptyMap())
		xs = append(xs, forms...)
		out := CallList(xs...)
		if call.HasSpan() {
			out = out.withSpan(call.srcSpan().Start, call.srcSpan().End)
		}
		return out
	}
}

func expandVal(v Value, env *env, c *ctx) (Value, error) {
	if err := c.push(); err != nil {
		return Value{}, err
	}
	defer c.pop()
	xs, err := expandIn(v, env, c)
	if err != nil {
		return Value{}, err
	}
	return packExpr(xs, v), nil
}

func expandForms(forms []Value, env *env, c *ctx) ([]Value, error) {
	var out []Value
	for _, form := range forms {
		if form.k == KindComment {
			out = append(out, form)
			continue
		}
		if err := c.push(); err != nil {
			return nil, err
		}
		xs, err := expandIn(form, env, c)
		c.pop()
		if err != nil {
			return nil, err
		}
		out = append(out, xs...)
	}
	return out, nil
}

func expandIn(v Value, env *env, c *ctx) ([]Value, error) {
	if name, clauses, args, ok := asMacroCall(v, c); ok {
		frags, err := applyMacro(name, clauses, args, c, v)
		if err != nil {
			return nil, err
		}
		return expandForms(frags, env, c)
	}
	ex, err := expandTree(v, env, c)
	if err != nil {
		return nil, err
	}
	return []Value{ex}, nil
}

func expandTree(v Value, env *env, c *ctx) (Value, error) {
	switch v.k {
	case KindComment, KindInt, KindFloat, KindString, KindFn, KindNative, KindQuote, KindUnquote, KindSplice, KindSymbol:
		return v, nil
	case KindMap:
		if v.mapData() == nil {
			return v, nil
		}
		m := newMap()
		for i, k := range v.mapData().keys {
			val, err := expandVal(v.mapData().vals[i], env, c)
			if err != nil {
				return Value{}, err
			}
			m.put(k, val)
		}
		out := v
		out = out.withMap(m)
		return out, nil
	case KindList:
		if v.IsVec() {
			return expandElems(v, env, c)
		}
		xs := filterComments(v.Items())
		if len(xs) == 0 {
			return v, nil
		}
		if xs[0].k == KindSymbol {
			switch xs[0].Name() {
			case "let", "let!":
				return expandLet(v, env, c)
			case "fn":
				return expandFn(v, env, c)
			case "if":
				return expandIf(v, env, c)
			case "after":
				return expandAfter(v, env, c)
			}
		}
		return expandElems(v, env, c)
	default:
		return v, nil
	}
}

func expandElems(v Value, env *env, c *ctx) (Value, error) {
	xs := make([]Value, len(v.Items()))
	for i, x := range v.Items() {
		var err error
		xs[i], err = expandVal(x, env, c)
		if err != nil {
			return Value{}, err
		}
	}
	out := v
	out = out.withItems(xs)
	return out, nil
}

func expandLet(v Value, env *env, c *ctx) (Value, error) {
	if len(v.Items()) < 2 {
		return expandElems(v, env, c)
	}
	binds, err := expandVal(v.Items()[1], env, c)
	if err != nil {
		return Value{}, err
	}
	body, err := expandForms(v.Items()[2:], env, c)
	if err != nil {
		return Value{}, err
	}
	out := v
	out = out.withItems(append([]Value{v.Items()[0], binds}, body...))
	return out, nil
}

func expandFn(v Value, env *env, c *ctx) (Value, error) {
	parsed, err := parseFn(v.Items()[1:])
	if err != nil || parsed.kind != "long" {
		return expandElems(v, env, c)
	}
	rebuilt := []Value{v.Items()[0]}
	for i, cl := range parsed.clauses {
		if cl.ParamsForm != nil {
			rebuilt = append(rebuilt, *cl.ParamsForm)
		}
		body, err := expandForms(cl.Body, env, c)
		if err != nil {
			return Value{}, err
		}
		rebuilt = append(rebuilt, body...)
		if i < len(parsed.clauses)-1 {
			rebuilt = append(rebuilt, Symbol("fn"))
		}
	}
	out := v
	out = out.withItems(rebuilt)
	return out, nil
}

func expandIf(v Value, env *env, c *ctx) (Value, error) {
	clauses, err := parseIfArgs(v.Items()[1:])
	if err != nil {
		return expandElems(v, env, c)
	}
	for i := range clauses {
		if clauses[i].Test != nil {
			t, err := expandVal(*clauses[i].Test, env, c)
			if err != nil {
				return Value{}, err
			}
			clauses[i].Test = &t
		}
		body, err := expandForms(clauses[i].Body, env, c)
		if err != nil {
			return Value{}, err
		}
		clauses[i].Body = body
	}
	xs := []Value{v.Items()[0]}
	for i, cl := range clauses {
		if i > 0 {
			xs = append(xs, Symbol("else"))
			if cl.Test != nil {
				xs = append(xs, Symbol("if"))
			}
		}
		if cl.Test != nil {
			if cl.Not {
				xs = append(xs, Symbol("not"))
			}
			xs = append(xs, *cl.Test)
		}
		xs = append(xs, cl.Body...)
	}
	out := v
	out = out.withItems(xs)
	return out, nil
}

func expandAfter(v Value, env *env, c *ctx) (Value, error) {
	if len(v.Items()) < 2 {
		return expandElems(v, env, c)
	}
	dur, err := expandVal(v.Items()[1], env, c)
	if err != nil {
		return Value{}, err
	}
	body, err := expandForms(v.Items()[2:], env, c)
	if err != nil {
		return Value{}, err
	}
	out := v
	out = out.withItems(append([]Value{v.Items()[0], dur}, body...))
	return out, nil
}
