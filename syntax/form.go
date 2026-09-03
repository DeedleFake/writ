package syntax

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unique"

	"deedles.dev/writ/scanner"
)

// Kind is the syntactic kind of a [Form].
type Kind int

const (
	KindInvalid Kind = iota
	KindInt
	KindFloat
	KindString
	KindSymbol
	KindList
	KindMap
	KindQuote
	KindUnquote
	KindSplice
	KindComment
)

func (k Kind) String() string {
	switch k {
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindString:
		return "string"
	case KindSymbol:
		return "symbol"
	case KindList:
		return "list"
	case KindMap:
		return "map"
	case KindQuote:
		return "quote"
	case KindUnquote:
		return "unquote"
	case KindSplice:
		return "splice"
	case KindComment:
		return "comment"
	default:
		return "invalid"
	}
}

// Span is a byte range in source text.
type Span struct {
	Start int
	End   int
}

// MapPair is one map entry. Key is a symbol.
type MapPair struct {
	Key   Form
	Value Form
}

// Form is a Writ source form. Quote, unquote, splice, and comment exist
// only here; they are not runtime values.
type Form struct {
	k   Kind
	n   int64 // small int, or float64 bits
	s   string
	h   unique.Handle[string]
	p   any // list, map, quote-inner, *big.Int
	src *srcInfo
}

type srcInfo struct {
	span     Span
	hasSpan  bool
	cmt      string
	blank    bool
	broke    bool
	keySpans map[string]Span
	lex      string
}

type listData struct {
	xs  []Form
	vec bool
}

type mapData struct {
	keys []Form
	vals []Form
	idx  map[string]int
}

// True, False, and Nil are interned symbols. True and False are booleans;
// Nil is not.
var (
	True  = internSym("true")
	False = internSym("false")
	Nil   = internSym("nil")
)

func internSym(name string) Form {
	return Form{k: KindSymbol, h: unique.Make(name)}
}

func (v Form) withName(name string) Form {
	n := internSym(name)
	if v.src != nil {
		n.src = &srcInfo{
			span:    v.src.span,
			hasSpan: v.src.hasSpan,
			cmt:     v.src.cmt,
			blank:   v.src.blank,
			broke:   v.src.broke,
			lex:     v.src.lex,
		}
	}
	return n
}

func (v Form) cloneSrc() *srcInfo {
	if v.src == nil {
		return &srcInfo{}
	}
	cp := *v.src
	return &cp
}

func (v Form) list() *listData {
	ld, _ := v.p.(*listData)
	return ld
}

func (v Form) mapData() *mapData {
	m, _ := v.p.(*mapData)
	return m
}

func (v Form) isWrap() bool {
	switch v.k {
	case KindQuote, KindUnquote, KindSplice:
		return true
	default:
		return false
	}
}

func (v Form) innerPtr() *Form {
	if !v.isWrap() {
		return nil
	}
	in, _ := v.p.(*Form)
	return in
}

func (v Form) bigInt() *big.Int {
	b, _ := v.p.(*big.Int)
	return b
}

func (v Form) floatVal() float64 {
	return math.Float64frombits(uint64(v.n))
}

// WithItems returns a copy of v with list elements xs. The vector/call
// shape is preserved.
func (v Form) WithItems(xs []Form) Form {
	vec := false
	if ld := v.list(); ld != nil {
		vec = ld.vec
	}
	v.p = &listData{xs: xs, vec: vec}
	return v
}

func listVal(xs []Form, vec bool) Form {
	return Form{k: KindList, p: &listData{xs: xs, vec: vec}}
}

// Int64 returns an integer form.
func Int64(n int64) Form {
	return Form{k: KindInt, n: n}
}

// Int returns an integer form. v is copied.
func Int(v *big.Int) Form {
	if v == nil {
		return Int64(0)
	}
	if v.IsInt64() {
		return Int64(v.Int64())
	}
	return Form{k: KindInt, p: new(big.Int).Set(v)}
}

// MustInt parses a decimal integer. It panics on error.
func MustInt(s string) Form {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("writ: invalid integer " + s)
	}
	return Int(n)
}

// Float returns a float64 form.
func Float(f float64) Form {
	return Form{k: KindFloat, n: int64(math.Float64bits(f))}
}

// String returns a string form.
func String(s string) Form {
	return Form{k: KindString, s: s}
}

// Symbol returns an interned symbol.
func Symbol(name string) Form {
	switch name {
	case "true":
		return True
	case "false":
		return False
	case "nil":
		return Nil
	}
	return internSym(name)
}

// Bool returns True or False.
func Bool(b bool) Form {
	if b {
		return True
	}
	return False
}

