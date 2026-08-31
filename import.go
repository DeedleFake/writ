package writ

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"deedles.dev/writ/parser"
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
)

func (rt *Runtime) loadImport(spec, fromFile string) (runtime.Value, error) {
	if pkg, ok := rt.pkgs[spec]; ok {
		return runtime.PackageValue(pkg), nil
	}
	path, kind, err := rt.resolveImport(spec, fromFile)
	if err != nil {
		return runtime.Value{}, err
	}
	if kind == "pkg" {
		return runtime.PackageValue(rt.pkgs[path]), nil
	}
	if rt.m.LoadingCycle(path) {
		return runtime.Value{}, runtime.Errorf("import cycle: %s", strings.Join(append(rt.m.LoadingPath(), path), " -> "))
	}
	if exp, ok := rt.m.Loaded(path); ok {
		return exp, nil
	}
	if kind == "plugin" {
		if rt.m.Checking() || !rt.nativePlugins {
			return runtime.Value{}, runtime.ErrorMsg("native plugins are disabled")
		}
		pkg, err := runtime.LoadPlugin(path)
		if err != nil {
			return runtime.Value{}, err
		}
		exp := runtime.PackageValue(pkg)
		rt.m.RememberPackage(path, exp)
		return exp, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return runtime.Value{}, err
	}
	defer f.Close()
	rt.m.PushLoading(path)
	defer rt.m.PopLoading()
	prev := rt.m.File()
	rt.m.SetFile(path)
	defer rt.m.SetFile(prev)
	forms, err := parser.Parse(f)
	if err != nil {
		return runtime.Value{}, parseErr(err, path)
	}
	return rt.m.EvalModule(path, forms)
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
		return "", "", runtime.ErrorMsg("native plugins are disabled")
	}
	absSpec := filepath.IsAbs(spec)
	if absSpec && !rt.allowAbsolute {
		return "", "", runtime.ErrorMsg("absolute import paths are disabled")
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
			if rt.nativePlugins && runtime.PluginsSupported() {
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
		return "", "", runtime.Errorf("plugin not found: %s", spec)
	}
	return "", "", runtime.Errorf("cannot find import %q", spec)
}

func packageType(p runtime.Package) types.Type {
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
	fields := make([]types.ArrowKey, 0, len(keys))
	for _, k := range keys {
		t := types.Any()
		if _, ok := p.Funcs[k]; ok {
			t = types.FnType(types.PosRestArrow(types.Dynamic(types.Any())))
		}
		fields = append(fields, types.ArrowKey{Name: k, Type: t})
	}
	return types.MapType(fields, nil)
}

func (rt *Runtime) importType(spec, fromFile string) (types.Type, []types.Diagnostic, error) {
	if pkg, ok := rt.pkgs[spec]; ok {
		return packageType(pkg), nil, nil
	}
	path, kind, err := rt.resolveImport(spec, fromFile)
	if err != nil {
		return types.Type{}, nil, err
	}
	if kind == "pkg" {
		return packageType(rt.pkgs[path]), nil, nil
	}
	if slices.Contains(rt.checkLoading, path) {
		return types.Type{}, nil, runtime.Errorf("import cycle: %s", spec)
	}
	if rt.exportCache != nil {
		if e, ok := rt.exportCache[path]; ok {
			return e.t, prefixImportDiags(path, e.diags), nil
		}
	}
	if kind == "plugin" {
		anyT := types.Any()
		return types.Dynamic(types.MapType(nil, &anyT)), nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return types.Type{}, nil, err
	}
	defer f.Close()
	prev := rt.m.File()
	rt.m.SetFile(path)
	res := rt.checkSrc(f, path)
	rt.m.SetFile(prev)
	diags := prefixImportDiags(path, res.Diagnostics)
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "cycle") {
			return types.Type{}, diags, runtime.ErrorMsg(path + ": " + d.Message)
		}
	}
	if e, ok := rt.exportCache[path]; ok {
		return e.t, diags, nil
	}
	anyT := types.Any()
	return types.Dynamic(types.MapType(nil, &anyT)), diags, nil
}

func prefixImportDiags(path string, diags []types.Diagnostic) []types.Diagnostic {
	if len(diags) == 0 {
		return nil
	}
	out := make([]types.Diagnostic, len(diags))
	for i, d := range diags {
		if path != "" && !strings.HasPrefix(d.Message, path) {
			d.Message = path + ": " + d.Message
		}
		out[i] = d
	}
	return out
}
