package types

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"deedles.dev/writ/runtime"
)

type typeKind int

const (
	tyNone typeKind = iota
	tyAny
	tyNil
	tyBool
	tyInt
	tyFloat
	tyStr
	tyUStr
	tySym
	tyUSym
	tyEmptyList
	tyEmptyMap
	tyList
	tyTuple
	tyMap
	tyFn
	tyOr
	tyDyn
	tyNative
)

// Type is a static type. Use the constructors such as [IntType] and [Union].
type Type struct {
	k      typeKind
	has    bool
	b      bool
	s      string
	inner  *Type
	items  []Type
	fields []mapField
	rest   *Type
	arrows []Arrow
	rt     reflect.Type
}

type mapField struct {
	name string
	t    Type
}

// Arrow is one function clause type.
type Arrow struct {
	Key       bool
	Args      []Type
	Keys      []ArrowKey
	Rest      bool
	Result    Type
	restTyped bool
}

// ArrowKey is a keyword argument type.
type ArrowKey struct {
	Name string
	Type Type
}

// PosArrow builds a positional arrow.
func PosArrow(ret Type, args ...Type) Arrow {
	return Arrow{Args: args, Result: ret}
}

// PosRestArrow is a positional arrow that accepts extra arguments.
func PosRestArrow(ret Type, args ...Type) Arrow {
	return Arrow{Args: args, Rest: true, Result: ret}
}

// KeyArrow builds a keyword arrow.
func KeyArrow(ret Type, keys ...ArrowKey) Arrow {
	return Arrow{Key: true, Keys: keys, Result: ret}
}

func None() Type          { return Type{k: tyNone} }
func Any() Type           { return Type{k: tyAny} }
func NilType() Type       { return Type{k: tyNil} }
func BoolType() Type      { return Type{k: tyBool} }
func TrueType() Type      { return Type{k: tyBool, has: true, b: true} }
func FalseType() Type     { return Type{k: tyBool, has: true, b: false} }
func IntType() Type       { return Type{k: tyInt} }
func FloatType() Type     { return Type{k: tyFloat} }
func StringType() Type    { return Type{k: tyStr} }
func UnknownString() Type { return Type{k: tyUStr} }
func SymbolType() Type    { return Type{k: tySym} }
func UnknownSymbol() Type { return Type{k: tyUSym} }
func EmptyList() Type     { return Type{k: tyEmptyList} }
func EmptyMapType() Type  { return Type{k: tyEmptyMap} }

func ExactString(s string) Type { return Type{k: tyStr, has: true, s: s} }

func ExactSymbol(s string) Type {
	switch s {
	case "true":
		return TrueType()
	case "false":
		return FalseType()
	case "nil":
		return NilType()
	}
	return Type{k: tySym, has: true, s: s}
}

func ListOf(el Type) Type {
	inner := el
	return Type{k: tyList, inner: &inner}
}

func Tuple(items ...Type) Type {
	if len(items) == 0 {
		return EmptyList()
	}
	return Type{k: tyTuple, items: items}
}

func MapType(keys []ArrowKey, rest *Type) Type {
	if len(keys) == 0 && rest == nil {
		return EmptyMapType()
	}
	fields := make([]mapField, len(keys))
	for i, k := range keys {
		fields[i] = mapField{name: k.Name, t: k.Type}
	}
	return Type{k: tyMap, fields: fields, rest: rest}
}

func FnType(arrows ...Arrow) Type {
	return Type{k: tyFn, arrows: arrows}
}

var nativeIntern sync.Map // reflect.Type -> Type

func nativeOf(rt reflect.Type) Type {
	if rt == nil {
		return Type{k: tyNative}
	}
	if v, ok := nativeIntern.Load(rt); ok {
		return v.(Type)
	}
	t := Type{k: tyNative, rt: rt}
	actual, _ := nativeIntern.LoadOrStore(rt, t)
	return actual.(Type)
}

// Native is a nominal host type, interned by [reflect.Type]. Different
// type parameters do not unify.
func Native[T any]() Type {
	return nativeOf(reflect.TypeFor[T]())
}

func Union(ts ...Type) Type { return tOr(ts) }

func Dynamic(t Type) Type { return tDyn(t) }

func tDyn(t Type) Type {
	if t.k == tyDyn {
		return t
	}
	if t.k == tyNone {
		return t
	}
	inner := t
	return Type{k: tyDyn, inner: &inner}
}