// List returns a data list (vector).
func List(xs ...Form) Form {
	return listVal(xs, true)
}

// CallList returns a call-shaped list (parentheses).
func CallList(xs ...Form) Form {
	return listVal(xs, false)
}

// Quote wraps v.
func Quote(v Form) Form {
	inner := v
	return Form{k: KindQuote, p: &inner}
}

// Unquote wraps v.
func Unquote(v Form) Form {
	inner := v
	return Form{k: KindUnquote, p: &inner}
}

// Splice wraps v.
func Splice(v Form) Form {
	inner := v
	return Form{k: KindSplice, p: &inner}
}

// Comment returns a comment form. text is the source including ';'.
func Comment(text string) Form {
	return Form{k: KindComment, s: text}
}

// MapFrom builds a map from pairs. Later keys win.
func MapFrom(pairs ...MapPair) Form {
	m := newMap()
	for _, p := range pairs {
		m.put(p.Key, p.Value)
	}
	return Form{k: KindMap, p: m}
}

// EmptyMap is `[:]`.
func EmptyMap() Form {
	return Form{k: KindMap, p: newMap()}
}

func newMap() *mapData {
	return &mapData{idx: make(map[string]int)}
}

func (m *mapData) get(k string) (Form, bool) {
	if m == nil {
		return Nil, false
	}
	i, ok := m.idx[k]
	if !ok {
		return Nil, false
	}
	return m.vals[i], true
}

func (m *mapData) put(k Form, v Form) {
	name := k.Name()
	if i, ok := m.idx[name]; ok {
		m.vals[i] = v
		return
	}
	m.idx[name] = len(m.keys)
	m.keys = append(m.keys, Symbol(name))
	m.vals = append(m.vals, v)
}

func (m *mapData) len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

func (m *mapData) pairs() []MapPair {
	if m == nil {
		return nil
	}
	out := make([]MapPair, len(m.keys))
	for i, k := range m.keys {
		out[i] = MapPair{Key: k, Value: m.vals[i]}
	}
	return out
}

func (v Form) withSpan(start, end int) Form {
	s := v.cloneSrc()
	s.span = Span{Start: start, End: end}
	s.hasSpan = true
	v.src = s
	return v
}

func (v Form) withCmt(cmt string) Form {
	s := v.cloneSrc()
	s.cmt = cmt
	v.src = s
	return v
}

func (v Form) innerVal() Form {
	in := v.innerPtr()
	if in == nil {
		return Nil
	}
	return *in
}

func (v Form) setInner(in Form) Form {
	if !v.isWrap() {
		return v
	}
	cp := in
	v.p = &cp
	return v
}

func (v Form) Kind() Kind { return v.k }

// Span returns the source span, if any.
func (v Form) Span() (Span, bool) {
	if v.src == nil || !v.src.hasSpan {
		return Span{}, false
	}
	return v.src.span, true
}

// HasSpan reports whether v has a source span.
func (v Form) HasSpan() bool { return v.src != nil && v.src.hasSpan }

func (v Form) IsNil() bool { return v.k == KindSymbol && v.h == Nil.h }

func (v Form) IsTrue() bool { return v.k == KindSymbol && v.h == True.h }

// IsFalse reports whether v is false.
func (v Form) IsFalse() bool { return v.k == KindSymbol && v.h == False.h }

func (v Form) IsBool() bool { return v.IsTrue() || v.IsFalse() }

// Truthy is false only for nil and false.
func (v Form) Truthy() bool { return !v.IsNil() && !v.IsFalse() }

// IsInt reports whether v is an integer.
func (v Form) IsInt() bool { return v.k == KindInt }

// IsFloat reports whether v is a float.
func (v Form) IsFloat() bool { return v.k == KindFloat }

// IsNum reports whether v is an int or a float.
func (v Form) IsNum() bool { return v.k == KindInt || v.k == KindFloat }

// BigInt returns a copy of the integer. It is 0 if v is not an int.
func (v Form) BigInt() *big.Int {
	if v.k != KindInt {
		return new(big.Int)
	}
	if b := v.bigInt(); b != nil {
		return new(big.Int).Set(b)
	}
	return big.NewInt(v.n)
}

func (v Form) asBig() *big.Int {
	if b := v.bigInt(); b != nil {
		return b
	}
	return big.NewInt(v.n)
}

// Float64 returns the float, or 0 if v is not a float.
func (v Form) Float64() float64 {
	if v.k != KindFloat {
		return 0
	}
	return v.floatVal()
}

