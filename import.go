package writ

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func evalImport(args []Value, env *env, c *ctx) (Value, error) {
	if len(args) != 1 {
		return Value{}, errMsg("import needs one path")
	}
	v, err := evalVal(args[0], env, c)
	if err != nil {
		return Value{}, err
	}
	if v.k != KindString {
		return Value{}, errMsg("import needs a string path")
	}
	rt := c.rt
	if rt == nil {
		return Value{}, errMsg("import needs a runtime")
	}
	return rt.loadImport(v.s, c.file)
}

func (rt *Runtime) loadImport(spec, fromFile string) (Value, error) {
	if pkg, ok := rt.pkgs[spec]; ok {
		return packageMap(pkg), nil
	}
	path, kind, err := rt.resolveImport(spec, fromFile)
	if err != nil {
		return Value{}, err
	}
	if kind == "pkg" {
		return packageMap(rt.pkgs[path]), nil
	}
	for _, p := range rt.loading {
		if p == path {
			return Value{}, errf("import cycle: %s", strings.Join(append(rt.loading, path), " -> "))
		}
	}
	if loaded, ok := rt.loaded[path]; ok {
		return loaded.exports, nil
	}
	if kind == "plugin" {
		if rt.checking || !rt.nativePlugins {
			return Value{}, errMsg("native plugins are disabled")
		}
		pkg, err := loadPlugin(path)
		if err != nil {
			return Value{}, err
		}
		exp := packageMap(pkg)
		rt.rememberLoaded(path, &loadedPkg{exports: exp, types: packageType(pkg)})
		return exp, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	rt.loading = append(rt.loading, path)
	defer func() { rt.loading = rt.loading[:len(rt.loading)-1] }()
	prev := rt.file
	rt.file = path
	defer func() { rt.file = prev }()
	forms, err := Parse(string(data))
	if err != nil {
		return Value{}, asError(err).withFile(path)
	}
	prog, err := compileForms(forms, rt)
	if err != nil {
		return Value{}, asError(err).withFile(path)
	}
	env := makeEnv(nil)
	installFns(prog.Fns, env)
	macros := toMacroTable(prog.Macros)
	c := newCtx(rt, env, macros)
	c.file = path
	if _, err := evalForms(prog.Boot, env, c); err != nil {
		return Value{}, asError(err).withFile(path)
	}
	names := map[string]Value{}
	for _, f := range prog.Fns {
		if v, ok := env.get(f.Name); ok {
			names[f.Name] = v
		}
	}
	for _, m := range prog.Macros {
		if _, ok := names[m.Name]; ok {
			continue
		}
		names[m.Name] = makeFnVal(m.Clauses, env)
	}
	exp := mapFromNames(names)
	for i := range prog.Handlers {
		prog.Handlers[i].env = env
	}
	rt.rememberLoaded(path, &loadedPkg{exports: exp, handlers: prog.Handlers, env: env, types: exportMapType(prog)})
	rt.handlers = append(rt.handlers, prog.Handlers...)
	return exp, nil
}

func (rt *Runtime) rememberLoaded(path string, l *loadedPkg) {
	if _, ok := rt.loaded[path]; !ok {
		rt.loadedOrder = append(rt.loadedOrder, path)
	}
	rt.loaded[path] = l
}

func pluginExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".so", ".dylib", ".dll":
		return true
	default:
		return false
	}
}

func (rt *Runtime) importRoots(fromFile string) []string {
	var roots []string
	if fromFile != "" {
		roots = append(roots, filepath.Dir(fromFile))
	}
	roots = append(roots, rt.search...)
	if len(rt.search) == 0 && fromFile == "" {
		if wd, err := os.Getwd(); err == nil {
			roots = append(roots, wd)
		}
	}
	return roots
}

