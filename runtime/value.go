package runtime

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"deedles.dev/writ/scanner"
)

// Kind is the runtime kind of a [Value].
type Kind int

const (
	KindInvalid Kind = iota
	KindInt
	KindFloat
	KindString
	KindSymbol
	KindList
	KindMap
	KindFn
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
	case KindFn:
		return "fn"
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

// MapPair is one map entry.
type MapPair struct {
	Key   string
	Value Value
}

// Value is a Writ value or source form.
type Value struct {
	k        Kind
	i        int64
	big      *big.Int
	f        float64
	s        string
	xs       []Value
	vec      bool
	mp       *mapData
	fn       *fnVal
	inner    *Value
	span     Span
	hasSpan  bool
	cmt      string
	blank    bool
	broke    bool
	keySpans map[string]Span
}

type mapData struct {
	keys []string
	vals []Value
	idx  map[string]int
}

type fnVal struct {
	clauses []Clause
	keys    []string
	env     *env
	native  Func
	name    string
}

// True, False, and Nil are interned symbols. True and False are booleans;
// Nil is not.
var (
	True  = internSym("true")
	False = internSym("false")
	Nil   = internSym("nil")
)

func internSym(name string) Value {
	return Value{k: KindSymbol, s: name}
}

// Int64 returns an integer value.
func Int64(n int64) Value {
	return Value{k: KindInt, i: n}
}

// Int returns an integer value. v is copied.
func Int(v *big.Int) Value {
	if v == nil {
		return Int64(0)
	}
	if v.IsInt64() {
		return Int64(v.Int64())
	}
	return Value{k: KindInt, big: new(big.Int).Set(v)}
}

// MustInt parses a decimal integer. It panics on error.
func MustInt(s string) Value {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("writ: invalid integer " + s)
	}
	return Int(n)
}

// Float returns a float64 value.
func Float(f float64) Value {
	return Value{k: KindFloat, f: f}
}

// String returns a string value.
func String(s string) Value {
	return Value{k: KindString, s: s}
}

