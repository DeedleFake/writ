package parser

import (
	"encoding/json"
	"strings"

	"deedles.dev/writ/runtime"
)

const maxInline = 72

type printer struct {
	src string
}

var bodySpecials = map[string]struct{}{
	"on": {}, "def": {}, "fn": {}, "let": {}, "if": {},
	"after": {}, "pipe": {}, "let!": {}, "defm": {}, "import": {},
	".": {},
}

// Format pretty-prints src. A trailing newline is added when src is not empty.
func Format(src string) (string, error) {
	forms, err := Parse(src)
	if err != nil {
		return "", err
	}
	if len(forms) == 0 {
		return "", nil
	}
	p := &printer{src: src}
	text := p.formatVal(forms[0], 0)
	for i := 1; i < len(forms); i++ {
		prev := forms[i-1]
		form := forms[i]
		sep := "\n"
		if prev.Kind() == runtime.KindComment {
			sep = "\n"
		} else if form.Blank() {
			sep = "\n\n"
		}
		text += sep + p.formatVal(form, 0)
	}
	return text + "\n", nil
}

func (p *printer) originalAtom(v runtime.Value) string {
	if p.src == "" {
		return ""
	}
	sp, ok := v.Span()
	if !ok {
		return ""
	}
	if sp.Start < 0 || sp.End > len(p.src) || sp.End <= sp.Start {
		return ""
	}
	return p.src[sp.Start:sp.End]
}

func fnIsMulti(v runtime.Value) bool {
	xs := v.Items()
	if v.Kind() != runtime.KindList || len(xs) == 0 || xs[0].Kind() != runtime.KindSymbol || xs[0].Name() != "fn" {
		return false
	}
	kind, clauses, err := runtime.ParseFn(xs[1:])
	return err == nil && kind == "long" && len(clauses) > 1
}

