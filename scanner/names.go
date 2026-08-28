package scanner

import "slices"

// IsKeyword reports whether name is a special form.
func IsKeyword(name string) bool {
	_, ok := keywords[name]
	return ok
}

// Words returns keywords and builtins, sorted, for completion.
func Words() []string {
	out := make([]string, 0, len(keywords)+len(builtins))
	for k := range keywords {
		out = append(out, k)
	}
	for k := range builtins {
		if _, ok := keywords[k]; ok {
			continue
		}
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// IsBuiltin reports whether name is a core builtin (including true/false/nil).
func IsBuiltin(name string) bool {
	_, ok := builtins[name]
	return ok
}

// IsCoreBuiltin reports whether name is a callable core builtin.
// true, false, and nil are builtins but not callable.
func IsCoreBuiltin(name string) bool {
	if name == "true" || name == "false" || name == "nil" {
		return false
	}
	return IsBuiltin(name)
}

var keywords = map[string]struct{}{
	"def":    {},
	"fn":     {},
	"let":    {},
	"if":     {},
	"else":   {},
	"and":    {},
	"or":     {},
	"not":    {},
	"on":     {},
	"after":  {},
	"pipe":   {},
	"eval":   {},
	"defm":   {},
	"let!":   {},
	"import": {},
	".":      {},
}

var builtins = map[string]struct{}{
	"+": {}, "-": {}, "*": {}, "/": {}, "mod": {},
	"abs": {}, "min": {}, "max": {}, "floor": {}, "ceil": {},
	"=": {}, "!=": {}, "<": {}, ">": {}, "<=": {}, ">=": {},
	"str": {}, "symbol": {}, "len": {},
	"cons": {}, "head": {}, "tail": {}, "nth": {}, "append": {},
	"list-map": {}, "list-filter": {}, "list-reduce": {},
	"map-get": {}, "map-set": {}, "map-update": {}, "map-merge": {},
	"map-to-list": {}, "list-to-map": {}, "map-keys": {}, "map-vals": {},
	"empty?": {}, "list?": {}, "map?": {}, "num?": {}, "int?": {}, "float?": {},
	"str?": {}, "symbol?": {}, "bool?": {}, "nil?": {},
	"prop-get": {}, "prop-set": {}, "prop-update": {},
	"true": {}, "false": {}, "nil": {},
}
