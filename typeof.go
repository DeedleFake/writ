package writ

import (
	"reflect"

	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
)

// TypeOf returns the static type of a runtime value for host APIs
// (RegisterBuiltin / RegisterPackage typing). types does not import runtime.
func TypeOf(v runtime.Value) types.Type {
	switch v.Kind() {
	case runtime.KindInt:
		return types.IntType()
	case runtime.KindFloat:
		return types.FloatType()
	case runtime.KindString:
		return types.StringType()
	case runtime.KindSymbol:
		return types.ExactSymbol(v.Name())
	case runtime.KindList:
		if v.IsVec() {
			var items []types.Type
			for _, x := range v.Items() {
				items = append(items, TypeOf(x))
			}
			if len(items) == 0 {
				return types.EmptyList()
			}
			return types.Tuple(items...)
		}
		return types.Any()
	case runtime.KindMap:
		if len(v.Pairs()) == 0 {
			return types.EmptyMapType()
		}
		fields := make([]types.FnKey, 0, len(v.Pairs()))
		for _, pair := range v.Pairs() {
			fields = append(fields, types.FnKey{Name: pair.Key.Name(), Type: TypeOf(pair.Value)})
		}
		return types.MapType(fields, nil)
	case runtime.KindFn:
		return types.FnType()
	case runtime.KindMacro:
		return types.MacroType()
	case runtime.KindNative:
		nv, ok := v.Native()
		if !ok {
			return types.Any()
		}
		if nv == nil {
			return types.OpaqueType()
		}
		return types.NativeOf(reflect.TypeOf(nv))
	case runtime.KindSyntax:
		return types.Any()
	default:
		return types.Any()
	}
}