// AsFloat64 converts an int or float to float64.
func (v Form) AsFloat64() (float64, bool) {
	switch v.k {
	case KindFloat:
		return v.floatVal(), true
	case KindInt:
		if b := v.bigInt(); b != nil {
			f, _ := new(big.Float).SetInt(b).Float64()
			return f, true
		}
		return float64(v.n), true
	default:
		return 0, false
	}
}

// Text returns the string contents, or "" if v is not a string.
func (v Form) Text() string {
	if v.k != KindString {
		return ""
	}
	return v.s
}

// Name returns the interned symbol name, or "" if v is not a symbol.
func (v Form) Name() string {
	if v.k != KindSymbol {
		return ""
	}
	return v.h.Value()
}

// Items returns list elements. The slice must not be mutated.
func (v Form) Items() []Form {
	if v.k != KindList {
		return nil
	}
	if ld := v.list(); ld != nil {
		return ld.xs
	}
	return nil
}

// IsVec reports whether v is a [] list rather than a () call.
func (v Form) IsVec() bool {
	if v.k != KindList {
		return false
	}
	ld := v.list()
	return ld != nil && ld.vec
}

// Pairs returns map entries in insertion order.
func (v Form) Pairs() []MapPair {
	if v.k != KindMap {
		return nil
	}
	return v.mapData().pairs()
}

// MapGet looks up a symbol by name.
func (v Form) MapGet(key string) (Form, bool) {
	if v.k != KindMap {
		return Nil, false
	}
	return v.mapData().get(key)
}

func (v Form) commentText() string {
	if v.k != KindComment {
		return ""
	}
	return v.s
}

func (v Form) isKeySym() bool {
	if v.k != KindSymbol {
		return false
	}
	s := v.Name()
	return len(s) > 1 && strings.HasSuffix(s, ":")
}

func (v Form) keyName() string {
	if !v.isKeySym() {
		return ""
	}
	s := v.Name()
	return s[:len(s)-1]
}

func reservedLit(v Form) bool {
	return v.IsTrue() || v.IsFalse() || v.IsNil()
}

func isSymName(v Form, name string) bool {
	return v.k == KindSymbol && v.h == unique.Make(name)
}

func filterComments(xs []Form) []Form {
	out := make([]Form, 0, len(xs))
	for _, x := range xs {
		if x.k != KindComment {
			out = append(out, x)
		}
	}
	return out
}

func intFromString(s string) (Form, bool) {
	if len(s) == 0 {
		return Form{}, false
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return Form{}, false
	}
	return Int(n), true
}

func needsTicks(name string) bool {
	if name == "" {
		return true
	}
	if strings.ContainsAny(name, " \t\n\r();[]',@`") {
		return true
	}
	if strings.Contains(name, ".") && name != "." {
		return true
	}
	if scanner.IsNumLit(name) {
		return true
	}
	return false
}

func formatSymName(name string) string {
	if !needsTicks(name) {
		return name
	}
	var b strings.Builder
	b.WriteByte('`')
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '\\' || c == '`' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('`')
	return b.String()
}

