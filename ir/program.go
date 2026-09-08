package ir

import "deedles.dev/writ/syntax"

// Pattern is one parameter or literal match.
// When Bind is false, Lit is the literal form to match.
type Pattern struct {
	Bind bool
	Name string
	Lit  syntax.Form
}

// Params is a function, macro, or handler parameter list.
type Params struct {
	Key  bool
	Pats []Pattern
	Keys []KeyPat
	Rest string
}

// KeyPat is one keyword parameter.
type KeyPat struct {
	Name string
	Pat  Pattern
}

// Clause is one function, macro, or handler clause.
type Clause struct {
	Params     Params
	Body       []syntax.Form
	ParamsForm *syntax.Form
}

// NamedFn is a top-level def or defm.
type NamedFn struct {
	Name     string
	Clauses  []Clause
	NameForm syntax.Form
}

// Handler is a compiled (on ...) form.
type Handler struct {
	Event   string
	Clauses []Clause
}

// NamedImport is a top-level keyed (import name: path ...).
type NamedImport struct {
	Name     string
	PathForm syntax.Form
	NameForm syntax.Form
}

// Program is expanded top-level forms.
type Program struct {
	Handlers []Handler
	Boot     []syntax.Form
	Fns      []NamedFn
	Macros   []NamedFn
	Imports  []NamedImport
}

// DefHead is a parsed (def ...) or (defm ...) head.
type DefHead struct {
	Name       string
	NameForm   syntax.Form
	Params     Params
	ParamsForm syntax.Form
	Body       []syntax.Form
	HeadForm   syntax.Form
}

// IfClause is one branch of (if ...).
type IfClause struct {
	Test *syntax.Form
	Not  bool
	Body []syntax.Form
}

// Exported reports whether a top-level def/defm name is in a module export map.
// Names that start with '-' are private to the defining script.
func Exported(name string) bool {
	return name != "" && name[0] != '-'
}