// Symbol returns an interned symbol.
func Symbol(name string) Value {
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
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// List returns a data list (vector).
func List(xs ...Value) Value {
	return Value{k: KindList, xs: xs, vec: true}
}

// CallList returns a call-shaped list (parentheses).
func CallList(xs ...Value) Value {
	return Value{k: KindList, xs: xs, vec: false}
}

// Quote wraps v.
func Quote(v Value) Value {
	inner := v
	return Value{k: KindQuote, inner: &inner}
}

// Unquote wraps v.
func Unquote(v Value) Value {
	inner := v
	return Value{k: KindUnquote, inner: &inner}
}

// Splice wraps v.
func Splice(v Value) Value {
	inner := v
	return Value{k: KindSplice, inner: &inner}
}

// MapFrom builds a map from pairs. Later keys win.
func MapFrom(pairs ...MapPair) Value {
	m := newMap()
	for _, p := range pairs {
		m.put(p.Key, p.Value)
	}
	return Value{k: KindMap, mp: m}
}

// EmptyMap is `[:]`.
func EmptyMap() Value {
	return Value{k: KindMap, mp: newMap()}
}

func newMap() *mapData {
	return &mapData{idx: make(map[string]int)}
}

func (m *mapData) clone() *mapData {
	if m == nil {
		return newMap()
	}
	out := &mapData{
		keys: append([]string(nil), m.keys...),
		vals: append([]Value(nil), m.vals...),
		idx:  make(map[string]int, len(m.idx)),
	}
	for k, i := range m.idx {
		out.idx[k] = i
	}
	return out
}

func (m *mapData) get(k string) (Value, bool) {
	if m == nil {
		return Nil, false
	}
	i, ok := m.idx[k]
	if !ok {
		return Nil, false
	}
	return m.vals[i], true
}

func (m *mapData) put(k string, v Value) {
	if i, ok := m.idx[k]; ok {
		m.vals[i] = v
		return
	}
	m.idx[k] = len(m.keys)
	m.keys = append(m.keys, k)
	m.vals = append(m.vals, v)
}

func (m *mapData) del(k string) {
	i, ok := m.idx[k]
	if !ok {
		return
	}
	m.keys = append(m.keys[:i], m.keys[i+1:]...)
	m.vals = append(m.vals[:i], m.vals[i+1:]...)
	delete(m.idx, k)
	for j := i; j < len(m.keys); j++ {
		m.idx[m.keys[j]] = j
	}
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

func (v Value) withSpan(start, end int) Value {
	v.span = Span{Start: start, End: end}
	v.hasSpan = true
	return v
}

func (v Value) withCmt(cmt string) Value {
	v.cmt = cmt
	return v
}

func (v Value) innerVal() Value {
	if v.inner == nil {
		return Nil
	}
	return *v.inner
}

func (v Value) setInner(in Value) Value {
	cp := in
	v.inner = &cp
	return v
}

func (v Value) Kind() Kind { return v.k }

// Span returns the source span, if any.
func (v Value) Span() (Span, bool) {
	if !v.hasSpan {
		return Span{}, false
	}
	return v.span, true
}

// HasSpan reports whether v has a source span.
func (v Value) HasSpan() bool { return v.hasSpan }

func (v Value) IsNil() bool { return v.k == KindSymbol && v.s == "nil" }

func (v Value) IsTrue() bool { return v.k == KindSymbol && v.s == "true" }

// IsFalse reports whether v is false.
func (v Value) IsFalse() bool { return v.k == KindSymbol && v.s == "false" }

func (v Value) IsBool() bool { return v.IsTrue() || v.IsFalse() }

// Truthy is false only for nil and false.
func (v Value) Truthy() bool { return !v.IsNil() && !v.IsFalse() }

// IsInt reports whether v is an integer.
func (v Value) IsInt() bool { return v.k == KindInt }

// IsFloat reports whether v is a float.
func (v Value) IsFloat() bool { return v.k == KindFloat }

// IsNum reports whether v is an int or a float.
func (v Value) IsNum() bool { return v.k == KindInt || v.k == KindFloat }

// BigInt returns a copy of the integer. It is 0 if v is not an int.
func (v Value) BigInt() *big.Int {
	if v.k != KindInt {
		return new(big.Int)
	}
	if v.big != nil {
		return new(big.Int).Set(v.big)
	}
	return big.NewInt(v.i)
}

func (v Value) asBig() *big.Int {
	if v.big != nil {
		return v.big
	}
	return big.NewInt(v.i)
}

// Float64 returns the float, or 0 if v is not a float.
func (v Value) Float64() float64 {
	if v.k != KindFloat {
		return 0
	}
	return v.f
}

// AsFloat64 converts an int or float to float64.
func (v Value) AsFloat64() (float64, bool) {
	switch v.k {
	case KindFloat:
		return v.f, true
	case KindInt:
		if v.big != nil {
			f, _ := new(big.Float).SetInt(v.big).Float64()
			return f, true
		}
		return float64(v.i), true
	default:
		return 0, false
	}
}

// Text returns the string contents, or "" if v is not a string.
func (v Value) Text() string {
	if v.k != KindString {
		return ""
	}
	return v.s
}

// Name returns the symbol name, or "" if v is not a symbol.
func (v Value) Name() string {
	if v.k != KindSymbol {
		return ""
	}
	return v.s
}

// Items returns list elements. The slice must not be mutated.
func (v Value) Items() []Value {
	if v.k != KindList {
		return nil
	}
	return v.xs
}

// IsVec reports whether v is a [] list rather than a () call.
func (v Value) IsVec() bool { return v.k == KindList && v.vec }

// Pairs returns map entries in insertion order.
func (v Value) Pairs() []MapPair {
	if v.k != KindMap {
		return nil
	}
	return v.mp.pairs()
}

// MapGet looks up a string key.
func (v Value) MapGet(key string) (Value, bool) {
	if v.k != KindMap {
		return Nil, false
	}
	return v.mp.get(key)
}

func (v Value) commentText() string {
	if v.k != KindComment {
		return ""
	}
	return v.s
}

func (v Value) isKeySym() bool {
	return v.k == KindSymbol && len(v.s) > 1 && strings.HasSuffix(v.s, ":")
}

func (v Value) keyName() string {
	if !v.isKeySym() {
		return ""
	}
	return v.s[:len(v.s)-1]
}

func reservedLit(v Value) bool {
	return v.IsTrue() || v.IsFalse() || v.IsNil()
}

func isSymName(v Value, name string) bool {
	return v.k == KindSymbol && v.s == name
}

func filterComments(xs []Value) []Value {
	out := make([]Value, 0, len(xs))
	for _, x := range xs {
		if x.k != KindComment {
			out = append(out, x)
		}
	}
	return out
}

func intFromString(s string) (Value, bool) {
	if len(s) == 0 {
		return Value{}, false
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return Value{}, false
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

func formatInt(v Value) string {
	if v.big != nil {
		return v.big.String()
	}
	return strconv.FormatInt(v.i, 10)
}

// String renders v for display, matching the language printer.
func (v Value) String() string {
	return printVal(v)
}

func printVal(v Value) string {
	switch v.k {
	case KindInt:
		return formatInt(v)
	case KindFloat:
		return formatFloat(v.f)
	case KindString:
		return v.s
	case KindSymbol:
		return v.s
	case KindFn:
		return "#<fn>"
	case KindList:
		var b strings.Builder
		b.WriteByte('[')
		for i, x := range v.xs {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(printVal(x))
		}
		b.WriteByte(']')
		return b.String()
	case KindMap:
		if v.mp.len() == 0 {
			return "[:]"
		}
		var b strings.Builder
		b.WriteByte('[')
		for i, k := range v.mp.keys {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(printVal(v.mp.vals[i]))
		}
		b.WriteByte(']')
		return b.String()
	case KindComment:
		return v.s
	case KindQuote:
		return "'" + printVal(v.innerVal())
	case KindUnquote:
		return "," + printVal(v.innerVal())
	case KindSplice:
		return "@" + printVal(v.innerVal())
	default:
		return "#<invalid>"
	}
}

// Equal reports value equality. 1 and 1.0 compare equal. Functions never
// compare equal.
func (v Value) Equal(o Value) bool {
	if v.k == KindInt && o.k == KindFloat {
		return intFloatEqual(v, o.f)
	}
	if v.k == KindFloat && o.k == KindInt {
		return intFloatEqual(o, v.f)
	}
	if v.k != o.k {
		return false
	}
	switch v.k {
	case KindInt:
		return v.asBig().Cmp(o.asBig()) == 0
	case KindFloat:
		return v.f == o.f
	case KindString, KindSymbol, KindComment:
		return v.s == o.s
	case KindFn:
		return false
	case KindQuote, KindUnquote, KindSplice:
		return v.innerVal().Equal(o.innerVal())
	case KindList:
		if len(v.xs) != len(o.xs) {
			return false
		}
		for i := range v.xs {
			if !v.xs[i].Equal(o.xs[i]) {
				return false
			}
		}
		return true
	case KindMap:
		if v.mp.len() != o.mp.len() {
			return false
		}
		for i, k := range v.mp.keys {
			ov, ok := o.mp.get(k)
			if !ok || !v.mp.vals[i].Equal(ov) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func intFloatEqual(i Value, f float64) bool {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return false
	}
	bf := new(big.Float).SetInt(i.asBig())
	ff := new(big.Float).SetFloat64(f)
	return bf.Cmp(ff) == 0
}

func cloneList(xs []Value, vec bool) Value {
	out := make([]Value, len(xs))
	copy(out, xs)
	return Value{k: KindList, xs: out, vec: vec}
}

// Comment returns a comment form. text is the source including ';'.
func Comment(text string) Value {
	return Value{k: KindComment, s: text}
}

// WithSpan returns a copy of v with a source span.
func (v Value) WithSpan(start, end int) Value {
	return v.withSpan(start, end)
}

// WithComment returns a copy of v with a trailing comment.
func (v Value) WithComment(cmt string) Value {
	return v.withCmt(cmt)
}

// WithBlank marks v as following a blank line.
func (v Value) WithBlank() Value {
	v.blank = true
	return v
}

// WithBroke marks v as containing a newline in source.
func (v Value) WithBroke() Value {
	v.broke = true
	return v
}

// WithKeySpans records source spans of map keys.
func (v Value) WithKeySpans(spans map[string]Span) Value {
	v.keySpans = spans
	return v
}

// Inner returns the wrapped value of quote, unquote, or splice.
func (v Value) Inner() Value { return v.innerVal() }

// SetInner returns a copy of v wrapping in.
func (v Value) SetInner(in Value) Value { return v.setInner(in) }

// IsKey reports whether v is a keyword symbol (name:).
func (v Value) IsKey() bool { return v.isKeySym() }

// KeyName returns the map/keyword name without the trailing colon.
func (v Value) KeyName() string { return v.keyName() }

// TrailingComment is the inline comment after v, if any.
func (v Value) TrailingComment() string { return v.cmt }

// CommentText is the comment form body, including ';'.
func (v Value) CommentText() string { return v.commentText() }

// Blank reports whether a blank line preceded v.
func (v Value) Blank() bool { return v.blank }

// Broke reports whether v's source contained a newline.
func (v Value) Broke() bool { return v.broke }

// KeySpans returns recorded map key spans.
func (v Value) KeySpans() map[string]Span { return v.keySpans }

// FilterComments drops comment forms.
func FilterComments(xs []Value) []Value { return filterComments(xs) }

// IsName reports whether v is the symbol name.
func IsName(v Value, name string) bool { return isSymName(v, name) }

// FormatSymbol renders a symbol name, quoting with ticks when needed.
func FormatSymbol(name string) string { return formatSymName(name) }

// Print renders v for display, matching the language printer.
func Print(v Value) string { return printVal(v) }

// ParseInt parses a decimal integer.
func ParseInt(s string) (Value, bool) { return intFromString(s) }
