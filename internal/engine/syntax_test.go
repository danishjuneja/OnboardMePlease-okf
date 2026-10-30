package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyntaxAdapterPreservesUnicodePaths(t *testing.T) {
	python := os.Getenv("OMP_SYNTAX_PYTHON")
	if python == "" {
		t.Skip("optional tree-sitter environment not configured")
	}
	path := "src/\u03b1.ts"
	result, err := analyzeSyntax(python, []syntaxInput{{Path: path, Source: []byte("export function hello() {}\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if result[path].Syntax != "tree_sitter_typescript" {
		t.Fatalf("unicode path not preserved: %#v", result)
	}
}

func TestSyntaxAdapterReportsErrorClass(t *testing.T) {
	python := os.Getenv("OMP_SYNTAX_PYTHON")
	if python == "" {
		t.Skip("optional tree-sitter environment not configured")
	}
	_, err := syntaxCommand(python, []byte("{"), false)
	if err == nil || !strings.Contains(err.Error(), "JSONDecodeError") {
		t.Fatalf("missing parser failure class: %v", err)
	}
}

func TestSyntaxAdapterAcceptsEmptySources(t *testing.T) {
	python := os.Getenv("OMP_SYNTAX_PYTHON")
	if python == "" {
		t.Skip("optional tree-sitter environment not configured")
	}
	inputs := []syntaxInput{{Path: "empty.ts", Source: []byte{}}, {Path: "nil.ts", Source: nil}}
	result, err := analyzeSyntax(python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if result[input.Path].Syntax != "tree_sitter_typescript" {
			t.Fatalf("empty file missing: %#v", result)
		}
	}
}

func TestSyntaxDeclarationsProduceSearchableChunks(t *testing.T) {
	src := []byte("import x\n\ndef alpha():\n    return beta()\n\ndef beta():\n    return 1\n")
	analysis := syntaxAnalysis{Syntax: "tree_sitter_python", Declarations: []syntaxDeclaration{{Line: 3, Name: "alpha"}, {Line: 6, Name: "beta"}}}
	f := parseFileWithSyntax("sample.py", src, &analysis)
	var names []string
	for _, c := range f.Chunks {
		if c.Symbol != "" {
			names = append(names, c.Symbol)
		}
	}
	if strings.Join(names, ",") != "alpha,beta" || f.Syntax != "tree_sitter_python" {
		t.Fatalf("declarations not indexed: %#v", f)
	}
}

func TestRouteShapePreservesPathStructure(t *testing.T) {
	if routeShape("/api/orders/{id}/cancel") != routeShape("/api/orders/{}/cancel") {
		t.Fatal("route parameters should match")
	}
	if routeShape("/api/orders/{id}/cancel") == routeShape("/api/orders/{id}/refund") {
		t.Fatal("different routes must not match")
	}
	if routeShape("https://example.com/api/orders/{id}/cancel") != "" {
		t.Fatal("absolute hosts require routing analysis")
	}
}

func TestCandidateRelationsCannotProveClaims(t *testing.T) {
	if relationSupportsClaim(Edge{To: "target", Resolution: "candidate"}) {
		t.Fatal("candidate must not establish a connection")
	}
	if !relationSupportsClaim(Edge{To: "target", Resolution: "compiler_resolved"}) {
		t.Fatal("resolved relationship should remain usable")
	}
}

func TestCandidateFanoutIsBoundedAndReported(t *testing.T) {
	root := t.TempDir()
	files := map[string]File{}
	for i := 0; i < 65; i++ {
		name := fmt.Sprintf("server%02d.go", i)
		content := []byte("package demo\nfunc register() { mux.HandleFunc(\"POST /orders/{id}\", handler) }\n")
		if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
			t.Fatal(err)
		}
		files[name] = parseFile(name, content)
	}
	content := []byte("fetch('/orders/123', {method: 'POST'})\n")
	analysis := syntaxAnalysis{Syntax: "tree_sitter_typescript", Requests: []syntaxRequest{{Line: 1, Method: "POST", Route: "/orders/{}"}}}
	files["client.ts"] = parseFileWithSyntax("client.ts", content, &analysis)
	edges, gaps := syntaxEdges(Workspace{Root: root}, files)
	if len(edges) != 64 || len(gaps) != 1 || !strings.HasPrefix(gaps[0], "syntax_candidates_truncated:") {
		t.Fatalf("fan-out was not bounded and reported: %d, %#v", len(edges), gaps)
	}
}

// Set OMP_SYNTAX_PYTHON to a Python environment with both grammars to exercise
// the actual isolated adapter and the SQLite-backed end-to-end path.
func TestOptionalSyntaxAndHTTPNavigation(t *testing.T) {
	if os.Getenv("OMP_SYNTAX_PYTHON") == "" {
		t.Skip("optional tree-sitter environment not configured")
	}
	python, probe, key, err := configureSyntax(Workspace{Root: t.TempDir()}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !syntaxSupported(probe, "x.ts") || !syntaxSupported(probe, "x.py") {
		t.Fatal("test requires TypeScript and Python grammars")
	}
	root := t.TempDir()
	if output, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	for name, content := range map[string]string{
		"go.mod":    "module example.test/demo\n\ngo 1.27.1\n",
		"server.go": "package demo\nfunc register(m *Mux) { m.HandleFunc(\"POST /api/orders/{id}/cancel\", handler) }\ntype Mux struct{}\nfunc (*Mux) HandleFunc(s string, f func()) {}\nfunc handler() {}\ntype Pub interface { Publish(string, string) }\nfunc send(p Pub) { p.Publish(\"refund.requested\", \"1\") }\n",
		"client.ts": "export function cancelOrder(id: string) { return fetch(`/api/orders/${id}/cancel`, {method: 'POST'}); }\n",
		"worker.py": "def on_message(topic):\n    if topic != 'refund.requested':\n        return\n    return topic\n",
		"caller.py": "def relay():\n    return on_message('refund.requested')\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := Workspace{Root: root, ConfigHash: ID(key), Extractor: Version + ":" + key, SyntaxPython: python, SyntaxProbe: probe}
	s, err := Open(w, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if _, err = s.Update(false); err != nil {
		t.Fatal(err)
	}
	res, err := s.Search("cancelOrder", 10)
	if err != nil {
		t.Fatal(err)
	}
	hits := res.Data.([]Hit)
	if len(hits) == 0 || hits[0].Source == nil || hits[0].Source.Symbol != "cancelOrder" {
		t.Fatalf("TypeScript declaration missing: %#v", hits)
	}
	py, err := s.Search("on_message", 10)
	if err != nil || len(py.Data.([]Hit)) == 0 || py.Data.([]Hit)[0].Source.Symbol != "on_message" {
		t.Fatalf("Python declaration missing: %#v %v", py, err)
	}
	relay, err := s.Search("relay", 10)
	if err != nil || len(relay.Data.([]Hit)) == 0 {
		t.Fatalf("caller missing: %v", err)
	}
	refs, err := s.Evidence(relay.Data.([]Hit)[0].ID, "outgoing", "calls", 2, 20, 65536, "")
	if err != nil {
		t.Fatal(err)
	}
	refWalk := refs.Data.(Traversal)
	if len(refWalk.Edges) != 1 || refWalk.Edges[0].Resolution != "candidate" || refWalk.Edges[0].Target != "on_message" {
		t.Fatalf("cross-file candidate missing: %#v", refWalk)
	}
	graph, err := s.Evidence(hits[0].ID, "outgoing", "http_candidate", 2, 20, 65536, "")
	if err != nil {
		t.Fatal(err)
	}
	walk := graph.Data.(Traversal)
	if len(walk.Edges) != 1 || walk.Edges[0].Resolution != "candidate" || len(walk.Sources) != 2 {
		t.Fatalf("HTTP candidate missing: %#v", walk)
	}
	navigation, err := s.Navigate("cancelOrder", 10, "outgoing", "http_candidate", 2, 20, 65536)
	if err != nil {
		t.Fatal(err)
	}
	navigated := navigation.Data.(Navigation)
	if navigated.Seed != hits[0].ID || len(navigated.Traversal.Edges) != 1 || navigated.Traversal.Edges[0].Resolution != "candidate" {
		t.Fatalf("combined navigation lost its candidate or seed: %#v", navigated)
	}
	send, err := s.Search("send", 10)
	if err != nil || len(send.Data.([]Hit)) == 0 {
		t.Fatalf("sender missing: %v", err)
	}
	events, err := s.Evidence(send.Data.([]Hit)[0].ID, "outgoing", "message_candidate", 2, 20, 65536, "")
	if err != nil {
		t.Fatal(err)
	}
	messageWalk := events.Data.(Traversal)
	if len(messageWalk.Edges) != 1 || messageWalk.Edges[0].Resolution != "candidate" || messageWalk.Edges[0].Target != "refund.requested" {
		t.Fatalf("message candidate missing: %#v", messageWalk)
	}
	units, err := s.Pending(20, "")
	if err != nil {
		t.Fatal(err)
	}
	u := units.Data.(map[string]any)["units"].([]Unit)[0]
	r := Result{Schema: 1, Generation: s.Meta("generation"), Unit: u.ID, Fingerprint: u.Fingerprint, Title: "candidate", Reviewed: u.Paths, Assessment: "agent_assessed", Facets: map[string]string{"entry_points": "supported", "conditions": "unresolved", "state_changes": "not_applicable", "transactions": "not_applicable", "downstream": "unresolved", "failures": "not_applicable"}, Claims: []Claim{{ID: "c", Text: "Client reaches server", Kind: "source_fact", Evidence: []string{walk.Edges[0].From}, Relations: []string{walk.Edges[0].ID}}}}
	if _, err = s.Apply(r, true); err == nil || !strings.Contains(err.Error(), "unresolved_relation") {
		t.Fatalf("candidate accepted as proof: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "client.ts"), []byte("export function changed() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := s.Evidence(hits[0].ID, "outgoing", "http_candidate", 2, 20, 65536, ""); err != nil || result.State != "stale" {
		t.Fatalf("changed source not detected: %#v %v", result, err)
	}
	if result, err := s.Navigate("changed", 10, "outgoing", "http_candidate", 2, 20, 65536); err != nil || result.State != "stale" {
		t.Fatalf("navigation hid a changed inventory: %#v %v", result, err)
	}
}
