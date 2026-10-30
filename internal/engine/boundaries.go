package engine

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

type routeRegistration struct{ chunk string }
type syntaxTarget struct{ chunk string }
type eventPublication struct {
	chunk, path string
	line        int
}

// These edges are navigational candidates: syntax and matching route shapes do
// not resolve shadowing, runtime routing, service identity, or execution.
func syntaxEdges(w Workspace, files map[string]File) ([]Edge, []string) {
	routes := map[string][]routeRegistration{}
	publications := map[string][]eventPublication{}
	declarations := map[string][]syntaxTarget{}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := files[p]
		language := syntaxLanguage(p)
		if language == "" || !strings.HasPrefix(f.Syntax, "tree_sitter_") {
			continue
		}
		for _, c := range f.Chunks {
			if c.Symbol != "" {
				key := language + "\x00" + c.Symbol
				declarations[key] = append(declarations[key], syntaxTarget{c.ID})
			}
		}
	}
	for _, p := range paths {
		f := files[p]
		if !strings.HasSuffix(p, ".go") || f.Reason != "" {
			continue
		}
		b, _, err := w.Read(p)
		if err != nil {
			continue
		}
		fs := token.NewFileSet()
		tree, err := parser.ParseFile(fs, p, b, 0)
		if err != nil {
			continue
		}
		ast.Inspect(tree, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "HandleFunc" && selector.Sel.Name != "Publish") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			chunk := chunkAt(f, fs.Position(call.Pos()).Line)
			if selector.Sel.Name == "Publish" {
				if pattern != "" && chunk != "" {
					publications[pattern] = append(publications[pattern], eventPublication{chunk, p, fs.Position(call.Pos()).Line})
				}
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			parts := strings.Fields(pattern)
			if len(parts) != 2 {
				return true
			}
			shape := routeShape(parts[1])
			if shape != "" && chunk != "" {
				key := parts[0] + "\x00" + shape
				routes[key] = append(routes[key], routeRegistration{chunk})
			}
			return true
		})
	}
	for key := range routes {
		sort.Slice(routes[key], func(i, j int) bool { return routes[key][i].chunk < routes[key][j].chunk })
	}
	for key := range publications {
		sort.Slice(publications[key], func(i, j int) bool {
			a, b := publications[key][i], publications[key][j]
			if a.path != b.path {
				return a.path < b.path
			}
			if a.line != b.line {
				return a.line < b.line
			}
			return a.chunk < b.chunk
		})
	}
	var edges []Edge
	truncated := false
	appendEdge := func(edge Edge) {
		if len(edges) >= 100000 {
			truncated = true
			return
		}
		edges = append(edges, edge)
	}
	for _, p := range paths {
		f := files[p]
		if f.Reason != "" {
			continue
		}
		for _, call := range f.SyntaxCalls {
			targets := declarations[syntaxLanguage(p)+"\x00"+call.Name]
			from := chunkAt(f, call.Line)
			if from == "" {
				continue
			}
			if len(targets) > 64 {
				targets = targets[:64]
				truncated = true
			}
			for _, target := range targets {
				edge := Edge{From: from, To: target.chunk, Kind: "calls", Resolution: "candidate", Method: "tree_sitter_syntax", Path: p, Line: call.Line, Target: call.Name,
					Conditions: []string{"Name match only; receiver, imports, scope, shadowing and runtime dispatch are unverified."}}
				edge.ID = ID(Version, edge.From, edge.To, edge.Kind, edge.Resolution, p, fmt.Sprint(edge.Line), edge.Target)
				appendEdge(edge)
			}
		}
		for _, req := range f.Requests {
			shape, from := routeShape(req.Route), chunkAt(f, req.Line)
			if shape == "" || from == "" {
				continue
			}
			matches := routes[req.Method+"\x00"+shape]
			if len(matches) > 64 {
				matches = matches[:64]
				truncated = true
			}
			for _, route := range matches {
				edge := Edge{From: from, To: route.chunk, Kind: "http_candidate", Resolution: "candidate", Method: "route_shape", Path: p, Line: req.Line, Target: req.Method + " " + req.Route,
					Conditions: []string{"Matching method and route shape; fetch binding, routing, service identity and deployment are unverified."}}
				edge.ID = ID(Version, edge.From, edge.To, edge.Kind, edge.Resolution, p, fmt.Sprint(edge.Line), edge.Target)
				appendEdge(edge)
			}
		}
		for _, event := range f.Events {
			to := chunkAt(f, event.Line)
			if to == "" {
				continue
			}
			matches := publications[event.Topic]
			if len(matches) > 64 {
				matches = matches[:64]
				truncated = true
			}
			for _, publication := range matches {
				edge := Edge{From: publication.chunk, To: to, Kind: "message_candidate", Resolution: "candidate", Method: "topic_literal", Path: publication.path, Line: publication.line, Target: event.Topic,
					Conditions: []string{"Matching string literals only; publisher binding, consumer registration, broker identity and delivery are unverified."}}
				edge.ID = ID(Version, edge.From, edge.To, edge.Kind, edge.Resolution, edge.Path, fmt.Sprint(edge.Line), edge.Target)
				appendEdge(edge)
			}
		}
	}
	if truncated {
		return edges, []string{"syntax_candidates_truncated: inspect source directly"}
	}
	return edges, nil
}

func syntaxLanguage(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	}
	return ""
}
