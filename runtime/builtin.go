package runtime

import (
	"math"
	"math/big"
)

func posArgs(name string, call callParts) ([]Value, error) {
	if len(call.keys) > 0 {
		return nil, errf("%s does not take keyword arguments", name)
	}
	return call.pos, nil
}

func asFloat(v Value, ctx string) (float64, error) {
	f, ok := v.AsFloat64()
	if !ok {
		return 0, errf("%s needs a number", ctx)
	}
	return f, nil
}

func asName(v Value) (string, error) {
	if v.k == KindSymbol {
		if v.isKeySym() {
			return v.keyName(), nil
		}
		return v.Name(), nil
	}
	return "", errMsg("expected a symbol")
}

func asPath(v Value, ctx string) ([]string, error) {
	if v.k == KindList {
		if len(v.Items()) == 0 {
			return nil, errf("%s needs a key", ctx)
		}
		out := make([]string, len(v.Items()))
		for i, x := range v.Items() {
			s, err := asName(x)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	s, err := asName(v)
	if err != nil {
		return nil, errf("%s needs a symbol", ctx)
	}
	return []string{s}, nil
}

func allInt(args []Value) bool {
	for _, a := range args {
		if a.k != KindInt {
			return false
		}
	}
	return true
}

func allSmallInt(args []Value) bool {
	for _, a := range args {
		if a.k != KindInt || a.bigInt() != nil {
			return false
		}
	}
	return true
}

func addSmall(args []Value) (Value, bool) {
	if !allSmallInt(args) {
		return Value{}, false
	}
	var sum int64
	for _, a := range args {
		n := a.n
		if n > 0 && sum > maxInt64-n {
			return Value{}, false
		}
		if n < 0 && sum < minInt64-n {
			return Value{}, false
		}
		sum += n
	}
	return Int64(sum), true
}

func mulSmall(args []Value) (Value, bool) {
	if !allSmallInt(args) {
		return Value{}, false
	}
	prod := int64(1)
	for _, a := range args {
		n := a.n
		if n != 0 && prod != 0 {
			if prod > 0 {
				if n > 0 && prod > maxInt64/n {
					return Value{}, false
				}
				if n < 0 && n < minInt64/prod {
					return Value{}, false
				}
			} else {
				if n > 0 && prod < minInt64/n {
					return Value{}, false
				}
				if n < 0 && prod != 0 && n < maxInt64/prod {
					return Value{}, false
				}
			}
		}
		prod *= n
	}
	return Int64(prod), true
}

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)

func numsAsFloat(args []Value, name string) ([]float64, error) {
	out := make([]float64, len(args))
	for i, a := range args {
		f, err := asFloat(a, name)
		if err != nil {
			return nil, err
		}
		out[i] = f
	}
	return out, nil
}

func callBuiltin(name string, call callParts, c *ctx) (Value, error) {
	args, err := posArgs(name, call)
	if err != nil {
		return Value{}, err
	}
	switch name {
	case "+":
		if len(args) == 0 {
			return Int64(0), nil
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("%s needs a number", name)
			}
		}
		if allInt(args) {
			if v, ok := addSmall(args); ok {
				return v, nil
			}
			sum := new(big.Int)
			for _, a := range args {
				sum.Add(sum, a.asBig())
			}
			return Int(sum), nil
		}
		var sum float64
		for _, a := range args {
			f, _ := a.AsFloat64()
			sum += f
		}
		return Float(sum), nil
	case "*":
		if len(args) == 0 {
			return Int64(1), nil
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("%s needs a number", name)
			}
		}
		if allInt(args) {
			if v, ok := mulSmall(args); ok {
				return v, nil
			}
			prod := big.NewInt(1)
			for _, a := range args {
				prod.Mul(prod, a.asBig())
			}
			return Int(prod), nil
		}
		prod := 1.0
		for _, a := range args {
			f, _ := a.AsFloat64()
			prod *= f
		}
		return Float(prod), nil
	case "-":
		if len(args) == 0 {
			return Value{}, errf("%s needs a number", name)
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("%s needs a number", name)
			}
		}
		if len(args) == 1 {
			if args[0].k == KindInt {
				if args[0].bigInt() == nil && args[0].n != minInt64 {
					return Int64(-args[0].n), nil
				}
				return Int(new(big.Int).Neg(args[0].asBig())), nil
			}
			f, _ := args[0].AsFloat64()
			return Float(-f), nil
		}
		if allInt(args) {
			if allSmallInt(args) {
				acc := args[0].n
				ok := true
				for _, a := range args[1:] {
					n := a.n
					if n < 0 && acc > maxInt64+n {
						ok = false
						break
					}
					if n > 0 && acc < minInt64+n {
						ok = false
						break
					}
					acc -= n
				}
				if ok {
					return Int64(acc), nil
				}
			}
			acc := new(big.Int).Set(args[0].asBig())
			for _, a := range args[1:] {
				acc.Sub(acc, a.asBig())
			}
			return Int(acc), nil
		}
		acc, _ := args[0].AsFloat64()
		for _, a := range args[1:] {
			f, _ := a.AsFloat64()
			acc -= f
		}
		return Float(acc), nil
	case "/":
		if len(args) == 0 {
			return Value{}, errf("/ needs a number")
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("/ needs a number")
			}
		}
		if len(args) == 1 {
			return args[0], nil
		}
		if allInt(args) {
			acc := new(big.Int).Set(args[0].asBig())
			even := true
			for _, a := range args[1:] {
				d := a.asBig()
				if d.Sign() == 0 {
					return Value{}, errMsg("division by zero")
				}
				mod := new(big.Int)
				quo, rem := new(big.Int).QuoRem(acc, d, mod)
				if rem.Sign() != 0 {
					even = false
					break
				}
				acc = quo
			}
			if even {
				return Int(acc), nil
			}
		}
		acc, _ := args[0].AsFloat64()
		for _, a := range args[1:] {
			f, _ := a.AsFloat64()
			if f == 0 {
				return Value{}, errMsg("division by zero")
			}
			acc /= f
		}
		return Float(acc), nil
	case "mod":
		if len(args) < 2 {
			return Value{}, errMsg("mod needs two integers")
		}
		if args[0].k != KindInt || args[1].k != KindInt {
			return Value{}, errMsg("mod needs integers")
		}
		if args[1].asBig().Sign() == 0 {
			return Value{}, errMsg("division by zero")
		}
		return Int(new(big.Int).Mod(args[0].asBig(), args[1].asBig())), nil
	case "abs":
		if len(args) == 0 || !args[0].IsNum() {
			return Value{}, errf("%s needs a number", name)
		}
		if args[0].k == KindInt {
			return Int(new(big.Int).Abs(args[0].asBig())), nil
		}
		return Float(math.Abs(args[0].floatVal())), nil
	case "min":
		if len(args) == 0 {
			return Value{}, errf("%s needs a number", name)
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("%s needs a number", name)
			}
		}
		if allInt(args) {
			m := args[0].asBig()
			best := new(big.Int).Set(m)
			for _, a := range args[1:] {
				if a.asBig().Cmp(best) < 0 {
					best.Set(a.asBig())
				}
			}
			return Int(best), nil
		}
		fs, err := numsAsFloat(args, name)
		if err != nil {
			return Value{}, err
		}
		m := fs[0]
		for _, f := range fs[1:] {
			if f < m {
				m = f
			}
		}
		return Float(m), nil
	case "max":
		if len(args) == 0 {
			return Value{}, errf("%s needs a number", name)
		}
		for _, a := range args {
			if !a.IsNum() {
				return Value{}, errf("%s needs a number", name)
			}
		}
		if allInt(args) {
			best := new(big.Int).Set(args[0].asBig())
			for _, a := range args[1:] {
				if a.asBig().Cmp(best) > 0 {
					best.Set(a.asBig())
				}
			}
			return Int(best), nil
		}
		fs, err := numsAsFloat(args, name)
		if err != nil {
			return Value{}, err
		}
		m := fs[0]
		for _, f := range fs[1:] {
			if f > m {
				m = f
			}
		}
		return Float(m), nil
	case "floor":
		if len(args) == 0 || !args[0].IsNum() {
			return Value{}, errf("%s needs a number", name)
		}
		if args[0].k == KindInt {
			return args[0], nil
		}
		return Float(math.Floor(args[0].floatVal())), nil
	case "ceil":
		if len(args) == 0 || !args[0].IsNum() {
			return Value{}, errf("%s needs a number", name)
		}
		if args[0].k == KindInt {
			return args[0], nil
		}
		return Float(math.Ceil(args[0].floatVal())), nil
	case "=":
		if len(args) < 2 {
			return Bool(false), nil
		}
		return Bool(args[0].Equal(args[1])), nil
	case "!=":
		if len(args) < 2 {
			return Bool(true), nil
		}
		return Bool(!args[0].Equal(args[1])), nil
	case "<", ">", "<=", ">=":
		if len(args) < 2 {
			return Value{}, errf("%s needs two numbers", name)
		}
		if !args[0].IsNum() || !args[1].IsNum() {
			return Value{}, errf("%s needs a number", name)
		}
		cmp, err := cmpNum(args[0], args[1])
		if err != nil {
			return Value{}, err
		}
		switch name {
		case "<":
			return Bool(cmp < 0), nil
		case ">":
			return Bool(cmp > 0), nil
		case "<=":
			return Bool(cmp <= 0), nil
		default:
			return Bool(cmp >= 0), nil
		}
	case "str":
		var b []byte
		for _, a := range args {
			b = append(b, printVal(a)...)
		}
		return String(string(b)), nil
	case "len":
		if len(args) == 0 {
			return Value{}, errMsg("len needs a string, a list, or a map")
		}
		a := args[0]
		switch a.k {
		case KindString:
			return Int64(int64(len(a.s))), nil
		case KindList:
			return Int64(int64(len(a.Items()))), nil
		case KindMap:
			return Int64(int64(a.mapData().len())), nil
		default:
			return Value{}, errMsg("len needs a string, a list, or a map")
		}
	case "cons":
		if len(args) == 0 {
			return CallList(Nil), nil
		}
		x := args[0]
		if len(args) > 1 && args[1].k == KindList {
			out := make([]Value, 1+len(args[1].Items()))
			out[0] = x
			copy(out[1:], args[1].Items())
			return CallList(out...), nil
		}
		y := Nil
		if len(args) > 1 {
			y = args[1]
		}
		return CallList(x, y), nil
	case "head":
		if len(args) > 0 && args[0].k == KindList && len(args[0].Items()) > 0 {
			return args[0].Items()[0], nil
		}
		return Nil, nil
	case "tail":
		if len(args) > 0 && args[0].k == KindList {
			return CallList(args[0].Items()[1:]...), nil
		}
		return Nil, nil
	case "nth":
		i := 0
		if len(args) > 1 {
			if !args[1].IsNum() {
				return Value{}, errMsg("nth needs a number")
			}
			f, _ := args[1].AsFloat64()
			i = int(math.Floor(f))
		}
		if len(args) > 0 && args[0].k == KindList && i >= 0 && i < len(args[0].Items()) {
			return args[0].Items()[i], nil
		}
		return Nil, nil
	case "append":
		var out []Value
		for _, a := range args {
			if a.k == KindList {
				out = append(out, a.Items()...)
			} else {
				out = append(out, a)
			}
		}
		return CallList(out...), nil
	case "list-map":
		if len(args) < 2 {
			return Value{}, errMsg("list-map needs a list or map, and a function")
		}
		seq, err := asSeq(args[0], "list-map")
		if err != nil {
			return Value{}, err
		}
		f := args[1]
		out := make([]Value, len(seq))
		for i, item := range seq {
			out[i], err = callPos(f, []Value{item}, c)
			if err != nil {
				return Value{}, err
			}
		}
		return List(out...), nil
	case "list-filter":
		if len(args) < 2 {
			return Value{}, errMsg("list-filter needs a list or map, and a function")
		}
		seq, err := asSeq(args[0], "list-filter")
		if err != nil {
			return Value{}, err
		}
		f := args[1]
		var out []Value
		for _, item := range seq {
			okv, err := callPos(f, []Value{item}, c)
			if err != nil {
				return Value{}, err
			}
			if okv.Truthy() {
				out = append(out, item)
			}
		}
		return List(out...), nil
	case "list-reduce":
		if len(args) < 3 {
			return Value{}, errMsg("list-reduce needs a list or map, an init, and a function")
		}
		seq, err := asSeq(args[0], "list-reduce")
		if err != nil {
			return Value{}, err
		}
		acc := args[1]
		f := args[2]
		for _, item := range seq {
			acc, err = callPos(f, []Value{acc, item}, c)
			if err != nil {
				return Value{}, err
			}
		}
		return acc, nil
	case "map-to-list":
		if len(args) == 0 || args[0].k != KindMap {
			return Value{}, errMsg("map-to-list needs a map")
		}
		var out []Value
		if args[0].mapData() != nil {
			for i, k := range args[0].mapData().keys {
				out = append(out, List(k, args[0].mapData().vals[i]))
			}
		}
		return List(out...), nil
	case "list-to-map":
		if len(args) == 0 || args[0].k != KindList {
			return Value{}, errMsg("list-to-map needs a list")
		}
		m := newMap()
		for _, p := range args[0].Items() {
			k, val, err := asPair(p, "list-to-map")
			if err != nil {
				return Value{}, err
			}
			m.put(k, val)
		}
		return Value{k: KindMap, p: m}, nil
	case "map-keys":
		if len(args) == 0 || args[0].k != KindMap {
			return Value{}, errMsg("map-keys needs a map")
		}
		var out []Value
		if args[0].mapData() != nil {
			out = append(out, args[0].mapData().keys...)
		}
		return List(out...), nil
	case "map-vals":
		if len(args) == 0 || args[0].k != KindMap {
			return Value{}, errMsg("map-vals needs a map")
		}
		var out []Value
		if args[0].mapData() != nil {
			out = append(out, args[0].mapData().vals...)
		}
		return List(out...), nil
	case "empty?":
		a := Nil
		if len(args) > 0 {
			a = args[0]
		}
		return Bool(a.IsNil() ||
			(a.k == KindList && len(a.Items()) == 0) ||
			(a.k == KindMap && a.mapData().len() == 0) ||
			(a.k == KindString && a.s == "")), nil
	case "list?":
		return Bool(len(args) > 0 && args[0].k == KindList), nil
	case "map?":
		return Bool(len(args) > 0 && args[0].k == KindMap), nil
	case "num?":
		return Bool(len(args) > 0 && args[0].IsNum()), nil
	case "int?":
		return Bool(len(args) > 0 && args[0].k == KindInt), nil
	case "float?":
		return Bool(len(args) > 0 && args[0].k == KindFloat), nil
	case "str?":
		return Bool(len(args) > 0 && args[0].k == KindString), nil
	case "bool?":
		return Bool(len(args) > 0 && args[0].IsBool()), nil
	case "nil?":
		return Bool(len(args) > 0 && args[0].IsNil()), nil
	case "symbol?":
		return Bool(len(args) > 0 && args[0].k == KindSymbol), nil
	case "symbol":
		if len(args) == 0 {
			return Value{}, errMsg("symbol needs a string")
		}
		a := args[0]
		if a.k == KindSymbol {
			return Symbol(a.Name()), nil
		}
		if a.k == KindString {
			return Symbol(a.s), nil
		}
		return Value{}, errMsg("symbol needs a string")
	case "map-get":
		if len(args) < 2 {
			return Value{}, errMsg("map-get needs a map and a key")
		}
		if args[0].k != KindMap {
			return Value{}, errMsg("map-get needs a map")
		}
		path, err := asPath(args[1], "map-get")
		if err != nil {
			return Value{}, err
		}
		return mapGetPath(args[0], path, "map-get")
	case "map-set":
		if len(args) < 3 {
			return Value{}, errMsg("map-set needs a map, a key, and a value")
		}
		if args[0].k != KindMap {
			return Value{}, errMsg("map-set needs a map")
		}
		path, err := asPath(args[1], "map-set")
		if err != nil {
			return Value{}, err
		}
		return mapSetPath(args[0], path, args[2], "map-set")
	case "map-update":
		if len(args) < 3 {
			return Value{}, errMsg("map-update needs a map, a key, and a function")
		}
		if args[0].k != KindMap {
			return Value{}, errMsg("map-update needs a map")
		}
		path, err := asPath(args[1], "map-update")
		if err != nil {
			return Value{}, err
		}
		cur, err := mapGetPath(args[0], path, "map-update")
		if err != nil {
			return Value{}, err
		}
		next, err := callPos(args[2], []Value{cur}, c)
		if err != nil {
			return Value{}, err
		}
		return mapSetPath(args[0], path, next, "map-update")
	case "prop-get":
		key := Nil
		if len(args) > 0 {
			key = args[0]
		}
		path, err := asPath(key, "prop-get")
		if err != nil {
			return Value{}, err
		}
		return getPropPath(c.rt, path, "prop-get")
	case "prop-set":
		key := Nil
		if len(args) > 0 {
			key = args[0]
		}
		val := Nil
		if len(args) > 1 {
			val = args[1]
		}
		path, err := asPath(key, "prop-set")
		if err != nil {
			return Value{}, err
		}
		return setPropPath(c.rt, path, val, "prop-set")
	case "prop-update":
		if len(args) < 2 {
			return Value{}, errMsg("prop-update needs a name and a function")
		}
		path, err := asPath(args[0], "prop-update")
		if err != nil {
			return Value{}, err
		}
		cur, err := getPropPath(c.rt, path, "prop-update")
		if err != nil {
			return Value{}, err
		}
		next, err := callPos(args[1], []Value{cur}, c)
		if err != nil {
			return Value{}, err
		}
		if _, err := setPropPath(c.rt, path, next, "prop-update"); err != nil {
			return Value{}, err
		}
		return next, nil
	case "map-merge":
		m := newMap()
		for _, a := range args {
			if a.k != KindMap {
				return Value{}, errMsg("map-merge needs maps")
			}
			if a.mapData() == nil {
				continue
			}
			for i, k := range a.mapData().keys {
				m.put(k, a.mapData().vals[i])
			}
		}
		return Value{k: KindMap, p: m}, nil
	default:
		if c.rt != nil {
			if b, ok := c.rt.extra[name]; ok {
				if len(call.keys) > 0 {
					return Value{}, errf("%s does not take keyword arguments", name)
				}
				return c.rt.hostCall(func() (Value, error) { return b(args) })
			}
		}
		return Value{}, errf("unknown function: %s", name)
	}
}

