package runtime

import (
	"deedles.dev/writ/syntax"
)

func stampForm(f syntax.Form, v Value) syntax.Form {
	if sp, ok := v.Span(); ok {
		f = f.WithSpan(sp.Start, sp.End)
	}
	return f
}

func stampVal(v Value, f syntax.Form) Value {
	if sp, ok := f.Span(); ok {
		v = v.WithSpan(sp.Start, sp.End)
	}
	return v
}

// FormFromValue embeds a runtime value into a source form. KindSyntax
// unwraps to its form. Functions, macros, and native values cannot be
// represented as forms; they become an invalid form.
func FormFromValue(v Value) syntax.Form {
	return stampForm(formFromValue(v), v)
}

func formFromValue(v Value) syntax.Form {
	switch v.k {
	case KindInt:
		if b := v.bigInt(); b != nil {
			return syntax.Int(b)
		}
		return syntax.Int64(v.n)
	case KindFloat:
		return syntax.Float(v.floatVal())
	case KindString:
		return syntax.String(v.s)
	case KindSymbol:
		return syntax.Symbol(v.Name())
	case KindSyntax:
		if f, ok := v.Form(); ok {
			return f
		}
		return syntax.Form{}
	case KindList:
		xs := v.Items()
		out := make([]syntax.Form, len(xs))
		for i, x := range xs {
			out[i] = FormFromValue(x)
		}
		if v.IsVec() {
			return syntax.List(out...)
		}
		return syntax.CallList(out...)
	case KindMap:
		pairs := v.Pairs()
		fp := make([]syntax.MapPair, len(pairs))
		for i, p := range pairs {
			fp[i] = syntax.MapPair{Key: syntax.Symbol(p.Key.Name()), Value: FormFromValue(p.Value)}
		}
		return syntax.MapFrom(fp...)
	default:
		return syntax.Form{}
	}
}

// ValueFromLiteralForm converts int/float/string/symbol/list/map forms
// to values. Quote/unquote/splice become KindSyntax values holding the
// form. Comments become nil.
func ValueFromLiteralForm(f syntax.Form) Value {
	return stampVal(valueFromLiteralForm(f), f)
}

func valueFromLiteralForm(f syntax.Form) Value {
	switch f.Kind() {
	case syntax.KindInt:
		n := f.BigInt()
		if n.IsInt64() {
			return Int64(n.Int64())
		}
		return Int(n)
	case syntax.KindFloat:
		return Float(f.Float64())
	case syntax.KindString:
		return String(f.Text())
	case syntax.KindSymbol:
		return Symbol(f.Name())
	case syntax.KindList:
		items := f.Items()
		out := make([]Value, 0, len(items))
		for _, x := range items {
			if x.Kind() == syntax.KindComment {
				continue
			}
			out = append(out, ValueFromLiteralForm(x))
		}
		if f.IsVec() {
			return List(out...)
		}
		return CallList(out...)
	case syntax.KindMap:
		var pairs []MapPair
		for _, p := range f.Pairs() {
			pairs = append(pairs, MapPair{Key: Symbol(p.Key.Name()), Value: ValueFromLiteralForm(p.Value)})
		}
		return MapFrom(pairs...)
	case syntax.KindQuote, syntax.KindUnquote, syntax.KindSplice:
		return Syntax(f)
	case syntax.KindComment:
		return Nil
	default:
		return Value{}
	}
}

func formFromResidual(v Value) syntax.Form {
	if f, ok := v.Form(); ok {
		return f
	}
	return FormFromValue(v)
}

func formSpan(f syntax.Form) (start, end int, ok bool) {
	sp, ok := f.Span()
	if !ok {
		return 0, 0, false
	}
	return sp.Start, sp.End, true
}

func errForm(v syntax.Form, msg string) *Error {
	if start, end, ok := formSpan(v); ok {
		return errAt(start, end, msg)
	}
	return errMsg(msg)
}

func errFormf(v syntax.Form, format string, args ...any) *Error {
	return errForm(v, errf(format, args...).Message)
}