func hasBreakComment(v runtime.Value) bool {
	switch v.Kind() {
	case runtime.KindComment:
		return true
	case runtime.KindQuote, runtime.KindUnquote, runtime.KindSplice:
		return v.TrailingComment() != "" || hasBreakComment(v.Inner())
	case runtime.KindMap:
		for _, pair := range v.Pairs() {
			if pair.Value.TrailingComment() != "" || hasBreakComment(pair.Value) {
				return true
			}
		}
		return false
	case runtime.KindList:
		xs := v.Items()
		for i, x := range xs {
			if x.Kind() == runtime.KindComment {
				return true
			}
			if x.TrailingComment() != "" && i < len(xs)-1 {
				return true
			}
			if hasBreakComment(x) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func suffixCmt(v runtime.Value, text string) string {
	if v.TrailingComment() != "" {
		return text + " " + v.TrailingComment()
	}
	return text
}

func prefixLines(prefix, text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return prefix + text
	}
	pad := strings.Repeat(" ", len(prefix))
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(pad)
		b.WriteString(l)
	}
	return b.String()
}

func pushPrefixed(lines *[]string, prefix, text string) {
	*lines = append(*lines, strings.Split(prefixLines(prefix, text), "\n")...)
}

func (p *printer) formatVal(v runtime.Value, indent int) string {
	if v.Kind() == runtime.KindComment {
		return v.CommentText()
	}
	if v.Kind() == runtime.KindQuote || v.Kind() == runtime.KindUnquote || v.Kind() == runtime.KindSplice {
		mark := "'"
		if v.Kind() == runtime.KindUnquote {
			mark = ","
		} else if v.Kind() == runtime.KindSplice {
			mark = "@"
		}
		return suffixCmt(v, prefixLines(mark, p.formatVal(v.Inner(), indent)))
	}
	if v.Kind() == runtime.KindMap {
		return p.formatMap(v, indent)
	}
	if v.Kind() != runtime.KindList {
		return p.formatAtom(v)
	}
	if s, ok := p.formatDotted(v); ok {
		return s
	}
	xs := v.Items()
	empty := "()"
	if v.IsVec() {
		empty = "[]"
	}
	if len(xs) == 0 {
		return suffixCmt(v, empty)
	}
	if v.IsVec() {
		if hasBreakComment(v) || v.Broke() {
			return suffixCmt(v, p.formatVecBlock(v, indent))
		}
		inline := p.formatInline(v)
		col := indent * 2
		if len(inline)+col <= maxInline {
			return inline
		}
		return suffixCmt(v, p.formatVecBlock(v, indent))
	}
	headName := ""
	if xs[0].Kind() == runtime.KindSymbol {
		headName = xs[0].Name()
	}
	_, special := bodySpecials[headName]
	if hasBreakComment(v) || (special && v.Broke()) || fnIsMulti(v) {
		return suffixCmt(v, p.formatBlock(v, indent))
	}
	inline := p.formatInline(v)
	col := indent * 2
	if len(inline)+col <= maxInline {
		return inline
	}
	return suffixCmt(v, p.formatBlock(v, indent))
}

func closeOn(lines []string, close string) string {
	if len(lines) == 0 {
		return close
	}
	last := lines[len(lines)-1]
	cmt := strings.Index(last, " ;")
	if cmt >= 0 {
		lines[len(lines)-1] = last[:cmt] + close + last[cmt:]
	} else {
		lines[len(lines)-1] = last + close
	}
	return strings.Join(lines, "\n")
}

func (p *printer) formatVecBlock(v runtime.Value, indent int) string {
	xs := v.Items()
	if len(xs) == 0 {
		return "[]"
	}
	var lines []string
	pushPrefixed(&lines, "[", p.formatVal(xs[0], indent+1))
	for _, item := range xs[1:] {
		pushPrefixed(&lines, "  ", p.formatVal(item, indent+1))
	}
	return closeOn(lines, "]")
}

func (p *printer) formatMap(v runtime.Value, indent int) string {
	pairs := v.Pairs()
	if len(pairs) == 0 {
		return suffixCmt(v, "[:]")
	}
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = runtime.FormatSymbol(pair.Key.Name()) + ": " + p.formatInline(pair.Value)
	}
	inline := "[" + strings.Join(parts, " ") + "]"
	col := indent * 2
	if !hasBreakComment(v) && !v.Broke() && len(inline)+col <= maxInline {
		return suffixCmt(v, inline)
	}
	pairText := func(k runtime.Value, val runtime.Value) string {
		return prefixLines(runtime.FormatSymbol(k.Name())+": ", p.formatVal(val, indent+1))
	}
	var lines []string
	pushPrefixed(&lines, "[", pairText(pairs[0].Key, pairs[0].Value))
	for _, pair := range pairs[1:] {
		pushPrefixed(&lines, " ", pairText(pair.Key, pair.Value))
	}
	return suffixCmt(v, closeOn(lines, "]"))
}

