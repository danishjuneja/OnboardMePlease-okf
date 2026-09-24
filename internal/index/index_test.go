package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "parse-go" {
		if err := RunParser(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestGoParserProcessIsolation(t *testing.T) {
	symbols, calls, reason := parseGoIsolated(context.Background(), "snapshot", "server/main.go", []byte("package main\nfunc main(){helper()}\nfunc helper(){}\n"))
	if reason != "" || len(symbols) != 2 || len(calls) != 1 {
		t.Fatalf("isolated parse: symbols=%v calls=%v reason=%s", symbols, calls, reason)
	}
	_, _, reason = parseGoIsolated(context.Background(), "snapshot", "broken.go", []byte("package main\nfunc broken( {"))
	if reason != "go_parse_failed" {
		t.Fatalf("malformed parser result: %s", reason)
	}
}

func TestChunksHaveStableSourceRanges(t *testing.T) {
	lines := make([]string, 165)
	for i := range lines {
		lines[i] = "line content"
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	first, err := chunks("snapshot", "src/readme.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := chunks("snapshot", "src/readme.txt", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || first[0].StartLine != 1 || first[0].EndLine != 80 || first[1].StartLine != 81 || first[2].EndLine != 165 {
		t.Fatalf("unexpected chunks: %+v", first)
	}
	if first[0].ID != second[0].ID {
		t.Fatal("chunk ID changed for identical snapshot")
	}
	if first[0].ID == first[1].ID {
		t.Fatal("separate ranges share an ID")
	}
}

func TestGoASTResolvesGuardedCycleWithoutNameGuessing(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mixed-monolith", "server", "cancel.go"))
	if err != nil {
		t.Fatal(err)
	}
	symbols, calls, reason := extractGo("snapshot", "server/cancel.go", data)
	if reason != "" {
		t.Fatal(reason)
	}
	relations := resolveCalls("snapshot", symbols, calls)
	byID := map[string]string{}
	for _, symbol := range symbols {
		byID[symbol.ID] = symbol.Name
	}
	var forward, backward bool
	for _, relation := range relations {
		if byID[relation.FromID] == "processCancellation" && byID[relation.ToID] == "auditCancellation" {
			forward = true
		}
		if byID[relation.FromID] == "auditCancellation" && byID[relation.ToID] == "processCancellation" {
			backward = true
		}
	}
	if !forward || !backward {
		t.Fatalf("guarded call cycle not captured: %+v", relations)
	}
	legacy := []Symbol{{ID: "legacy", Path: "server/legacy.go", Name: "cancelOrder", QualifiedName: "server/main.cancelOrder", Kind: "function"}}
	for _, relation := range resolveCalls("snapshot", append(symbols, legacy...), calls) {
		if relation.ToID == "legacy" {
			t.Fatal("unrelated same-name helper was linked")
		}
	}
}

func TestMalformedGoKeepsTextCapability(t *testing.T) {
	data := []byte("func broken( {\n")
	pieces, err := chunks("snapshot", "bad.go", data)
	if err != nil || len(pieces) != 1 {
		t.Fatalf("text chunks: %v, %v", pieces, err)
	}
	_, _, reason := extractGo("snapshot", "bad.go", data)
	if reason != "go_parse_failed" {
		t.Fatalf("parse reason = %q", reason)
	}
}
