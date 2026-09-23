package writ

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"deedles.dev/writ/parser"
	"deedles.dev/writ/syntax"
)

func (rt *Runtime) loadImport(spec, fromFile string) (Value, error) {
	if pkg, ok := rt.pkgs[spec]; ok {
		return PackageValue(pkg), nil
	}
	path, kind, err := rt.resolveImport(spec, fromFile)
	if err != nil {
		return Value{}, err
	}
	if kind == "pkg" {
		return PackageValue(rt.pkgs[path]), nil
	}
	if rt.m.LoadingCycle(path) {
		return Value{}, syntax.Errorf("import cycle: %s", strings.Join(append(rt.m.LoadingPath(), path), " -> "))
	}
	if exp, ok := rt.m.Loaded(path); ok {
		return exp, nil
	}
	if kind == "wasm" {
		pkg, err := LoadWasm(path)
		if err != nil {
			return Value{}, err
		}
		rt.m.RememberPackage(path, pkg)
		return PackageValue(pkg), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return Value{}, err
	}
	defer f.Close()
	rt.m.PushLoading(path)
	defer rt.m.PopLoading()
	prev := rt.m.File()
	rt.m.SetFile(path)
	defer rt.m.SetFile(prev)
	forms, err := parser.Parse(f)
	if err != nil {
		return Value{}, parseErr(err, path)
	}
	return rt.m.EvalModule(path, forms)
}

func wasmExt(ext string) bool {
	return strings.ToLower(ext) == ".wasm"
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
	if ext == "" {
		return "", "", syntax.Errorf("import %q needs a .writ or .wasm suffix", spec)
	}
	absSpec := filepath.IsAbs(spec)
	if absSpec && !rt.allowAbsolute {
		return "", "", syntax.ErrorMsg("absolute import paths are disabled")
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
		case wasmExt(e):
			k = "wasm"
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
	return "", "", syntax.Errorf("cannot find import %q", spec)
}

func packageType(p Package) Type {
	keys := make([]string, 0, len(p.Exports))
	for k := range p.Exports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]FnKey, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, FnKey{Name: k, Type: exportType(p.Exports[k])})
	}
	return MapType(fields, nil)
}

func exportType(v Value) Type {
	if t, ok := v.DeclaredType(); ok {
		return t
	}
	switch v.k {
	case KindFn:
		return Dynamic(Any())
	case KindMacro:
		return MacroType()
	default:
		return Any()
	}
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
	if slices.Contains(rt.checkLoading, path) {
		return Type{}, nil, syntax.Errorf("import cycle: %s", spec)
	}
	if rt.exportCache != nil {
		if e, ok := rt.exportCache[path]; ok {
			return e.t, prefixImportDiags(path, e.diags), nil
		}
	}
	if kind == "wasm" {
		if pkg, ok := rt.m.LoadedPackage(path); ok {
			return packageType(pkg), nil, nil
		}
		pkg, err := LoadWasm(path)
		if err != nil {
			return Type{}, nil, err
		}
		rt.m.RememberPackage(path, pkg)
		return packageType(pkg), nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return Type{}, nil, err
	}
	defer f.Close()
	prev := rt.m.File()
	rt.m.SetFile(path)
	res := rt.checkSrc(f, path)
	rt.m.SetFile(prev)
	diags := prefixImportDiags(path, res.Diagnostics)
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "cycle") {
			return Type{}, diags, syntax.ErrorMsg(path + ": " + d.Message)
		}
	}
	if e, ok := rt.exportCache[path]; ok {
		return e.t, diags, nil
	}
	anyT := Any()
	return Dynamic(MapType(nil, &anyT)), diags, nil
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
