package json

import (
	"encoding/json"
	"io"
	"math"
	"strings"

	"deedles.dev/writ/runtime"
)

func decode(s string) (runtime.Value, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return runtime.Value{}, runtime.Errorf("json: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return runtime.Value{}, runtime.Errorf("json: %v", err)
		}
		return runtime.Value{}, runtime.Errorf("json: trailing data after value")
	}
	return fromJSON(raw)
}

func fromJSON(v any) (runtime.Value, error) {
	switch x := v.(type) {
	case nil:
		return runtime.Nil, nil
	case bool:
		return runtime.Bool(x), nil
	case string:
		return runtime.String(x), nil
	case json.Number:
		return fromNumber(x)
	case []any:
		xs := make([]runtime.Value, len(x))
		for i, e := range x {
			ev, err := fromJSON(e)
			if err != nil {
				return runtime.Value{}, err
			}
			xs[i] = ev
		}
		return runtime.List(xs...), nil
	case map[string]any:
		pairs := make([]runtime.MapPair, 0, len(x))
		for k, e := range x {
			ev, err := fromJSON(e)
			if err != nil {
				return runtime.Value{}, err
			}
			pairs = append(pairs, runtime.MapPair{Key: runtime.Symbol(k), Value: ev})
		}
		return runtime.MapFrom(pairs...), nil
	default:
		return runtime.Value{}, runtime.Errorf("json: unexpected %T", v)
	}
}

func fromNumber(n json.Number) (runtime.Value, error) {
	s := string(n)
	if isIntegerSpelling(s) {
		if v, ok := runtime.ParseInt(s); ok {
			return v, nil
		}
	}
	f, err := n.Float64()
	if err != nil {
		return runtime.Value{}, runtime.Errorf("json: %v", err)
	}
	return runtime.Float(f), nil
}

// isIntegerSpelling reports optional "-" followed by digits only.
func isIntegerSpelling(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '-' {
		s = s[1:]
		if s == "" {
			return false
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func encode(v runtime.Value) ([]byte, error) {
	raw, err := toJSON(v)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, runtime.Errorf("json: %v", err)
	}
	return b, nil
}

func toJSON(v runtime.Value) (any, error) {
	switch v.Kind() {
	case runtime.KindSymbol:
		switch {
		case v.IsNil():
			return nil, nil
		case v.IsTrue():
			return true, nil
		case v.IsFalse():
			return false, nil
		default:
			return nil, runtime.Errorf("json: cannot encode symbol %s", v.Name())
		}
	case runtime.KindString:
		return v.Text(), nil
	case runtime.KindInt:
		return json.Number(v.BigInt().String()), nil
	case runtime.KindFloat:
		f := v.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, runtime.Errorf("json: cannot encode non-finite float")
		}
		return f, nil
	case runtime.KindList:
		xs := v.Items()
		out := make([]any, len(xs))
		for i, x := range xs {
			e, err := toJSON(x)
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	case runtime.KindMap:
		m := make(map[string]any, len(v.Pairs()))
		for _, p := range v.Pairs() {
			e, err := toJSON(p.Value)
			if err != nil {
				return nil, err
			}
			m[p.Key.Name()] = e
		}
		return m, nil
	case runtime.KindFn, runtime.KindMacro, runtime.KindNative, runtime.KindSyntax:
		return nil, runtime.Errorf("json: cannot encode %s", v.Kind())
	default:
		return nil, runtime.Errorf("json: cannot encode %s", v.Kind())
	}
}