func underRoot(path string, roots []string) bool {
	for _, r := range roots {
		root, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

func (rt *Runtime) resolveImport(spec, fromFile string) (path, kind string, err error) {
	if _, ok := rt.pkgs[spec]; ok {
		return spec, "pkg", nil
	}
	ext := strings.ToLower(filepath.Ext(spec))
	if pluginExt(ext) && !rt.nativePlugins {
		return "", "", errMsg("native plugins are disabled")
	}
	absSpec := filepath.IsAbs(spec)
	if absSpec && !rt.allowAbsolute {
		return "", "", errMsg("absolute import paths are disabled")
	}
	var candidates []string
	if fromFile != "" && !absSpec {
		candidates = append(candidates, filepath.Join(filepath.Dir(fromFile), spec))
	}
	if absSpec {
		candidates = append(candidates, spec)
	} else {
		for _, dir := range rt.search {
			candidates = append(candidates, filepath.Join(dir, spec))
		}
		if len(rt.search) == 0 && fromFile == "" {
			if wd, err := os.Getwd(); err == nil {
				candidates = append(candidates, filepath.Join(wd, spec))
			}
		}
	}
	if ext == "" {
		var more []string
		for _, c := range candidates {
			more = append(more, c+".writ")
			if rt.nativePlugins && pluginsSupported() {
				more = append(more, c+".so", c+".dylib", c+".dll")
			}
		}
		candidates = append(candidates, more...)
	}
	roots := rt.importRoots(fromFile)
	tryFile := func(p string) (string, string, bool) {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return "", "", false
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		e := strings.ToLower(filepath.Ext(abs))
		k := "writ"
		switch {
		case pluginExt(e):
			if !rt.nativePlugins {
				return "", "", false
			}
			k = "plugin"
		case e != ".writ":
			return "", "", false
		}
		if !rt.allowAbsolute && !underRoot(abs, roots) {
			return "", "", false
		}
		return abs, k, true
	}
	for _, c := range candidates {
		if p, k, ok := tryFile(c); ok {
			return p, k, nil
		}
	}
	if pluginExt(ext) {
		return "", "", errf("plugin not found: %s", spec)
	}
	return "", "", errf("cannot find import %q", spec)
}

func packageMap(p Package) Value {
	names := map[string]Value{}
	for name, f := range p.Funcs {
		fn := f
		names[name] = Value{k: KindFn, fn: &fnVal{native: fn, name: name}}
	}
	for name, v := range p.Vals {
		names[name] = v
	}
	return mapFromNames(names)
}

func mapFromNames(names map[string]Value) Value {
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]MapPair, len(keys))
	for i, k := range keys {
		pairs[i] = MapPair{Key: k, Value: names[k]}
	}
	return MapFrom(pairs...)
}

func packageType(p Package) Type {
	var fields []mapField
	keys := make([]string, 0, len(p.Funcs)+len(p.Vals))
	seen := map[string]struct{}{}
	for k := range p.Funcs {
		keys = append(keys, k)
		seen[k] = struct{}{}
	}
	for k := range p.Vals {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := Any()
		if p.Types != nil {
			if tt, ok := p.Types[k]; ok {
				t = tt
			}
		} else if _, ok := p.Funcs[k]; ok {
			t = FnType(PosRestArrow(tDyn(Any())))
		}
		fields = append(fields, mapField{name: k, t: t})
	}
	if len(fields) == 0 {
		return EmptyMapType()
	}
	return tMap(fields, nil)
}

func exportMapType(prog program) Type {
	var fields []mapField
	seen := map[string]struct{}{}
	var names []string
	for _, f := range prog.Fns {
		if _, ok := seen[f.Name]; ok {
			continue
		}
		seen[f.Name] = struct{}{}
		names = append(names, f.Name)
	}
	for _, m := range prog.Macros {
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		names = append(names, m.Name)
	}
	sort.Strings(names)
	for _, n := range names {
		fields = append(fields, mapField{name: n, t: tDyn(Any())})
	}
	if len(fields) == 0 {
		return EmptyMapType()
	}
	return tMap(fields, nil)
}

func (rt *Runtime) importType(spec, fromFile string) (Type, []Diagnostic, error) {
	if pkg, ok := rt.pkgs[spec]; ok {
		return packageType(pkg), nil, nil
	}
	path, kind, err := rt.resolveImport(spec, fromFile)
	if err != nil {
		return Type{}, nil, err
	}
	if kind == "pkg" {
		return packageType(rt.pkgs[path]), nil, nil
	}
	for _, p := range rt.checkLoading {
		if p == path {
			return Type{}, nil, errf("import cycle: %s", spec)
		}
	}
	if rt.exportCache != nil {
		if e, ok := rt.exportCache[path]; ok {
			return e.t, prefixImportDiags(path, e.diags), nil
		}
	}
	if loaded, ok := rt.loaded[path]; ok && loaded.types.k != 0 {
		return loaded.types, nil, nil
	}
	if kind == "plugin" {
		anyT := Any()
		return tDyn(tMap(nil, &anyT)), nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Type{}, nil, err
	}
	prev := rt.file
	rt.file = path
	res := rt.checkSrc(string(data), path)
	rt.file = prev
	diags := prefixImportDiags(path, res.Diagnostics)
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "cycle") {
			return Type{}, diags, errMsg(path + ": " + d.Message)
		}
	}
	if e, ok := rt.exportCache[path]; ok {
		return e.t, diags, nil
	}
	anyT := Any()
	return tDyn(tMap(nil, &anyT)), diags, nil
}

func prefixImportDiags(path string, diags []Diagnostic) []Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]Diagnostic, len(diags))
	for i, d := range diags {
		if path != "" && !strings.HasPrefix(d.Message, path) {
			d.Message = path + ": " + d.Message
		}
		out[i] = d
	}
	return out
}
