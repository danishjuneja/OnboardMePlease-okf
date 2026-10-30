package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"onboardmeplease/internal/engine"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionJSONLReusesSnapshot(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package demo\nfunc Demo() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := engine.Workspace{Root: root, ConfigHash: engine.ID("test"), Extractor: engine.Version}
	s, err := engine.Open(w, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if _, err = s.Update(false); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"command\":\"investigate\",\"query\":\"Demo\"}\n{\"command\":\"search\",\"query\":\"Demo\"}\n{\"command\":\"wrong\"}\n{\"command\":\"close\"}\n")
	var output bytes.Buffer
	if err = serveSession(s, input, &output, nil); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected ready and four replies: %s", output.String())
	}
	for _, line := range lines[1:3] {
		var r engine.Envelope
		if err = json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.State != "partial" || r.Verification != "session_snapshot" {
			t.Fatalf("invalid session state: %#v", r)
		}
	}
	var invalid engine.Envelope
	if err = json.Unmarshal([]byte(lines[3]), &invalid); err != nil || invalid.State != "unavailable" {
		t.Fatalf("invalid command not reported: %#v %v", invalid, err)
	}
}

func TestSessionFailedRefreshInvalidatesSnapshot(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package demo\nfunc Demo() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := engine.Workspace{Root: root, ConfigHash: engine.ID("test"), Extractor: engine.Version}
	s, err := engine.Open(w, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if _, err = s.Update(false); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"command\":\"refresh\"}\n{\"command\":\"search\",\"query\":\"Demo\"}\n{\"command\":\"close\"}\n")
	var output bytes.Buffer
	if err = serveSession(s, input, &output, func() (engine.Workspace, error) { return engine.Workspace{}, fmt.Errorf("bad_config") }); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var refreshed, next engine.Envelope
	if err := json.Unmarshal([]byte(lines[1]), &refreshed); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[2]), &next); err != nil {
		t.Fatal(err)
	}
	if refreshed.State != "unavailable" || next.State != "stale" {
		t.Fatalf("failed refresh did not invalidate: %#v %#v", refreshed, next)
	}
}