func cmpNum(a, b Value) (int, error) {
	if a.k == KindInt && b.k == KindInt {
		return a.asBig().Cmp(b.asBig()), nil
	}
	fa, ok := a.AsFloat64()
	if !ok {
		return 0, errMsg("comparison needs a number")
	}
	fb, ok := b.AsFloat64()
	if !ok {
		return 0, errMsg("comparison needs a number")
	}
	switch {
	case fa < fb:
		return -1, nil
	case fa > fb:
		return 1, nil
	default:
		return 0, nil
	}
}

func asSeq(v Value, ctx string) ([]Value, error) {
	if v.k == KindList {
		return v.Items(), nil
	}
	if v.k == KindMap {
		if v.mapData() == nil {
			return nil, nil
		}
		out := make([]Value, len(v.mapData().keys))
		for i, k := range v.mapData().keys {
			out[i] = List(k, v.mapData().vals[i])
		}
		return out, nil
	}
	return nil, errf("%s needs a list or a map", ctx)
}

func asPair(v Value, ctx string) (Value, Value, error) {
	if v.k != KindList || len(v.Items()) != 2 {
		return Value{}, Value{}, errf("%s needs [key value] pairs", ctx)
	}
	k := v.Items()[0]
	if k.k != KindSymbol {
		return Value{}, Value{}, errf("%s needs symbol keys", ctx)
	}
	if k.isKeySym() {
		k = Symbol(k.keyName())
	}
	return k, v.Items()[1], nil
}

