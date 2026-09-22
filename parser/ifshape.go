package parser

import (
	"deedles.dev/writ/syntax"
)

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
		return syntax.Form{}, false, 0, syntax.Errorf("%s needs a test", ctx)
	}
	if isNotSym(args[i]) {
		i++
		if i >= len(args) {
			return syntax.Form{}, false, 0, syntax.Errorf("%s not needs a test", ctx)
		}
		return args[i], true, i + 1, nil
	}
	return args[i], false, i + 1, nil
}

func parseIfArgs(args []syntax.Form) ([]IfClause, error) {
	if len(args) == 0 {
		return nil, syntax.ErrorMsg("(if test ...)")
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
