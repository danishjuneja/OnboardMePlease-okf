package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func investigationStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	if b, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, b)
	}
	for p, contents := range map[string]string{"go.mod": "module example.test/demo\n\ngo 1.27.1\n", "action.go": "package demo\ntype Action struct{}\nfunc ExecuteAction() { doWork() }\nfunc doWork() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := Workspace{Root: root, ConfigHash: ID("test"), Extractor: Version}
	s, err := Open(w, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	if _, err = s.Update(false); err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestInvestigationFindsTypeDeclarationFromQuestion(t *testing.T) {
	s, _ := investigationStore(t)
	r, err := s.Investigate("How does the Action class work exactly?", 3)
	if err != nil {
		t.Fatal(err)
	}
	sources := r.Data.(Investigation).Sources
	if len(sources) == 0 || !strings.Contains(sources[0].Excerpt, "type Action struct") {
		t.Fatalf("missing Action declaration: %#v", sources)
	}
}

func TestInvestigationFollowsSupportedCall(t *testing.T) {
	s, _ := investigationStore(t)
	r, err := s.Investigate("ExecuteAction", 3)
	if err != nil {
		t.Fatal(err)
	}
	data := r.Data.(Investigation)
	found := false
	for _, source := range data.RelatedSources {
		if source.Symbol == "doWork" {
			found = true
		}
	}
	if !found {
		t.Fatalf("call target was not returned: %#v", data)
	}
}

func TestInvestigationPrioritizesCallsAtHighDegreeNodes(t *testing.T) {
	s, _ := investigationStore(t)
	find := func(q string) string {
		r, err := s.Search(q, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range r.Data.([]Hit) {
			if h.Source != nil && h.Source.Symbol == q {
				return h.ID
			}
		}
		t.Fatalf("missing %s", q)
		return ""
	}
	seed, target := find("ExecuteAction"), find("doWork")
	if _, err := s.DB.Exec("DELETE FROM edges WHERE src=? OR dst=?", seed, seed); err != nil {
		t.Fatal(err)
	}
	insert := func(e Edge) {
		if _, err := s.DB.Exec("INSERT INTO edges VALUES (?,?,?,?,?)", e.ID, e.From, e.To, e.Kind, encode(e)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 70; i++ {
		insert(Edge{ID: fmt.Sprintf("%03d", i), From: seed, To: target, Kind: "next", Resolution: "source_order"})
	}
	insert(Edge{ID: "zzz-call", From: seed, To: target, Kind: "calls", Resolution: "compiler_resolved"})
	r, err := s.Investigate("ExecuteAction", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Data.(Investigation).Relationships {
		if e.ID == "zzz-call" {
			return
		}
	}
	t.Fatal("supported call was hidden by lower-priority edges")
}

func TestInvestigationRanksImplementationForNaturalLanguageFlow(t *testing.T) {
	s, root := investigationStore(t)
	for p, body := range map[string]string{
		"broker/produce.scala":       "class Broker { def handleProduceRequest(): Unit = { appendRecords() } }\n",
		"src/test/broker_test.scala": "// producer request broker request producer request\nclass BrokerTest {}\n",
	} {
		name := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Update(false); err != nil {
		t.Fatal(err)
	}
	r, err := s.Investigate("How does the broker handle a producer request?", 3)
	if err != nil {
		t.Fatal(err)
	}
	sources := r.Data.(Investigation).Sources
	if len(sources) == 0 || sources[0].Path != "broker/produce.scala" {
		t.Fatalf("implementation not ranked first: %#v", sources)
	}
}

func TestInvestigationAndEvidenceBackedNote(t *testing.T) {
	s, root := investigationStore(t)
	res, err := s.Investigate("How does ExecuteAction work exactly?", 3)
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(Investigation)
	if len(data.Sources) == 0 || data.Sources[0].Symbol != "ExecuteAction" {
		t.Fatalf("missing declaration: %#v", data)
	}
	id := data.Sources[0].ID
	if _, err = s.AddNote("ExecuteAction invokes doWork", "source_fact", []string{id}, nil, false, "high"); err != nil {
		t.Fatal(err)
	}
	res, err = s.Search("ExecuteAction invokes doWork", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range res.Data.([]Hit) {
		if h.Claim != nil && h.Claim.Claim.Text == "ExecuteAction invokes doWork" && h.Claim.Claim.Confidence == "high" {
			found = true
		}
	}
	if !found {
		t.Fatal("saved note is not searchable")
	}
	if _, err = s.AddNote("bad confidence", "source_fact", []string{id}, nil, false, "certain"); err == nil {
		t.Fatal("invalid confidence accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "action.go"), []byte("package demo\nfunc ExecuteAction() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err = s.Search("ExecuteAction invokes doWork", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Data.([]Hit) {
		if h.Claim != nil {
			t.Fatal("stale note was returned as current")
		}
	}
	if _, err = s.AddNote("stale", "source_fact", []string{id}, nil, false); err == nil {
		t.Fatal("stale evidence accepted")
	}
	if _, err = s.Update(false); err != nil {
		t.Fatal(err)
	}
	res, err = s.Search("ExecuteAction invokes doWork", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range res.Data.([]Hit) {
		if h.Claim != nil {
			t.Fatal("updated index reused a stale note")
		}
	}
}

func TestInvestigationEmptyAndNoteValidation(t *testing.T) {
	s, _ := investigationStore(t)
	res, err := s.Investigate("zzzz_nonexistent_identifier", 3)
	if err != nil || res.State != "no_match" {
		t.Fatalf("expected no match: %#v %v", res, err)
	}
	if _, err = s.AddNote("claim", "source_fact", nil, nil, false); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("missing evidence accepted: %v", err)
	}
}

func TestStatusKeepsDetailedGapsOptIn(t *testing.T) {
	s, _ := investigationStore(t)
	if _, err := s.DB.Exec("INSERT OR REPLACE INTO meta VALUES ('capability_gaps', ?)", `["one","two"]`); err != nil {
		t.Fatal(err)
	}
	compact, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	data := compact.Data.(map[string]any)
	if data["capability_gap_count"] != 2 || data["capability_gaps"] != nil {
		t.Fatalf("unexpected compact status: %#v", data)
	}
	verbose, err := s.Status(true)
	if err != nil || len(verbose.Data.(map[string]any)["capability_gaps"].([]string)) != 2 {
		t.Fatalf("missing verbose gaps: %#v %v", verbose, err)
	}
}