func mapGetPath(m Value, path []string, ctx string) (Value, error) {
	cur := m
	for _, k := range path {
		if cur.IsNil() {
			return Nil, nil
		}
		if cur.k != KindMap {
			return Value{}, errf("%s needs a map", ctx)
		}
		v, ok := cur.mapData().get(k)
		if !ok {
			return Nil, nil
		}
		cur = v
	}
	return cur, nil
}

func mapSetPath(m Value, path []string, val Value, ctx string) (Value, error) {
	if m.k != KindMap {
		return Value{}, errf("%s needs a map", ctx)
	}
	var goSet func(cur Value, i int) (Value, error)
	goSet = func(cur Value, i int) (Value, error) {
		if i == len(path) {
			return val, nil
		}
		var base *mapData
		if cur.IsNil() {
			base = newMap()
		} else if cur.k == KindMap {
			base = cur.mapData().clone()
		} else {
			return Value{}, errf("%s: not a map", ctx)
		}
		k := path[i]
		child, _ := base.get(k)
		next, err := goSet(child, i+1)
		if err != nil {
			return Value{}, err
		}
		if next.IsNil() && i == len(path)-1 {
			base.del(k)
		} else {
			base.put(Symbol(k), next)
		}
		return Value{k: KindMap, p: base}, nil
	}
	return goSet(m, 0)
}

func getPropPath(rt *Machine, path []string, ctx string) (Value, error) {
	if rt == nil {
		return Nil, nil
	}
	root := rt.getVar(path[0])
	if len(path) == 1 {
		return root, nil
	}
	if root.IsNil() {
		return Nil, nil
	}
	return mapGetPath(root, path[1:], ctx)
}

func setPropPath(rt *Machine, path []string, val Value, ctx string) (Value, error) {
	if rt == nil {
		return val, nil
	}
	if len(path) == 1 {
		rt.setVar(path[0], val)
		return val, nil
	}
	root := rt.getVar(path[0])
	base := root
	if root.IsNil() {
		base = EmptyMap()
	}
	next, err := mapSetPath(base, path[1:], val, ctx)
	if err != nil {
		return Value{}, err
	}
	rt.setVar(path[0], next)
	return val, nil
}