func formatFloat(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func formatInt(v Form) string {
	if b := v.bigInt(); b != nil {
		return b.String()
	}
	return strconv.FormatInt(v.n, 10)
}

// String renders v for display.
func (v Form) String() string {
	return printForm(v)
}

func printForm(v Form) string {
	switch v.k {
	case KindInt:
		return formatInt(v)
	case KindFloat:
		return formatFloat(v.floatVal())
	case KindString:
		return v.s
	case KindSymbol:
		return v.Name()
	case KindList:
		xs := v.Items()
		var b strings.Builder
		b.WriteByte('[')
		for i, x := range xs {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(printForm(x))
		}
		b.WriteByte(']')
		return b.String()
	case KindMap:
		m := v.mapData()
		if m.len() == 0 {
			return "[:]"
		}
		var b strings.Builder
		b.WriteByte('[')
		for i, k := range m.keys {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(formatSymName(k.Name()))
			b.WriteString(": ")
			b.WriteString(printForm(m.vals[i]))
		}
		b.WriteByte(']')
		return b.String()
	case KindComment:
		return v.s
	case KindQuote:
		return "'" + printForm(v.innerVal())
	case KindUnquote:
		return "," + printForm(v.innerVal())
	case KindSplice:
		return "@" + printForm(v.innerVal())
	default:
		return "#<invalid>"
	}
}

// Equal reports structural equality. 1 and 1.0 compare equal.
func (v Form) Equal(o Form) bool {
	if v.k == KindInt && o.k == KindFloat {
		return intFloatEqual(v, o.floatVal())
	}
	if v.k == KindFloat && o.k == KindInt {
		return intFloatEqual(o, v.floatVal())
	}
	if v.k != o.k {
		return false
	}
	switch v.k {
	case KindInt:
		return v.asBig().Cmp(o.asBig()) == 0
	case KindFloat:
		return v.floatVal() == o.floatVal()
	case KindString, KindComment:
		return v.s == o.s
	case KindSymbol:
		return v.h == o.h
	case KindQuote, KindUnquote, KindSplice:
		return v.innerVal().Equal(o.innerVal())
	case KindList:
		xs, ys := v.Items(), o.Items()
		if len(xs) != len(ys) {
			return false
		}
		for i := range xs {
			if !xs[i].Equal(ys[i]) {
				return false
			}
		}
		return true
	case KindMap:
		m, om := v.mapData(), o.mapData()
		if m.len() != om.len() {
			return false
		}
		for i, k := range m.keys {
			ov, ok := om.get(k.Name())
			if !ok || !m.vals[i].Equal(ov) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func intFloatEqual(i Form, f float64) bool {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return false
	}
	bf := new(big.Float).SetInt(i.asBig())
	ff := new(big.Float).SetFloat64(f)
	return bf.Cmp(ff) == 0
}

// WithSpan returns a copy of v with a source span.
func (v Form) WithSpan(start, end int) Form {
	return v.withSpan(start, end)
}

// Lexeme is the original source spelling recorded at parse time, if any.
func (v Form) Lexeme() string {
	if v.src == nil {
		return ""
	}
	return v.src.lex
}

// WithLexeme returns a copy of v with original source spelling.
func (v Form) WithLexeme(s string) Form {
	src := v.cloneSrc()
	src.lex = s
	v.src = src
	return v
}

// WithComment returns a copy of v with a trailing comment.
func (v Form) WithComment(cmt string) Form {
	return v.withCmt(cmt)
}

// WithBlank marks v as following a blank line.
func (v Form) WithBlank() Form {
	s := v.cloneSrc()
	s.blank = true
	v.src = s
	return v
}

// WithBroke marks v as containing a newline in source.
func (v Form) WithBroke() Form {
	s := v.cloneSrc()
	s.broke = true
	v.src = s
	return v
}

// WithKeySpans records source spans of map keys.
func (v Form) WithKeySpans(spans map[string]Span) Form {
	s := v.cloneSrc()
	s.keySpans = spans
	v.src = s
	return v
}

// Inner returns the wrapped form of quote, unquote, or splice.
func (v Form) Inner() Form { return v.innerVal() }

// SetInner returns a copy of v wrapping in.
func (v Form) SetInner(in Form) Form { return v.setInner(in) }

// WithName returns a copy of a symbol with a new interned name.
func (v Form) WithName(name string) Form { return v.withName(name) }

// WithMap returns a copy of v with pairs replacing the map contents.
func (v Form) WithMap(pairs []MapPair) Form {
	m := newMap()
	for _, p := range pairs {
		m.put(p.Key, p.Value)
	}
	v.p = m
	return v
}

// IsKey reports whether v is a keyword symbol (name:).
func (v Form) IsKey() bool { return v.isKeySym() }

// KeyName returns the map/keyword name without the trailing colon.
func (v Form) KeyName() string { return v.keyName() }

// TrailingComment is the inline comment after v, if any.
func (v Form) TrailingComment() string {
	if v.src == nil {
		return ""
	}
	return v.src.cmt
}

// CommentText is the comment form body, including ';'.
func (v Form) CommentText() string { return v.commentText() }

// Blank reports whether a blank line preceded v.
func (v Form) Blank() bool { return v.src != nil && v.src.blank }

// Broke reports whether v's source contained a newline.
func (v Form) Broke() bool { return v.src != nil && v.src.broke }

// KeySpans returns recorded map key spans.
func (v Form) KeySpans() map[string]Span {
	if v.src == nil {
		return nil
	}
	return v.src.keySpans
}

// FilterComments drops comment forms.
func FilterComments(xs []Form) []Form { return filterComments(xs) }

// IsName reports whether v is the symbol name.
func IsName(v Form, name string) bool { return isSymName(v, name) }

// FormatSymbol renders a symbol name, quoting with ticks when needed.
func FormatSymbol(name string) string { return formatSymName(name) }

// Print renders v for display.
func Print(v Form) string { return printForm(v) }

// ParseInt parses a decimal integer.
func ParseInt(s string) (Form, bool) { return intFromString(s) }

// ReservedLit reports whether v is true, false, or nil.
func ReservedLit(v Form) bool { return reservedLit(v) }