func tOr(ts []Type) Type {
	var flat []Type
	for _, t := range ts {
		if t.k == tyNone {
			continue
		}
		if t.k == tyAny {
			return Any()
		}
		if t.k == tyOr {
			flat = append(flat, t.items...)
		} else {
			flat = append(flat, t)
		}
	}
	var out []Type
	for _, t := range flat {
		dup := false
		for _, u := range out {
			if sameType(u, t) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return None()
	}
	if len(out) == 1 {
		return out[0]
	}
	return Type{k: tyOr, items: out}
}

func tList(el Type) Type { return ListOf(el) }

func tTuple(items []Type) Type { return Tuple(items...) }

func tMap(keys []mapField, rest *Type) Type {
	if len(keys) == 0 && rest == nil {
		return EmptyMapType()
	}
	return Type{k: tyMap, fields: keys, rest: rest}
}

func unwrap(t Type) Type {
	if t.k == tyDyn && t.inner != nil {
		return *t.inner
	}
	return t
}

func fromUsage(t Type) Type {
	u := unwrap(t)
	if u.k == tyAny {
		return tDyn(Any())
	}
	return u
}

func sameType(a, b Type) bool {
	if a.k != b.k {
		return false
	}
	switch a.k {
	case tyNone, tyAny, tyNil, tyInt, tyFloat, tyUStr, tyUSym, tyEmptyList, tyEmptyMap:
		return true
	case tyBool:
		return a.has == b.has && a.b == b.b
	case tyStr, tySym:
		return a.has == b.has && a.s == b.s
	case tyList:
		return sameType(*a.inner, *b.inner)
	case tyTuple, tyOr:
		if len(a.items) != len(b.items) {
			return false
		}
		if a.k == tyOr {
			used := make([]bool, len(b.items))
			for _, x := range a.items {
				found := false
				for i, y := range b.items {
					if !used[i] && sameType(x, y) {
						used[i] = true
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
			return true
		}
		for i := range a.items {
			if !sameType(a.items[i], b.items[i]) {
				return false
			}
		}
		return true
	case tyMap:
		if (a.rest == nil) != (b.rest == nil) {
			return false
		}
		if a.rest != nil && !sameType(*a.rest, *b.rest) {
			return false
		}
		if len(a.fields) != len(b.fields) {
			return false
		}
		bm := map[string]Type{}
		for _, f := range b.fields {
			bm[f.name] = f.t
		}
		for _, f := range a.fields {
			bt, ok := bm[f.name]
			if !ok || !sameType(f.t, bt) {
				return false
			}
		}
		return true
	case tyFn:
		if len(a.arrows) != len(b.arrows) {
			return false
		}
		for i := range a.arrows {
			if !sameArrow(a.arrows[i], b.arrows[i]) {
				return false
			}
		}
		return true
	case tyDyn:
		return sameType(*a.inner, *b.inner)
	case tyNative:
		return a.rt == b.rt
	default:
		return false
	}
}

func sameArrow(a, b Arrow) bool {
	if a.Key != b.Key || a.Rest != b.Rest || !sameType(a.Result, b.Result) {
		return false
	}
	if a.Key {
		if len(a.Keys) != len(b.Keys) {
			return false
		}
		bm := map[string]Type{}
		for _, k := range b.Keys {
			bm[k.Name] = k.Type
		}
		for _, k := range a.Keys {
			bt, ok := bm[k.Name]
			if !ok || !sameType(k.Type, bt) {
				return false
			}
		}
		return true
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if !sameType(a.Args[i], b.Args[i]) {
			return false
		}
	}
	return true
}

func isNone(t Type) bool { return unwrap(t).k == tyNone }

func (t Type) field(name string) (Type, bool) {
	for _, f := range t.fields {
		if f.name == name {
			return f.t, true
		}
	}
	return Type{}, false
}

// PrintType renders t in the checker notation.
func PrintType(t Type) string { return printType(t, nil) }

func printType(t Type, aliases []Alias) string {
	switch t.k {
	case tyNone:
		return "none()"
	case tyAny:
		return "any()"
	case tyNil:
		return "nil"
	case tyBool:
		if !t.has {
			return "bool()"
		}
		if t.b {
			return "true"
		}
		return "false"
	case tyInt:
		return "int()"
	case tyFloat:
		return "float()"
	case tyStr:
		if !t.has {
			return "string()"
		}
		b, _ := json.Marshal(t.s)
		return string(b)
	case tyUStr:
		return "unknown_string()"
	case tySym:
		if !t.has {
			return "symbol()"
		}
		return "'" + runtime.FormatSymbol(t.s)
	case tyUSym:
		return "unknown_symbol()"
	case tyEmptyList:
		return "empty_list()"
	case tyEmptyMap:
		return "empty_map()"
	case tyList:
		return "list(" + printType(*t.inner, aliases) + ")"
	case tyTuple:
		parts := make([]string, len(t.items))
		for i, x := range t.items {
			parts[i] = printType(x, aliases)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case tyMap:
		var parts []string
		for _, f := range t.fields {
			parts = append(parts, f.name+": "+printType(f.t, aliases))
		}
		if t.rest != nil {
			parts = append(parts, "*: "+printType(*t.rest, aliases))
		}
		if len(parts) == 0 {
			return "empty_map()"
		}
		return "[" + strings.Join(parts, " ") + "]"
	case tyFn:
		if len(t.arrows) == 0 {
			return "fn"
		}
		var parts []string
		for _, ar := range t.arrows {
			p := printArrow(ar, aliases)
			dup := false
			for _, x := range parts {
				if x == p {
					dup = true
					break
				}
			}
			if !dup {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, " and ")
	case tyOr:
		if name := closedLitName(t, aliases); name != "" {
			return name
		}
		var lits []string
		var rest []Type
		for _, x := range t.items {
			if x.k == tyStr && x.has {
				lits = append(lits, x.s)
			} else {
				rest = append(rest, x)
			}
		}
		var parts []string
		if len(lits) > 0 {
			quoted := make([]string, len(lits))
			for i, v := range lits {
				b, _ := json.Marshal(v)
				quoted[i] = string(b)
			}
			parts = append(parts, strings.Join(quoted, " or "))
		}
		for _, x := range rest {
			parts = append(parts, printType(x, aliases))
		}
		return strings.Join(parts, " or ")
	case tyDyn:
		return "dynamic(" + printType(*t.inner, aliases) + ")"
	case tyNative:
		if t.rt == nil {
			return "native()"
		}
		return "native(" + t.rt.String() + ")"
	default:
		return "any()"
	}
}

func printArrow(ar Arrow, aliases []Alias) string {
	if !ar.Key {
		args := make([]string, len(ar.Args))
		for i, t := range ar.Args {
			args[i] = printType(t, aliases)
		}
		if ar.Rest {
			args = append(args, "...")
		}
		return "(" + strings.Join(args, " ") + ") -> " + printType(ar.Result, aliases)
	}
	keys := make([]string, len(ar.Keys))
	for i, k := range ar.Keys {
		keys[i] = k.Name + ": " + printType(k.Type, aliases)
	}
	return "(" + strings.Join(keys, " ") + ") -> " + printType(ar.Result, aliases)
}

func closedLitName(t Type, aliases []Alias) string {
	if t.k != tyOr {
		return ""
	}
	var lits []string
	for _, x := range t.items {
		if x.k != tyStr || !x.has {
			return ""
		}
		lits = append(lits, x.s)
	}
	for _, a := range aliases {
		if sameStringSet(lits, a.Members) {
			return a.Name + "()"
		}
	}
	return ""
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
		if m[x] < 0 {
			return false
		}
	}
	return true
}

func intersect(a, b Type) Type {
	if a.k == tyDyn {
		return tDyn(intersect(*a.inner, b))
	}
	if b.k == tyDyn {
		return tDyn(intersect(a, *b.inner))
	}
	if a.k == tyAny {
		return b
	}
	if b.k == tyAny {
		return a
	}
	if a.k == tyNone || b.k == tyNone {
		return None()
	}
	if a.k == tyOr {
		out := make([]Type, len(a.items))
		for i, t := range a.items {
			out[i] = intersect(t, b)
		}
		return tOr(out)
	}
	if b.k == tyOr {
		out := make([]Type, len(b.items))
		for i, t := range b.items {
			out[i] = intersect(a, t)
		}
		return tOr(out)
	}
	if a.k == tyNil && b.k == tyNil {
		return NilType()
	}
	if a.k == tyInt && b.k == tyInt {
		return IntType()
	}
	if a.k == tyFloat && b.k == tyFloat {
		return FloatType()
	}
	if a.k == tyBool && b.k == tyBool {
		if !a.has {
			return b
		}
		if !b.has {
			return a
		}
		if a.b == b.b {
			return a
		}
		return None()
	}
	if isTrueT(a) {
		if (b.k == tyBool && (!b.has || b.b)) || (b.k == tySym && (!b.has || b.s == "true")) {
			return TrueType()
		}
	}
	if isTrueT(b) {
		if (a.k == tyBool && (!a.has || a.b)) || (a.k == tySym && (!a.has || a.s == "true")) {
			return TrueType()
		}
	}
	if isFalseT(a) {
		if (b.k == tyBool && (!b.has || !b.b)) || (b.k == tySym && (!b.has || b.s == "false")) {
			return FalseType()
		}
	}
	if isFalseT(b) {
		if (a.k == tyBool && (!a.has || !a.b)) || (a.k == tySym && (!a.has || a.s == "false")) {
			return FalseType()
		}
	}
	if a.k == tyNil && b.k == tySym && (!b.has || b.s == "nil") {
		return NilType()
	}
	if b.k == tyNil && a.k == tySym && (!a.has || a.s == "nil") {
		return NilType()
	}
	if a.k == tyBool && !a.has && b.k == tySym && !b.has {
		return BoolType()
	}
	if b.k == tyBool && !b.has && a.k == tySym && !a.has {
		return BoolType()
	}
	if a.k == tyUStr && b.k == tyUStr {
		return UnknownString()
	}
	if a.k == tyStr && b.k == tyStr {
		if !a.has {
			return b
		}
		if !b.has {
			return a
		}
		if a.s == b.s {
			return a
		}
		return None()
	}
	if a.k == tyUStr && b.k == tyStr && !b.has {
		return UnknownString()
	}
	if b.k == tyUStr && a.k == tyStr && !a.has {
		return UnknownString()
	}
	if a.k == tyUSym && b.k == tyUSym {
		return UnknownSymbol()
	}
	if a.k == tySym && b.k == tySym {
		if !a.has {
			return b
		}
		if !b.has {
			return a
		}
		if a.s == b.s {
			return a
		}
		return None()
	}
	if a.k == tyUSym && b.k == tySym && !b.has {
		return UnknownSymbol()
	}
	if b.k == tyUSym && a.k == tySym && !a.has {
		return UnknownSymbol()
	}
	if a.k == tyEmptyList {
		if b.k == tyEmptyList || b.k == tyList {
			return EmptyList()
		}
		return None()
	}
	if b.k == tyEmptyList {
		if a.k == tyList {
			return EmptyList()
		}
		return None()
	}
	if a.k == tyList && b.k == tyList {
		return tList(intersect(*a.inner, *b.inner))
	}
	if a.k == tyTuple && b.k == tyTuple {
		if len(a.items) != len(b.items) {
			return None()
		}
		items := make([]Type, len(a.items))
		for i := range a.items {
			items[i] = intersect(a.items[i], b.items[i])
			if isNone(items[i]) {
				return None()
			}
		}
		return tTuple(items)
	}
	if a.k == tyList && b.k == tyTuple {
		items := make([]Type, len(b.items))
		for i, t := range b.items {
			items[i] = intersect(t, *a.inner)
			if isNone(items[i]) {
				return None()
			}
		}
		return tTuple(items)
	}
	if b.k == tyList && a.k == tyTuple {
		items := make([]Type, len(a.items))
		for i, t := range a.items {
			items[i] = intersect(t, *b.inner)
			if isNone(items[i]) {
				return None()
			}
		}
		return tTuple(items)
	}
	if a.k == tyEmptyMap {
		if b.k == tyEmptyMap {
			return EmptyMapType()
		}
		if b.k == tyMap && len(b.fields) == 0 {
			return EmptyMapType()
		}
		return None()
	}
	if b.k == tyEmptyMap {
		if a.k == tyMap && len(a.fields) == 0 {
			return EmptyMapType()
		}
		return None()
	}
	if a.k == tyMap && b.k == tyMap {
		names := map[string]struct{}{}
		for _, f := range a.fields {
			names[f.name] = struct{}{}
		}
		for _, f := range b.fields {
			names[f.name] = struct{}{}
		}
		var keys []mapField
		for n := range names {
			av, aok := a.field(n)
			if !aok && a.rest != nil {
				av, aok = *a.rest, true
			}
			bv, bok := b.field(n)
			if !bok && b.rest != nil {
				bv, bok = *b.rest, true
			}
			if !aok || !bok {
				continue
			}
			iv := intersect(av, bv)
			if !isNone(iv) {
				keys = append(keys, mapField{name: n, t: iv})
			}
		}
		var rest *Type
		if a.rest != nil && b.rest != nil {
			r := intersect(*a.rest, *b.rest)
			if !isNone(r) {
				rest = &r
			}
		}
		if len(keys) == 0 && rest == nil {
			return None()
		}
		return tMap(keys, rest)
	}
	if a.k == tyFn && b.k == tyFn {
		arrows := append(append([]Arrow{}, a.arrows...), b.arrows...)
		return Type{k: tyFn, arrows: arrows}
	}
	if a.k == tyNative && b.k == tyNative && a.rt == b.rt {
		return a
	}
	return None()
}

func isTrueT(t Type) bool {
	return (t.k == tyBool && t.has && t.b) || (t.k == tySym && t.has && t.s == "true")
}

func isFalseT(t Type) bool {
	return (t.k == tyBool && t.has && !t.b) || (t.k == tySym && t.has && t.s == "false")
}

func overlaps(got, expected Type) bool {
	return !isNone(intersect(unwrap(got), unwrap(expected)))
}

func isSubtype(a, b Type) bool {
	a, b = unwrap(a), unwrap(b)
	if a.k == tyNone || b.k == tyAny {
		return true
	}
	if a.k == tyAny {
		return false
	}
	if a.k == tyOr {
		for _, t := range a.items {
			if !isSubtype(t, b) {
				return false
			}
		}
		return true
	}
	if b.k == tyOr {
		for _, t := range b.items {
			if isSubtype(a, t) {
				return true
			}
		}
		return false
	}
	hit := unwrap(intersect(a, b))
	if isNone(hit) {
		return false
	}
	return sameType(hit, a)
}

func argFits(got, domain Type) bool {
	if got.k == tyDyn {
		return overlaps(got, domain)
	}
	return isSubtype(got, domain)
}

func numType() Type { return tOr([]Type{IntType(), FloatType()}) }

func stringyType() Type { return tOr([]Type{StringType(), UnknownString()}) }

func pathWant() Type {
	key := stringyType()
	return tOr([]Type{key, EmptyList(), tList(key), Tuple(key)})
}

func listyType() Type {
	return tOr([]Type{EmptyList(), tList(Any()), Tuple(Any())})
}

func mapyType() Type {
	anyT := Any()
	return tOr([]Type{EmptyMapType(), tMap(nil, &anyT)})
}

type pathKeysResult struct {
	unknown bool
	none    bool
	keys    []string
}

func pathKeys(t Type) pathKeysResult {
	t = unwrap(t)
	if t.k == tyStr && t.has {
		return pathKeysResult{keys: []string{t.s}}
	}
	if t.k == tyUStr || t.k == tyStr {
		return pathKeysResult{unknown: true}
	}
	if t.k == tyTuple {
		var keys []string
		for _, item := range t.items {
			u := unwrap(item)
			if u.k == tyStr && u.has {
				keys = append(keys, u.s)
			} else {
				return pathKeysResult{unknown: true}
			}
		}
		if len(keys) == 0 {
			return pathKeysResult{none: true}
		}
		return pathKeysResult{keys: keys}
	}
	if t.k == tyList {
		return pathKeysResult{unknown: true}
	}
	if t.k == tyEmptyList {
		return pathKeysResult{none: true}
	}
	return pathKeysResult{none: true}
}

func typeMapGet(m Type, keys []string) Type {
	cur := unwrap(m)
	for _, k := range keys {
		if cur.k == tyEmptyMap {
			return NilType()
		}
		if cur.k == tyMap {
			if t, ok := cur.field(k); ok {
				cur = unwrap(t)
			} else if cur.rest != nil {
				cur = unwrap(*cur.rest)
			} else {
				return NilType()
			}
			continue
		}
		return tDyn(Any())
	}
	return cur
}

func typeMapSet(m Type, keys []string, val Type) Type {
	var setAt func(cur Type, i int) Type
	setAt = func(cur Type, i int) Type {
		if i == len(keys) {
			return val
		}
		k := keys[i]
		inner := unwrap(cur)
		var fields []mapField
		var rest *Type
		if inner.k == tyMap {
			fields = append([]mapField{}, inner.fields...)
			rest = inner.rest
		} else if inner.k != tyEmptyMap && inner.k != tyNil {
			anyT := Any()
			return tDyn(tMap(nil, &anyT))
		}
		var child Type
		found := false
		for _, f := range fields {
			if f.name == k {
				child = f.t
				found = true
				break
			}
		}
		if !found {
			if rest != nil {
				child = *rest
			} else {
				child = EmptyMapType()
			}
		}
		if isNone(child) || unwrap(child).k == tyNil {
			child = EmptyMapType()
		}
		next := setAt(child, i+1)
		leafNil := i == len(keys)-1 && unwrap(val).k == tyNil && val.k != tyDyn
		var nf []mapField
		if leafNil {
			for _, f := range fields {
				if f.name != k {
					nf = append(nf, f)
				}
			}
		} else {
			replaced := false
			for _, f := range fields {
				if f.name == k {
					nf = append(nf, mapField{name: k, t: next})
					replaced = true
				} else {
					nf = append(nf, f)
				}
			}
			if !replaced {
				nf = append(nf, mapField{name: k, t: next})
			}
		}
		if len(nf) == 0 && rest == nil {
			return EmptyMapType()
		}
		return tMap(nf, rest)
	}
	return setAt(m, 0)
}

func propLeafType(writes []Type, rest []string) Type {
	if len(rest) == 0 {
		if len(writes) == 0 {
			return NilType()
		}
		return tDyn(tOr(append(append([]Type{}, writes...), NilType())))
	}
	if len(writes) == 0 {
		return NilType()
	}
	got := make([]Type, len(writes))
	for i, w := range writes {
		got[i] = typeMapGet(w, rest)
	}
	return tDyn(tOr(append(got, NilType())))
}

func propWriteType(writes []Type, rest []string, val Type) Type {
	if len(rest) == 0 {
		return val
	}
	bases := writes
	if len(bases) == 0 {
		bases = []Type{EmptyMapType()}
	}
	out := make([]Type, len(bases))
	for i, b := range bases {
		out[i] = typeMapSet(b, rest, val)
	}
	return tOr(out)
}

func withoutFalsy(t Type) Type {
	t = unwrap(t)
	if t.k == tyNil || (t.k == tyBool && t.has && !t.b) {
		return None()
	}
	if t.k == tyBool && !t.has {
		return TrueType()
	}
	if t.k == tyOr {
		out := make([]Type, len(t.items))
		for i, x := range t.items {
			out[i] = withoutFalsy(x)
		}
		return tOr(out)
	}
	return t
}

func onlyFalsy(t Type) Type {
	return tOr([]Type{intersect(t, NilType()), intersect(t, FalseType())})
}

func kindOf(v runtime.Value) Type {
	switch v.Kind() {
	case runtime.KindInt:
		return IntType()
	case runtime.KindFloat:
		return FloatType()
	case runtime.KindString:
		return ExactString(v.Text())
	case runtime.KindSymbol:
		return ExactSymbol(v.Name())
	case runtime.KindList:
		if v.IsVec() {
			var items []Type
			for _, x := range v.Items() {
				if x.Kind() == runtime.KindComment {
					continue
				}
				items = append(items, kindOf(x))
			}
			if len(items) == 0 {
				return EmptyList()
			}
			return tTuple(items)
		}
		return Any()
	case runtime.KindMap:
		if len(v.Pairs()) == 0 {
			return EmptyMapType()
		}
		var fields []mapField
		for _, pair := range v.Pairs() {
			fields = append(fields, mapField{name: pair.Key, t: kindOf(pair.Value)})
		}
		return tMap(fields, nil)
	case runtime.KindFn:
		return FnType()
	case runtime.KindNative:
		nv, ok := v.Native()
		if !ok {
			return Any()
		}
		if nv == nil {
			return nativeOf(nil)
		}
		return nativeOf(reflect.TypeOf(nv))
	case runtime.KindComment:
		return NilType()
	case runtime.KindQuote:
		return kindOf(v.Inner())
	default:
		return Any()
	}
}

func collectAliases(v runtime.Value, into map[string]string) {
	switch v.Kind() {
	case runtime.KindQuote, runtime.KindUnquote, runtime.KindSplice:
		collectAliases(v.Inner(), into)
	case runtime.KindMap:
		for _, pair := range v.Pairs() {
			val := pair.Value
			if val.Kind() == runtime.KindSymbol && val.Name() != "true" && val.Name() != "false" && val.Name() != "nil" {
				into[pair.Key] = val.Name()
			} else {
				collectAliases(val, into)
			}
		}
	case runtime.KindList:
		for _, x := range v.Items() {
			collectAliases(x, into)
		}
	}
}

// Alias is a named type for display and host domain types.
type Alias struct {
	Name    string
	Type    Type
	Members []string
}
