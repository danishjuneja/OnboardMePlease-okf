package engine

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The importer reads only indexed local Go source and compiler-exported standard
// packages. It never invokes go list, downloads modules, or executes a build.
type localImporter struct {
	groups     map[string][]string
	files      map[string]File
	w          Workspace
	packages   map[string]*types.Package
	busy       map[string]bool
	std        types.Importer
	edges      []Edge
	gaps       []string
	moduleDirs map[string]string
	targets    map[types.Object]string
}

func chunkAt(f File, line int) string {
	for _, c := range f.Chunks {
		if line >= c.Start && line <= c.End {
			return c.ID
		}
	}
	return ""
}
func (l *localImporter) Import(p string) (*types.Package, error) {
	if pkg := l.packages[p]; pkg != nil {
		return pkg, nil
	}
	paths := l.groups[p]
	if len(paths) == 0 {
		if !strings.Contains(strings.Split(p, "/")[0], ".") {
			return l.std.Import(p)
		}
		return nil, fmt.Errorf("dependency_not_indexed: %s", p)
	}
	if l.busy[p] {
		return nil, fmt.Errorf("import_cycle")
	}
	l.busy[p] = true
	defer delete(l.busy, p)
	fs := token.NewFileSet()
	asts := []*ast.File{}
	filePaths := map[*ast.File]string{}
	for _, f := range paths {
		b, _, e := l.w.Read(f)
		if e != nil {
			continue
		}
		a, e := parser.ParseFile(fs, f, b, 0)
		if e == nil {
			asts = append(asts, a)
			filePaths[a] = f
		}
	}
	if len(asts) == 0 {
		return nil, fmt.Errorf("no_syntax")
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	errs := 0
	cfg := types.Config{Importer: l, Error: func(error) { errs++ }}
	pkg, _ := cfg.Check(p, fs, asts, info)
	if pkg == nil {
		return nil, fmt.Errorf("type_check_failed")
	}
	l.packages[p] = pkg
	for ident, obj := range info.Defs {
		if obj != nil {
			pos := fs.Position(ident.Pos())
			l.targets[obj] = chunkAt(l.files[pos.Filename], pos.Line)
		}
	}
	if errs > 0 {
		l.gaps = append(l.gaps, fmt.Sprintf("partial_type_information:%s:%d", p, errs))
	}
	target := func(obj types.Object) (string, string) {
		if obj == nil {
			return "", ""
		}
		return l.targets[obj], obj.String()
	}
	add := func(from, to, kind, res, method, file string, line int, name string) {
		if from == "" {
			return
		}
		ed := Edge{From: from, To: to, Kind: kind, Resolution: res, Method: method, Path: file, Line: line, Target: name}
		ed.ID = ID(Version, from, to, kind, res, file, fmt.Sprint(line), name)
		l.edges = append(l.edges, ed)
	}
	for _, a := range asts {
		file := filePaths[a]
		f := l.files[file]
		for i := 0; i+1 < len(f.Chunks); i++ {
			x, y := f.Chunks[i], f.Chunks[i+1]
			add(x.ID, y.ID, "next", "source_order", "chunker", file, x.End, "")
		}
		for _, imp := range a.Imports {
			dest, _ := strconv.Unquote(imp.Path.Value)
			line := fs.Position(imp.Pos()).Line
			to := ""
			if candidates := l.groups[dest]; len(candidates) > 0 && len(l.files[candidates[0]].Chunks) > 0 {
				to = l.files[candidates[0]].Chunks[0].ID
			}
			res := "external"
			if to != "" {
				res = "local_package"
			}
			add(chunkAt(f, line), to, "imports", res, "go_ast", file, line, dest)
		}
		ast.Inspect(a, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				line := fs.Position(node.Pos()).Line
				var obj types.Object
				dynamic := false
				switch callee := node.Fun.(type) {
				case *ast.Ident:
					obj = info.Uses[callee]
				case *ast.SelectorExpr:
					obj = info.Uses[callee.Sel]
					if sel := info.Selections[callee]; sel != nil {
						_, dynamic = sel.Recv().Underlying().(*types.Interface)
					}
				case *ast.IndexExpr:
					if id, ok := callee.X.(*ast.Ident); ok {
						obj = info.Uses[id]
					}
				}
				to, name := target(obj)
				res := "unresolved"
				if _, ok := obj.(*types.Func); ok && to != "" && !dynamic {
					res = "compiler_resolved"
				} else {
					to = ""
				}
				if dynamic {
					res = "dynamic_dispatch"
				}
				if name == "" {
					name = "unresolved call"
				}
				add(chunkAt(f, line), to, "calls", res, "go_types", file, line, name)
			case *ast.Ident:
				obj := info.Uses[node]
				if _, ok := obj.(*types.Func); !ok {
					return true
				}
				to, name := target(obj)
				line := fs.Position(node.Pos()).Line
				if to != "" {
					add(chunkAt(f, line), to, "references", "compiler_resolved", "go_types", file, line, name)
				}
			}
			return true
		})
	}
	return pkg, nil
}
func extractGraph(w Workspace, files map[string]File) ([]Edge, []string) {
	std := importer.ForCompiler(token.NewFileSet(), "gc", func(p string) (io.ReadCloser, error) {
		if path.Clean(p) != p || strings.HasPrefix(p, "../") || path.IsAbs(p) {
			return nil, fmt.Errorf("invalid_import")
		}
		return os.Open(filepath.Join(build.Default.GOROOT, "pkg", build.Default.GOOS+"_"+build.Default.GOARCH, filepath.FromSlash(p)+".a"))
	})
	l := &localImporter{groups: map[string][]string{}, files: files, w: w, packages: map[string]*types.Package{}, busy: map[string]bool{}, std: std, moduleDirs: map[string]string{}, targets: map[types.Object]string{}}
	for p := range files {
		if path.Base(p) == "go.mod" {
			b, _, e := w.Read(p)
			if e != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "module" {
					l.moduleDirs[path.Dir(p)] = strings.Trim(fields[1], "\"")
				}
			}
		}
	}
	for _, f := range files {
		for i := 0; i+1 < len(f.Chunks); i++ {
			a, b := f.Chunks[i], f.Chunks[i+1]
			e := Edge{From: a.ID, To: b.ID, Kind: "next", Resolution: "source_order", Method: "chunker", Path: f.Path, Line: a.End}
			e.ID = ID(Version, e.From, e.To, e.Kind, e.Resolution, e.Path, fmt.Sprint(e.Line), "")
			l.edges = append(l.edges, e)
		}
	}
	paths := []string{}
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	origins := map[string]string{}
	ambiguous := map[string]bool{}
	for _, p := range paths {
		f := files[p]
		if !strings.HasSuffix(p, ".go") || f.Reason != "" {
			continue
		}
		if strings.HasSuffix(p, "_test.go") {
			l.gaps = append(l.gaps, "tests_text_and_syntax_only:"+p)
			continue
		}
		ok, e := build.Default.MatchFile(filepath.Join(w.Root, filepath.FromSlash(path.Dir(p))), path.Base(p))
		if e != nil || !ok {
			l.gaps = append(l.gaps, "inactive_build_file:"+p)
			continue
		}
		dir := path.Dir(p)
		best := ""
		mod := ""
		for d, m := range l.moduleDirs {
			if (d == "." || dir == d || strings.HasPrefix(dir, d+"/")) && (best == "" || len(d) > len(best)) {
				best = d
				mod = m
			}
		}
		key := dir
		if mod != "" {
			rel := strings.TrimPrefix(strings.TrimPrefix(dir, best), "/")
			if best == "." {
				rel = strings.TrimPrefix(dir, "./")
				if rel == "." {
					rel = ""
				}
			}
			key = strings.TrimSuffix(mod+"/"+rel, "/")
		}
		if origin, ok := origins[key]; ok && origin != dir {
			ambiguous[key] = true
		}
		origins[key] = dir
		l.groups[key] = append(l.groups[key], p)
	}
	for k := range ambiguous {
		delete(l.groups, k)
		l.gaps = append(l.gaps, "ambiguous_module_identity:"+k)
	}
	keys := []string{}
	for k := range l.groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, e := l.Import(k); e != nil {
			l.gaps = append(l.gaps, "semantic_unavailable:"+k)
		}
	}
	l.gaps = append(l.gaps, "non_go_languages:text_only", "framework_wiring:agent_investigation", "go_build_context:"+build.Default.GOOS+"/"+build.Default.GOARCH)
	sort.Slice(l.edges, func(i, j int) bool { return l.edges[i].ID < l.edges[j].ID })
	out := []Edge{}
	last := ""
	for _, e := range l.edges {
		if e.ID != last {
			out = append(out, e)
			last = e.ID
		}
	}
	return out, l.gaps
}
