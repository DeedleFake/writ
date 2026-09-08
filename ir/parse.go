package ir

import (
	"slices"
	"strconv"
	"strings"

	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

func isLit(v syntax.Form) bool {
	return v.Kind() == syntax.KindInt || v.Kind() == syntax.KindFloat || v.Kind() == syntax.KindString || syntax.ReservedLit(v)
}

func asPattern(v syntax.Form) (Pattern, error) {
	if v.Kind() == syntax.KindSymbol {
		if syntax.ReservedLit(v) {
			return Pattern{Lit: v}, nil
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
		return Pattern{Lit: v}, nil
	}
	return Pattern{}, errMsg("parameter must be a name or a literal")
}

// ParseParams parses a parameter list form for ctx ("fn", "def", "defm", "on").
func ParseParams(form syntax.Form, ctx string) (Params, error) {
	return parseParams(form, ctx)
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

// UnionKeys returns the sorted-stable union of keyword names across clauses.
func UnionKeys(clauses []Clause) []string {
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
	return earlier.Lit.Equal(later.Lit)
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

// UnreachableBy reports whether next is covered by an earlier clause in prev.
func UnreachableBy(prev []Clause, next Params) bool {
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
		if UnreachableBy(clauses, params) {
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

func containsStr(xs []string, s string) bool {
	return slices.Contains(xs, s)
}
