package parser

import (
	"slices"
	"strconv"
	"strings"

	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

// FnClauseShape is one (fn ...) clause as forms only (no bound values).
type FnClauseShape struct {
	ParamsForm *syntax.Form
	Body       []syntax.Form
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
				if start, end, ok := formSpan(node); ok {
					return syntax.ErrorAt(start, end, "bad slot "+name)
				}
				return syntax.Errorf("bad slot %s", name)
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

func formSpan(f syntax.Form) (start, end int, ok bool) {
	sp, ok := f.Span()
	if !ok {
		return 0, 0, false
	}
	return sp.Start, sp.End, true
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

func shortFnBody(args []syntax.Form) []syntax.Form {
	xs := syntax.FilterComments(args)
	if len(xs) <= 1 {
		return xs
	}
	return []syntax.Form{syntax.CallList(xs...)}
}

// ParseFn parses (fn ...) arguments into form-shaped clauses (no Value binding).
func ParseFn(args []syntax.Form) (kind string, clauses []FnClauseShape, err error) {
	n, err := maxSlot(args)
	if err != nil {
		return "", nil, err
	}
	if n > 0 {
		if hasNestedFn(args) {
			return "", nil, syntax.ErrorMsg("a short fn cannot contain another fn")
		}
		return "short", []FnClauseShape{{Body: shortFnBody(args)}}, nil
	}
	if len(args) == 0 {
		return "", nil, syntax.ErrorMsg("(fn (args...) body)")
	}
	i := 0
	for i < len(args) {
		paramsForm := args[i]
		if paramsForm.Kind() != syntax.KindList {
			return "", nil, syntax.ErrorMsg("(fn (args...) body)")
		}
		i++
		var body []syntax.Form
		for i < len(args) && !isFnSep(args[i]) {
			body = append(body, args[i])
			i++
		}
		pf := paramsForm
		clauses = append(clauses, FnClauseShape{ParamsForm: &pf, Body: body})
		if i < len(args) && isFnSep(args[i]) {
			i++
			if i >= len(args) {
				return "", nil, syntax.ErrorMsg("fn after fn needs a parameter list")
			}
		}
	}
	return "long", clauses, nil
}
