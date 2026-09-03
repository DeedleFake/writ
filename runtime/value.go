package runtime

import (
	"maps"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unique"

	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
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
	KindMacro
	KindNative
	KindSyntax
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
	case KindMacro:
		return "macro"
	case KindNative:
		return "native"
	case KindSyntax:
		return "syntax"
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
	Key   Value
	Value Value
}

// Value is a Writ runtime value.
type Value struct {
	k   Kind
	n   int64 // small int, or float64 bits
	s   string
	h   unique.Handle[string]
	p   any      // list, map, fn, *big.Int, native, syntax.Form
	src *srcInfo // nil for synthetic values
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
	xs  []Value
	vec bool
}

type mapData struct {
	keys []Value
	vals []Value
	idx  map[string]int
}

type fnVal struct {
	clauses []Clause
	keys    []string
	env     *env
	native  Func
	macro   Macro
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
	return Value{k: KindSymbol, h: unique.Make(name)}
}

func (v Value) cloneSrc() *srcInfo {
	if v.src == nil {
		return &srcInfo{}
	}
	cp := *v.src
	return &cp
}

func (v Value) list() *listData {
	ld, _ := v.p.(*listData)
	return ld
}

func (v Value) mapData() *mapData {
	m, _ := v.p.(*mapData)
	return m
}

func (v Value) fnData() *fnVal {
	f, _ := v.p.(*fnVal)
	return f
}

func (v Value) bigInt() *big.Int {
	b, _ := v.p.(*big.Int)
	return b
}

func (v Value) floatVal() float64 {
	return math.Float64frombits(uint64(v.n))
}

func listVal(xs []Value, vec bool) Value {
	return Value{k: KindList, p: &listData{xs: xs, vec: vec}}
}

// Int64 returns an integer value.
func Int64(n int64) Value {
	return Value{k: KindInt, n: n}
}

// Int returns an integer value. v is copied.
func Int(v *big.Int) Value {
	if v == nil {
		return Int64(0)
	}
	if v.IsInt64() {
		return Int64(v.Int64())
	}
	return Value{k: KindInt, p: new(big.Int).Set(v)}
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
	return Value{k: KindFloat, n: int64(math.Float64bits(f))}
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
	return listVal(xs, true)
}

// CallList returns a call-shaped list (parentheses).
func CallList(xs ...Value) Value {
	return listVal(xs, false)
}

// Native boxes a host object. Native values are opaque to Writ.
func Native(v any) Value {
	return Value{k: KindNative, p: v}
}

// Syntax boxes a source form as a runtime value. Nested quote evaluates
// to a Syntax value holding the remaining quote form.
func Syntax(f syntax.Form) Value {
	return Value{k: KindSyntax, p: f}
}

// MapFrom builds a map from pairs. Later keys win.
func MapFrom(pairs ...MapPair) Value {
	m := newMap()
	for _, p := range pairs {
		m.put(p.Key, p.Value)
	}
	return Value{k: KindMap, p: m}
}

// EmptyMap is `[:]`.
func EmptyMap() Value {
	return Value{k: KindMap, p: newMap()}
}

func newMap() *mapData {
	return &mapData{idx: make(map[string]int)}
}

func (m *mapData) clone() *mapData {
	if m == nil {
		return newMap()
	}
	out := &mapData{
		keys: append([]Value(nil), m.keys...),
		vals: append([]Value(nil), m.vals...),
		idx:  make(map[string]int, len(m.idx)),
	}
	maps.Copy(out.idx, m.idx)
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

func (m *mapData) put(k Value, v Value) {
	name := k.Name()
	if i, ok := m.idx[name]; ok {
		m.vals[i] = v
		return
	}
	m.idx[name] = len(m.keys)
	m.keys = append(m.keys, Symbol(name))
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
		m.idx[m.keys[j].Name()] = j
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
	s := v.cloneSrc()
	s.span = Span{Start: start, End: end}
	s.hasSpan = true
	v.src = s
	return v
}

func (v Value) withCmt(cmt string) Value {
	s := v.cloneSrc()
	s.cmt = cmt
	v.src = s
	return v
}

func (v Value) Kind() Kind { return v.k }

// Span returns the source span, if any.
func (v Value) Span() (Span, bool) {
	if v.src == nil || !v.src.hasSpan {
		return Span{}, false
	}
	return v.src.span, true
}

// HasSpan reports whether v has a source span.
func (v Value) HasSpan() bool { return v.src != nil && v.src.hasSpan }

func (v Value) IsNil() bool { return v.k == KindSymbol && v.h == Nil.h }

func (v Value) IsTrue() bool { return v.k == KindSymbol && v.h == True.h }

// IsFalse reports whether v is false.
func (v Value) IsFalse() bool { return v.k == KindSymbol && v.h == False.h }

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
	if b := v.bigInt(); b != nil {
		return new(big.Int).Set(b)
	}
	return big.NewInt(v.n)
}

