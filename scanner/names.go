package scanner

// IsKeyword reports whether name is a special form.
func IsKeyword(name string) bool {
	_, ok := keywords[name]
	return ok
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
}

var builtins = map[string]struct{}{
	"+": {}, "-": {}, "*": {}, "/": {}, "mod": {},
	"abs": {}, "min": {}, "max": {}, "floor": {}, "ceil": {},
	"=": {}, "/=": {}, "<": {}, ">": {}, "<=": {}, ">=": {},
	"str": {}, "symbol": {}, "len": {},
	"cons": {}, "first": {}, "rest": {}, "nth": {}, "append": {},
	"map": {}, "filter": {}, "reduce": {},
	"get": {}, "set": {}, "update": {}, "merge": {},
	"pairs": {}, "from-pairs": {}, "keys": {}, "vals": {},
	"empty?": {}, "list?": {}, "map?": {}, "num?": {}, "int?": {}, "float?": {},
	"str?": {}, "symbol?": {}, "bool?": {}, "nil?": {},
	"get-prop": {}, "set-prop": {}, "update-prop": {},
	"true": {}, "false": {}, "nil": {},
}
