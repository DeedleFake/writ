package runtime

import (
	"maps"

	"deedles.dev/writ/syntax"
)

func collectImported(v syntax.Form, into map[*syntax.Form]struct{}, nodes *[]syntax.Form) {
	for _, n := range *nodes {
		if valuePtrEq(n, v) {
			return
		}
	}
	*nodes = append(*nodes, v)
	switch v.Kind() {
	case syntax.KindList:
		for _, x := range v.Items() {
			collectImported(x, into, nodes)
		}
	case syntax.KindMap:
		for _, pair := range v.Pairs() {
			collectImported(pair.Value, into, nodes)
		}
	case syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice:
		collectImported(v.Inner(), into, nodes)
	}
}

func valuePtrEq(a, b syntax.Form) bool {
	if a.Kind() != b.Kind() || a.IsVec() != b.IsVec() {
		return false
	}
	sa, oka := a.Span()
	sb, okb := b.Span()
	if oka != okb || sa != sb {
		return false
	}
	return a.Equal(b)
}

func spanStartEnd(f syntax.Form) (int, int) {
	sp, _ := f.Span()
	return sp.Start, sp.End
}

func importedHas(nodes []syntax.Form, v syntax.Form) bool {
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

func hygienic(v syntax.Form, imported []syntax.Form, subst map[string]string, h *hygState) syntax.Form {
	if importedHas(imported, v) {
		return v
	}
	switch v.Kind() {
	case syntax.KindSymbol:
		if n, ok := subst[v.Name()]; ok {
			return v.WithName(n)
		}
		return v
	case syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice:
		return v.SetInner(hygienic(v.Inner(), imported, subst, h))
	case syntax.KindMap:
		pairs := v.Pairs()
		outp := make([]syntax.MapPair, len(pairs))
		for i, pair := range pairs {
			outp[i] = syntax.MapPair{Key: pair.Key, Value: hygienic(pair.Value, imported, subst, h)}
		}
		return v.WithMap(outp)
	case syntax.KindList:
		name := ""
		if len(v.Items()) > 0 && v.Items()[0].Kind() == syntax.KindSymbol {
			name = v.Items()[0].Name()
		}
		if (name == "let" || name == "let!") && len(v.Items()) >= 1 {
			exported := name == "let!"
			var binds syntax.Form
			if len(v.Items()) > 1 {
				binds = v.Items()[1]
			}
			next := subst
			newBinds := binds
			hasBinds := len(v.Items()) > 1
			if hasBinds && !importedHas(imported, binds) && binds.Kind() == syntax.KindMap && !exported {
				next = copySubst(subst)
				pairs := binds.Pairs()
				outp := make([]syntax.MapPair, len(pairs))
				for i, pair := range pairs {
					old := pair.Key.Name()
					nk := h.fresh(old)
					next[old] = nk
					outp[i] = syntax.MapPair{Key: syntax.Symbol(nk), Value: hygienic(pair.Value, imported, subst, h)}
				}
				newBinds = binds.WithMap(outp)
			} else if hasBinds {
				newBinds = hygienic(binds, imported, subst, h)
			}
			body := make([]syntax.Form, 0, len(v.Items()))
			head := v.Items()[0]
			if head.Kind() == syntax.KindSymbol {
				head = head.WithName("let")
			} else {
				head = syntax.Symbol("let")
			}
			body = append(body, head)
			if hasBinds {
				body = append(body, newBinds)
			}
			for _, x := range v.Items()[2:] {
				body = append(body, hygienic(x, imported, next, h))
			}
			out := v
			out = out.WithItems(body)
			return out
		}
		if name == "fn" || name == "def" || name == "defm" || name == "on" {
			return hygienicFnLike(v, imported, subst, name, h)
		}
		if name == "." {
			return hygienicDot(v, imported, subst, h)
		}
		xs := make([]syntax.Form, len(v.Items()))
		for i, x := range v.Items() {
			xs[i] = hygienic(x, imported, subst, h)
		}
		out := v
		out = out.WithItems(xs)
		return out
	default:
		return v
	}
}

func copySubst(s map[string]string) map[string]string {
	out := make(map[string]string, len(s)+4)
	maps.Copy(out, s)
	return out
}

func hygienicDot(v syntax.Form, imported []syntax.Form, subst map[string]string, h *hygState) syntax.Form {
	xs := v.Items()
	out := make([]syntax.Form, len(xs))
	head, left := false, false
	for i, x := range xs {
		if x.Kind() == syntax.KindComment {
			out[i] = x
			continue
		}
		if !head {
			out[i] = x
			head = true
			continue
		}
		if !left {
			out[i] = hygienic(x, imported, subst, h)
			left = true
			continue
		}
		out[i] = x
	}
	v = v.WithItems(out)
	return v
}

func renameParamForm(form syntax.Form, imported []syntax.Form, subst map[string]string, h *hygState) (syntax.Form, map[string]string) {
	if importedHas(imported, form) || form.Kind() != syntax.KindList {
		return hygienic(form, imported, subst, h), subst
	}
	next := copySubst(subst)
	out := make([]syntax.Form, 0, len(form.Items()))
	for _, p := range form.Items() {
		if p.Kind() == syntax.KindSplice && p.Inner().Kind() == syntax.KindSymbol {
			old := p.Inner().Name()
			nk := h.fresh(old)
			next[old] = nk
			out = append(out, p.SetInner(p.Inner().WithName(nk)))
			continue
		}
		if p.Kind() == syntax.KindSymbol && !p.IsKey() {
			if syntax.ReservedLit(p) {
				out = append(out, p)
				continue
			}
			nk := h.fresh(p.Name())
			next[p.Name()] = nk
			out = append(out, p.WithName(nk))
			continue
		}
		if p.IsKey() {
			raw := p.KeyName()
			nk := h.fresh(raw)
			next[raw] = nk
			out = append(out, p.WithName(nk+":"))
			continue
		}
		out = append(out, hygienic(p, imported, subst, h))
	}
	form = form.WithItems(out)
	return form, next
}

func hygienicFnLike(v syntax.Form, imported []syntax.Form, subst map[string]string, name string, h *hygState) syntax.Form {
	xs := v.Items()
	if name == "fn" {
		parsed, err := parseFn(xs[1:])
		if err != nil {
			out := make([]syntax.Form, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v = v.WithItems(out)
			return v
		}
		rebuilt := []syntax.Form{xs[0]}
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
			rebuilt = append(rebuilt, syntax.Symbol("fn"))
		}
		if n := len(rebuilt); n > 0 && syntax.IsName(rebuilt[n-1], "fn") {
			rebuilt = rebuilt[:n-1]
		}
		v = v.WithItems(rebuilt)
		return v
	}
	var nm syntax.Form
	if len(xs) > 1 {
		nm = xs[1]
	}
	head := xs[0]
	if len(xs) > 0 {
		head = hygienic(xs[0], imported, subst, h)
	}
	if name == "def" || name == "defm" {
		if nm.Kind() != syntax.KindList || len(nm.Items()) == 0 {
			out := make([]syntax.Form, len(xs))
			for i, x := range xs {
				out[i] = hygienic(x, imported, subst, h)
			}
			v = v.WithItems(out)
			return v
		}
		nameForm := nm.Items()[0]
		paramsOnly := syntax.CallList(nm.Items()[1:]...)
		paramsOnly = paramsOnly.WithItems(nm.Items()[1:])
		if nm.HasSpan() {
			paramsOnly = paramsOnly.WithSpan(spanStartEnd(nm))
		}
		form, next := renameParamForm(paramsOnly, imported, subst, h)
		renamed := form.Items()
		newHead := nm
		newHead = newHead.WithItems(append([]syntax.Form{nameForm}, renamed...))
		body := make([]syntax.Form, 0, len(xs)-1)
		body = append(body, head, newHead)
		for _, b := range xs[2:] {
			body = append(body, hygienic(b, imported, next, h))
		}
		v = v.WithItems(body)
		return v
	}
	var params syntax.Form
	hasParams := false
	if name == "on" && len(xs) > 2 {
		params = xs[2]
		hasParams = true
	}
	if !hasParams {
		out := make([]syntax.Form, len(xs))
		for i, x := range xs {
			out[i] = hygienic(x, imported, subst, h)
		}
		v = v.WithItems(out)
		return v
	}
	form, next := renameParamForm(params, imported, subst, h)
	body := []syntax.Form{head, nm, form}
	for _, b := range xs[3:] {
		body = append(body, hygienic(b, imported, next, h))
	}
	v = v.WithItems(body)
	return v
}

func applyMacro(name string, f *fnVal, raw []syntax.Form, c *ctx, call syntax.Form) ([]syntax.Form, error) {
	if f != nil && f.macro != nil {
		callNative := func() (syntax.Form, error) { return f.macro(raw) }
		var result syntax.Form
		var err error
		if c != nil && c.rt != nil {
			result, err = c.rt.hostCallForm(callNative)
		} else {
			result, err = callNative()
		}
		if err != nil {
			return nil, err
		}
		var frags []syntax.Form
		if result.Kind() == syntax.KindList {
			frags = append([]syntax.Form{}, result.Items()...)
		} else {
			frags = []syntax.Form{result}
		}
		var imported []syntax.Form
		for _, a := range raw {
			collectImported(a, nil, &imported)
		}
		h := &hygState{}
		out := make([]syntax.Form, len(frags))
		for i, frag := range frags {
			out[i] = hygienic(frag, imported, map[string]string{}, h)
			if !out[i].HasSpan() && call.HasSpan() {
				out[i] = out[i].WithSpan(spanStartEnd(call))
			}
		}
		return out, nil
	}
	var clauses []Clause
	var defEnv *env
	if f != nil {
		clauses = f.clauses
		defEnv = f.env
	}
	parsed, err := parseCallRaw(raw)
	if err != nil {
		e := asError(err)
		if e.Start == 0 && e.End == 0 && call.HasSpan() {
			e.Start, e.End = spanStartEnd(call)
		}
		return nil, e
	}
	pos := make([]Value, len(parsed.pos))
	for i, f := range parsed.pos {
		pos[i] = ValueFromLiteralForm(f)
	}
	parts := callParts{pos: pos, keys: map[string]Value{}}
	for _, k := range parsed.keys {
		parts.keys[k.name] = ValueFromLiteralForm(k.raw)
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
		return nil, errForm(call, "this macro does not take keyword arguments")
	}
	if allKey && len(parts.pos) > 0 {
		return nil, errForm(call, "this macro needs key: arguments")
	}
	if defEnv == nil {
		defEnv = c.macroEnv
	}
	for _, clause := range clauses {
		child := makeEnv(defEnv)
		if !tryBind(clause.Params, parts, child) {
			continue
		}
		frags, err := evalSpread(clause.Body, child, c, false)
		if err != nil {
			return nil, err
		}
		var imported []syntax.Form
		for _, a := range raw {
			collectImported(a, nil, &imported)
		}
		h := &hygState{}
		out := make([]syntax.Form, len(frags))
		for i, f := range frags {
			out[i] = hygienic(FormFromValue(f), imported, map[string]string{}, h)
			if !out[i].HasSpan() && call.HasSpan() {
				out[i] = out[i].WithSpan(spanStartEnd(call))
			}
		}
		return out, nil
	}
	return nil, errFormf(call, "no matching clause for %s", name)
}

func asMacroCall(v syntax.Form, env *env, c *ctx) (name string, f *fnVal, args []syntax.Form, ok bool) {
	if v.Kind() != syntax.KindList || v.IsVec() {
		return "", nil, nil, false
	}
	xs := syntax.FilterComments(v.Items())
	if len(xs) == 0 {
		return "", nil, nil, false
	}
	head := xs[0]
	if head.Kind() == syntax.KindSymbol {
		name = head.Name()
		clauses, found := c.macros[name]
		if !found {
			return "", nil, nil, false
		}
		return name, &fnVal{clauses: clauses, env: c.macroEnv, name: name}, xs[1:], true
	}
	name, f, ok = resolveDotMacro(head, env)
	if !ok || f == nil {
		return "", nil, nil, false
	}
	return name, f, xs[1:], true
}

func isDotCall(v syntax.Form) bool {
	if v.Kind() != syntax.KindList || v.IsVec() {
		return false
	}
	xs := syntax.FilterComments(v.Items())
	return len(xs) >= 3 && syntax.IsName(xs[0], ".")
}

func resolveDotMacro(head syntax.Form, env *env) (string, *fnVal, bool) {
	if env == nil || !isDotCall(head) {
		return "", nil, false
	}
	xs := syntax.FilterComments(head.Items())
	left := xs[1]
	if left.Kind() != syntax.KindSymbol || left.IsKey() {
		return "", nil, false
	}
	cur, ok := env.get(left.Name())
	if !ok {
		return "", nil, false
	}
	for _, k := range xs[2:] {
		if k.Kind() != syntax.KindSymbol || k.IsKey() {
			return "", nil, false
		}
		if cur.k != KindMap {
			return "", nil, false
		}
		cur, ok = cur.mapData().get(k.Name())
		if !ok {
			return "", nil, false
		}
	}
	if cur.k != KindMacro {
		return "", nil, false
	}
	f := cur.fnData()
	if f == nil {
		return "", nil, false
	}
	name := f.name
	if name == "" {
		name = "macro"
	}
	return name, f, true
}

func packExpr(forms []syntax.Form, call syntax.Form) syntax.Form {
	switch len(forms) {
	case 0:
		out := syntax.Nil
		if call.HasSpan() {
			out = out.WithSpan(spanStartEnd(call))
		}
		return out
	case 1:
		return forms[0]
	default:
		xs := make([]syntax.Form, 0, 2+len(forms))
		xs = append(xs, syntax.Symbol("let"), syntax.EmptyMap())
		xs = append(xs, forms...)
		out := syntax.CallList(xs...)
		if call.HasSpan() {
			out = out.WithSpan(spanStartEnd(call))
		}
		return out
	}
}

func expandVal(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	if err := c.push(); err != nil {
		return syntax.Form{}, err
	}
	defer c.pop()
	xs, err := expandIn(v, env, c)
	if err != nil {
		return syntax.Form{}, err
	}
	return packExpr(xs, v), nil
}

func expandForms(forms []syntax.Form, env *env, c *ctx) ([]syntax.Form, error) {
	var out []syntax.Form
	for _, form := range forms {
		if form.Kind() == syntax.KindComment {
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

func expandIn(v syntax.Form, env *env, c *ctx) ([]syntax.Form, error) {
	if name, f, args, ok := asMacroCall(v, env, c); ok {
		frags, err := applyMacro(name, f, args, c, v)
		if err != nil {
			return nil, err
		}
		return expandForms(frags, env, c)
	}
	ex, err := expandTree(v, env, c)
	if err != nil {
		return nil, err
	}
	return []syntax.Form{ex}, nil
}

func expandTree(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	switch v.Kind() {
	case syntax.KindComment, syntax.KindInt, syntax.KindFloat, syntax.KindString, syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice, syntax.KindSymbol:
		return v, nil
	case syntax.KindMap:
		pairs := v.Pairs()
		outp := make([]syntax.MapPair, len(pairs))
		for i, pair := range pairs {
			val, err := expandVal(pair.Value, env, c)
			if err != nil {
				return syntax.Form{}, err
			}
			outp[i] = syntax.MapPair{Key: pair.Key, Value: val}
		}
		return v.WithMap(outp), nil
	case syntax.KindList:
		if v.IsVec() {
			return expandElems(v, env, c)
		}
		xs := syntax.FilterComments(v.Items())
		if len(xs) == 0 {
			return v, nil
		}
		if xs[0].Kind() == syntax.KindSymbol {
			switch xs[0].Name() {
			case "let", "let!":
				return expandLet(v, env, c)
			case "fn":
				return expandFn(v, env, c)
			case "if":
				return expandIf(v, env, c)
			case "after":
				return expandAfter(v, env, c)
			case ".":
				return expandDot(v, env, c)
			}
		}
		return expandElems(v, env, c)
	default:
		return v, nil
	}
}

func expandElems(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	xs := make([]syntax.Form, len(v.Items()))
	for i, x := range v.Items() {
		var err error
		xs[i], err = expandVal(x, env, c)
		if err != nil {
			return syntax.Form{}, err
		}
	}
	out := v
	out = out.WithItems(xs)
	return out, nil
}

func expandLet(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	if len(v.Items()) < 2 {
		return expandElems(v, env, c)
	}
	binds, err := expandVal(v.Items()[1], env, c)
	if err != nil {
		return syntax.Form{}, err
	}
	body, err := expandForms(v.Items()[2:], env, c)
	if err != nil {
		return syntax.Form{}, err
	}
	out := v
	out = out.WithItems(append([]syntax.Form{v.Items()[0], binds}, body...))
	return out, nil
}

func expandFn(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	parsed, err := parseFn(v.Items()[1:])
	if err != nil || parsed.kind != "long" {
		return expandElems(v, env, c)
	}
	rebuilt := []syntax.Form{v.Items()[0]}
	for i, cl := range parsed.clauses {
		if cl.ParamsForm != nil {
			rebuilt = append(rebuilt, *cl.ParamsForm)
		}
		body, err := expandForms(cl.Body, env, c)
		if err != nil {
			return syntax.Form{}, err
		}
		rebuilt = append(rebuilt, body...)
		if i < len(parsed.clauses)-1 {
			rebuilt = append(rebuilt, syntax.Symbol("fn"))
		}
	}
	out := v
	out = out.WithItems(rebuilt)
	return out, nil
}

func expandIf(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	clauses, err := parseIfArgs(v.Items()[1:])
	if err != nil {
		return expandElems(v, env, c)
	}
	for i := range clauses {
		if clauses[i].Test != nil {
			t, err := expandVal(*clauses[i].Test, env, c)
			if err != nil {
				return syntax.Form{}, err
			}
			clauses[i].Test = &t
		}
		body, err := expandForms(clauses[i].Body, env, c)
		if err != nil {
			return syntax.Form{}, err
		}
		clauses[i].Body = body
	}
	xs := []syntax.Form{v.Items()[0]}
	for i, cl := range clauses {
		if i > 0 {
			xs = append(xs, syntax.Symbol("else"))
			if cl.Test != nil {
				xs = append(xs, syntax.Symbol("if"))
			}
		}
		if cl.Test != nil {
			if cl.Not {
				xs = append(xs, syntax.Symbol("not"))
			}
			xs = append(xs, *cl.Test)
		}
		xs = append(xs, cl.Body...)
	}
	out := v
	out = out.WithItems(xs)
	return out, nil
}

func expandDot(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	xs := v.Items()
	out := make([]syntax.Form, len(xs))
	head, left := false, false
	for i, x := range xs {
		if x.Kind() == syntax.KindComment {
			out[i] = x
			continue
		}
		if !head {
			out[i] = x
			head = true
			continue
		}
		if !left {
			ex, err := expandVal(x, env, c)
			if err != nil {
				return syntax.Form{}, err
			}
			out[i] = ex
			left = true
			continue
		}
		out[i] = x
	}
	return v.WithItems(out), nil
}

func expandAfter(v syntax.Form, env *env, c *ctx) (syntax.Form, error) {
	if len(v.Items()) < 2 {
		return expandElems(v, env, c)
	}
	dur, err := expandVal(v.Items()[1], env, c)
	if err != nil {
		return syntax.Form{}, err
	}
	body, err := expandForms(v.Items()[2:], env, c)
	if err != nil {
		return syntax.Form{}, err
	}
	out := v
	out = out.WithItems(append([]syntax.Form{v.Items()[0], dur}, body...))
	return out, nil
}