func (v Value) asBig() *big.Int {
	if b := v.bigInt(); b != nil {
		return b
	}
	return big.NewInt(v.n)
}

// Float64 returns the float, or 0 if v is not a float.
func (v Value) Float64() float64 {
	if v.k != KindFloat {
		return 0
	}
	return v.floatVal()
}

// AsFloat64 converts an int or float to float64.
func (v Value) AsFloat64() (float64, bool) {
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
func (v Value) Text() string {
	if v.k != KindString {
		return ""
	}
	return v.s
}

// Name returns the interned symbol name, or "" if v is not a symbol.
func (v Value) Name() string {
	if v.k != KindSymbol {
		return ""
	}
	return v.h.Value()
}

// Items returns list elements. The slice must not be mutated.
func (v Value) Items() []Value {
	if v.k != KindList {
		return nil
	}
	if ld := v.list(); ld != nil {
		return ld.xs
	}
	return nil
}

// IsVec reports whether v is a [] list rather than a () call.
func (v Value) IsVec() bool {
	if v.k != KindList {
		return false
	}
	ld := v.list()
	return ld != nil && ld.vec
}

// Pairs returns map entries in insertion order.
func (v Value) Pairs() []MapPair {
	if v.k != KindMap {
		return nil
	}
	return v.mapData().pairs()
}

// MapGet looks up a symbol by name.
func (v Value) MapGet(key string) (Value, bool) {
	if v.k != KindMap {
		return Nil, false
	}
	return v.mapData().get(key)
}

// Native returns the boxed host object.
func (v Value) Native() (any, bool) {
	if v.k != KindNative {
		return nil, false
	}
	return v.p, true
}

// Form returns the boxed source form.
func (v Value) Form() (syntax.Form, bool) {
	if v.k != KindSyntax {
		return syntax.Form{}, false
	}
	f, _ := v.p.(syntax.Form)
	return f, true
}

// As type-asserts a native value into dst, which must be a non-nil *T.
func (v Value) As(dst any) bool {
	if v.k != KindNative || dst == nil {
		return false
	}
	dp := reflect.ValueOf(dst)
	if dp.Kind() != reflect.Pointer || dp.IsNil() {
		return false
	}
	elem := dp.Elem()
	if !elem.CanSet() {
		return false
	}
	if v.p == nil {
		switch elem.Kind() {
		case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan, reflect.UnsafePointer:
			elem.Set(reflect.Zero(elem.Type()))
			return true
		default:
			return false
		}
	}
	sv := reflect.ValueOf(v.p)
	if !sv.IsValid() || !sv.Type().AssignableTo(elem.Type()) {
		return false
	}
	elem.Set(sv)
	return true
}

// AsType type-asserts a KindNative value to T.
// Prefer it over [Value.As] when T is known at compile time.
func (v Value) AsType[T any]() (T, bool) {
	var zero T
	if v.k != KindNative {
		return zero, false
	}
	if v.p == nil {
		rt := reflect.TypeOf(&zero).Elem()
		switch rt.Kind() {
		case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map, reflect.Func, reflect.Chan, reflect.UnsafePointer:
			return zero, true
		default:
			return zero, false
		}
	}
	out, ok := v.p.(T)
	return out, ok
}

func (v Value) isKeySym() bool {
	if v.k != KindSymbol {
		return false
	}
	s := v.Name()
	return len(s) > 1 && strings.HasSuffix(s, ":")
}

func (v Value) keyName() string {
	if !v.isKeySym() {
		return ""
	}
	s := v.Name()
	return s[:len(s)-1]
}

func isSymName(v Value, name string) bool {
	return v.k == KindSymbol && v.h == unique.Make(name)
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

func formatInt(v Value) string {
	if b := v.bigInt(); b != nil {
		return b.String()
	}
	return strconv.FormatInt(v.n, 10)
}

func printNative(p any) string {
	if p == nil {
		return "#<native nil>"
	}
	if wh, ok := p.(*wireHandle); ok {
		if isGuestHandleID(wh.id) {
			return "#<handle guest>"
		}
		return "#<handle host>"
	}
	return "#<native " + reflect.TypeOf(p).String() + ">"
}

func nativeEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Type() != vb.Type() || !va.Comparable() || !vb.Comparable() {
		return false
	}
	return a == b
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
		return formatFloat(v.floatVal())
	case KindString:
		return v.s
	case KindSymbol:
		return v.Name()
	case KindFn:
		return "#<fn>"
	case KindMacro:
		return "#<macro>"
	case KindNative:
		return printNative(v.p)
	case KindSyntax:
		if f, ok := v.Form(); ok {
			return syntax.Print(f)
		}
		return "#<syntax>"
	case KindList:
		xs := v.Items()
		var b strings.Builder
		b.WriteByte('[')
		for i, x := range xs {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(printVal(x))
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
			b.WriteString(printVal(m.vals[i]))
		}
		b.WriteByte(']')
		return b.String()
	default:
		return "#<invalid>"
	}
}