func (p *printer) formatBlock(v runtime.Value, indent int) string {
	xs := v.Items()
	body := "  "
	head := xs[0]
	headName := ""
	if head.Kind() == runtime.KindSymbol {
		headName = head.Name()
	} else {
		headName = p.formatVal(head, indent)
	}

	if headName == "on" {
		ev := ""
		if len(xs) > 1 {
			ev = p.formatVal(xs[1], indent)
		}
		params := "()"
		if len(xs) > 2 {
			params = p.formatInline(xs[2])
		}
		rest := xs[3:]
		if len(rest) == 0 {
			return "(on " + ev + " " + params + ")"
		}
		lines := []string{"(on " + ev + " " + params}
		for _, item := range rest {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "after" {
		i := 1
		header := "(" + headName
		for i < len(xs) && xs[i].Kind() != runtime.KindList {
			header += " " + p.formatAtom(xs[i])
			i++
		}
		if i >= len(xs) {
			return header + ")"
		}
		lines := []string{header}
		for ; i < len(xs); i++ {
			pushPrefixed(&lines, body, p.formatVal(xs[i], indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "if" {
		clauses, err := runtime.ParseIfArgs(xs[1:])
		if err != nil {
			cond := "nil"
			if len(xs) > 1 {
				cond = p.formatVal(xs[1], indent+1)
			}
			var lines []string
			pushPrefixed(&lines, "(if ", cond)
			for _, item := range xs[2:] {
				pushPrefixed(&lines, body, p.formatVal(item, indent+1))
			}
			return closeOn(lines, ")")
		}
		var lines []string
		for i, c := range clauses {
			if i == 0 {
				t := "nil"
				if c.Test != nil {
					t = p.formatVal(*c.Test, indent+1)
				}
				not := ""
				if c.Not {
					not = "not "
				}
				pushPrefixed(&lines, "(if "+not, t)
			} else if c.Test != nil {
				not := ""
				if c.Not {
					not = "not "
				}
				pushPrefixed(&lines, "else if "+not, p.formatVal(*c.Test, indent+1))
			} else {
				lines = append(lines, "else")
			}
			for _, item := range c.Body {
				pushPrefixed(&lines, body, p.formatVal(item, indent+1))
			}
		}
		if len(lines) == 0 {
			return "(if)"
		}
		return closeOn(lines, ")")
	}

	if headName == "def" || headName == "defm" {
		headStr := "()"
		if len(xs) > 1 {
			headStr = p.formatInline(xs[1])
		}
		rest := xs[2:]
		if len(rest) == 0 {
			return "(" + headName + " " + headStr + ")"
		}
		lines := []string{"(" + headName + " " + headStr}
		for _, item := range rest {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "fn" {
		rest := xs[1:]
		kind, clauses, err := runtime.ParseFn(rest)
		if err == nil && kind == "long" {
			var lines []string
			for i, c := range clauses {
				sig := "()"
				if c.ParamsForm != nil {
					sig = p.formatInline(*c.ParamsForm)
				}
				if i == 0 {
					lines = append(lines, "(fn "+sig)
				} else {
					lines = append(lines, " fn "+sig)
				}
				for _, item := range c.Body {
					pushPrefixed(&lines, body, p.formatVal(item, indent+1))
				}
			}
			if len(lines) == 0 {
				return "(fn)"
			}
			return closeOn(lines, ")")
		}
		sig := ""
		if len(xs) > 1 {
			sig = " " + p.formatInline(xs[1])
		}
		after := xs[2:]
		if len(after) == 0 {
			return "(" + headName + sig + ")"
		}
		lines := []string{"(" + headName + sig}
		for _, item := range after {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "let" || headName == "let!" {
		bindStr := "[:]"
		if len(xs) > 1 {
			bindStr = p.formatVal(xs[1], indent+1)
		}
		var lines []string
		pushPrefixed(&lines, "("+headName+" ", bindStr)
		for _, item := range xs[2:] {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "pipe" {
		rest := xs[1:]
		if len(rest) == 0 {
			return "(pipe)"
		}
		first := p.formatVal(rest[0], indent+1)
		if len(rest) == 1 {
			return prefixLines("(pipe ", first) + ")"
		}
		var lines []string
		pushPrefixed(&lines, "(pipe ", first)
		for _, item := range rest[1:] {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	args := xs[1:]
	if len(args) == 0 {
		return "(" + headName + ")"
	}
	var chunks [][]runtime.Value
	for i := 0; i < len(args); {
		a := args[i]
		if a.IsKey() && i+1 < len(args) {
			chunks = append(chunks, []runtime.Value{a, args[i+1]})
			i += 2
		} else {
			chunks = append(chunks, []runtime.Value{a})
			i++
		}
	}
	fmtChunk := func(c []runtime.Value) string {
		if len(c) == 2 && c[0].IsKey() {
			return prefixLines(c[0].Name()+" ", p.formatVal(c[1], indent+1))
		}
		if len(c) == 1 {
			return p.formatVal(c[0], indent+1)
		}
		parts := make([]string, len(c))
		for i, x := range c {
			parts[i] = p.formatVal(x, indent+1)
		}
		return strings.Join(parts, " ")
	}
	argPad := strings.Repeat(" ", len(headName)+2)
	var lines []string
	pushPrefixed(&lines, "("+headName+" ", fmtChunk(chunks[0]))
	for _, c := range chunks[1:] {
		pushPrefixed(&lines, argPad, fmtChunk(c))
	}
	return closeOn(lines, ")")
}

func (p *printer) formatDotted(v runtime.Value) (string, bool) {
	if v.Kind() != runtime.KindList || v.IsVec() {
		return "", false
	}
	if hasBreakComment(v) {
		return "", false
	}
	xs := runtime.FilterComments(v.Items())
	if len(xs) < 3 || !runtime.IsName(xs[0], ".") {
		return "", false
	}
	for _, k := range xs[2:] {
		if k.Kind() != runtime.KindSymbol || k.IsKey() || runtime.FormatSymbol(k.Name()) != k.Name() {
			return "", false
		}
	}
	var left string
	if xs[1].Kind() == runtime.KindSymbol && !xs[1].IsKey() && runtime.FormatSymbol(xs[1].Name()) == xs[1].Name() {
		left = xs[1].Name()
	} else if s, ok := p.formatDotted(xs[1]); ok {
		left = s
	} else {
		return "", false
	}
	for _, k := range xs[2:] {
		left += "." + k.Name()
	}
	return suffixCmt(v, left), true
}

func (p *printer) formatInline(v runtime.Value) string {
	if s, ok := p.formatDotted(v); ok {
		return s
	}
	if v.Kind() == runtime.KindQuote || v.Kind() == runtime.KindUnquote || v.Kind() == runtime.KindSplice {
		mark := "'"
		if v.Kind() == runtime.KindUnquote {
			mark = ","
		} else if v.Kind() == runtime.KindSplice {
			mark = "@"
		}
		return mark + p.formatInline(v.Inner())
	}
	if v.Kind() != runtime.KindList {
		return p.formatAtom(v)
	}
	xs := v.Items()
	open, close := "(", ")"
	if v.IsVec() {
		open, close = "[", "]"
	}
	if len(xs) == 0 {
		return suffixCmt(v, open+close)
	}
	last := xs[len(xs)-1]
	parts := make([]string, len(xs))
	for i, x := range xs {
		if i == len(xs)-1 {
			parts[i] = p.formatCore(x)
		} else {
			parts[i] = p.formatInline(x)
		}
	}
	text := open + strings.Join(parts, " ") + close
	if last.TrailingComment() != "" {
		text += " " + last.TrailingComment()
	}
	return suffixCmt(v, text)
}

func (p *printer) formatCore(v runtime.Value) string {
	if s, ok := p.formatDotted(v); ok {
		return s
	}
	if v.Kind() == runtime.KindComment {
		return v.CommentText()
	}
	if v.Kind() == runtime.KindQuote || v.Kind() == runtime.KindUnquote || v.Kind() == runtime.KindSplice {
		mark := "'"
		if v.Kind() == runtime.KindUnquote {
			mark = ","
		} else if v.Kind() == runtime.KindSplice {
			mark = "@"
		}
		return mark + p.formatCore(v.Inner())
	}
	if v.Kind() == runtime.KindList {
		return p.formatInline(v)
	}
	if v.Kind() == runtime.KindMap {
		return p.formatMap(v, 0)
	}
	return p.formatAtomCore(v)
}

func (p *printer) formatAtomCore(v runtime.Value) string {
	switch v.Kind() {
	case runtime.KindInt:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		return v.String()
	case runtime.KindFloat:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		return v.String()
	case runtime.KindString:
		b, _ := json.Marshal(v.Text())
		return string(b)
	case runtime.KindSymbol:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		if v.IsTrue() || v.IsFalse() || v.IsNil() {
			return v.Name()
		}
		return runtime.FormatSymbol(v.Name())
	case runtime.KindFn:
		return "#<fn>"
	case runtime.KindMacro:
		return "#<macro>"
	case runtime.KindNative:
		return runtime.Print(v)
	case runtime.KindComment:
		return v.CommentText()
	case runtime.KindList:
		return p.formatInline(v)
	case runtime.KindMap:
		return p.formatMap(v, 0)
	case runtime.KindQuote:
		return "'" + p.formatAtomCore(v.Inner())
	case runtime.KindUnquote:
		return "," + p.formatAtomCore(v.Inner())
	case runtime.KindSplice:
		return "@" + p.formatAtomCore(v.Inner())
	default:
		return runtime.Print(v)
	}
}

func (p *printer) formatAtom(v runtime.Value) string {
	return suffixCmt(v, p.formatAtomCore(v))
}
