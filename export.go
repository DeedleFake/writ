package writ

// Fn wraps a native function as an untyped export Value.
func Fn(f Func) Value {
	return Value{k: KindFn, p: &fnVal{native: f}}
}

// TypedFn wraps a native function with a declared export type.
func TypedFn(f Func, t Type) Value {
	return WithType(Fn(f), t)
}

// Mac wraps a native macro as an untyped export Value.
func Mac(m Macro) Value {
	return Value{k: KindMacro, p: &fnVal{macro: m}}
}

// TypedMac wraps a native macro with a declared export type.
func TypedMac(m Macro, t Type) Value {
	return WithType(Mac(m), t)
}

// Typed attaches a declared type to any value (typically a package val export).
func Typed(v Value, t Type) Value {
	return WithType(v, t)
}

// WithType returns a copy of v with a declared type.
// A non-nil typ pointer marks the value typed even when t is a zero/empty FnType.
func WithType(v Value, t Type) Value {
	cp := t
	v.typ = &cp
	return v
}