// Equal reports value equality. 1 and 1.0 compare equal. Functions never
// compare equal. Native values use == when the dynamic type is comparable.
func (v Value) Equal(o Value) bool {
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
	case KindString:
		return v.s == o.s
	case KindSymbol:
		return v.h == o.h
	case KindFn, KindMacro:
		return false
	case KindNative:
		return nativeEqual(v.p, o.p)
	case KindSyntax:
		af, aok := v.Form()
		bf, bok := o.Form()
		return aok && bok && af.Equal(bf)
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

func intFloatEqual(i Value, f float64) bool {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return false
	}
	bf := new(big.Float).SetInt(i.asBig())
	ff := new(big.Float).SetFloat64(f)
	return bf.Cmp(ff) == 0
}

// WithSpan returns a copy of v with a source span.
func (v Value) WithSpan(start, end int) Value {
	return v.withSpan(start, end)
}

// Lexeme is the original source spelling recorded at parse time, if any.
func (v Value) Lexeme() string {
	if v.src == nil {
		return ""
	}
	return v.src.lex
}

// WithLexeme returns a copy of v with original source spelling.
func (v Value) WithLexeme(s string) Value {
	src := v.cloneSrc()
	src.lex = s
	v.src = src
	return v
}

// WithComment returns a copy of v with a trailing comment.
func (v Value) WithComment(cmt string) Value {
	return v.withCmt(cmt)
}

// WithBlank marks v as following a blank line.
func (v Value) WithBlank() Value {
	s := v.cloneSrc()
	s.blank = true
	v.src = s
	return v
}

// WithBroke marks v as containing a newline in source.
func (v Value) WithBroke() Value {
	s := v.cloneSrc()
	s.broke = true
	v.src = s
	return v
}

// WithKeySpans records source spans of map keys.
func (v Value) WithKeySpans(spans map[string]Span) Value {
	s := v.cloneSrc()
	s.keySpans = spans
	v.src = s
	return v
}

// IsKey reports whether v is a keyword symbol (name:).
func (v Value) IsKey() bool { return v.isKeySym() }

// KeyName returns the map/keyword name without the trailing colon.
func (v Value) KeyName() string { return v.keyName() }

// TrailingComment is the inline comment after v, if any.
func (v Value) TrailingComment() string {
	if v.src == nil {
		return ""
	}
	return v.src.cmt
}

// Blank reports whether a blank line preceded v.
func (v Value) Blank() bool { return v.src != nil && v.src.blank }

// Broke reports whether v's source contained a newline.
func (v Value) Broke() bool { return v.src != nil && v.src.broke }

// KeySpans returns recorded map key spans.
func (v Value) KeySpans() map[string]Span {
	if v.src == nil {
		return nil
	}
	return v.src.keySpans
}

// IsName reports whether v is the symbol name.
func IsName(v Value, name string) bool { return isSymName(v, name) }

// FormatSymbol renders a symbol name, quoting with ticks when needed.
func FormatSymbol(name string) string { return formatSymName(name) }

// Print renders v for display, matching the language printer.
func Print(v Value) string { return printVal(v) }

// ParseInt parses a decimal integer.
func ParseInt(s string) (Value, bool) { return intFromString(s) }
