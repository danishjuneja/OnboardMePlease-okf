package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFastFreshnessDetectsInventoryChanges(t *testing.T) {
	s, root := investigationStore(t)
	s.Fast = true
	r, err := s.Investigate("ExecuteAction", 3)
	if err != nil || r.Verification != "metadata" || r.State == "stale" {
		t.Fatalf("fast mode not labeled: %#v %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package demo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = s.Investigate("ExecuteAction", 3)
	if err != nil || r.State != "stale" {
		t.Fatalf("new file not detected: %#v %v", r, err)
	}
}

func TestFastFreshnessIgnoresAlreadyMissingTrackedFiles(t *testing.T) {
	s, root := investigationStore(t)
	name := filepath.Join(root, "removed.go")
	if err := os.WriteFile(name, []byte("package demo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "removed.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(false); err != nil {
		t.Fatal(err)
	}
	s.Fast = true
	current, err := s.Audit()
	if err != nil || !current {
		t.Fatalf("already-missing tracked file broke fast check: %v %v", current, err)
	}
}

func TestFastFreshnessDetectsDeletedExcludedFile(t *testing.T) {
	s, root := investigationStore(t)
	name := filepath.Join(root, "binary.dat")
	if err := os.WriteFile(name, []byte{0, 1, 2}, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", "binary.dat").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if _, err := s.Update(false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	s.Fast = true
	current, err := s.Audit()
	if err != nil || current {
		t.Fatalf("deleted excluded file not detected: %v %v", current, err)
	}
}

func TestFastFreshnessRechecksReturnedSourceContent(t *testing.T) {
	s, root := investigationStore(t)
	name := filepath.Join(root, "action.go")
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(original), "doWork", "noWork", 1)
	if err := os.WriteFile(name, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	s.Fast = true
	r, err := s.Investigate("ExecuteAction", 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range r.Data.(Investigation).Sources {
		if source.Path == "action.go" {
			t.Fatal("returned a source with a stale content hash")
		}
	}
	if r.State != "partial" {
		t.Fatalf("content mismatch not reported: %#v", r)
	}
}

func TestFastFreshnessDoesNotClaimFullVerification(t *testing.T) {
	s, root := investigationStore(t)
	path := filepath.Join(root, "go.mod")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := append([]byte(nil), before...)
	after[len(after)-2] = '0'
	if err := os.WriteFile(path, after, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	s.Fast = true
	fast, err := s.Investigate("ExecuteAction", 3)
	if err != nil || fast.Verification != "metadata" {
		t.Fatalf("missing fast verification label: %#v %v", fast, err)
	}
	s.Fast = false
	full, err := s.Investigate("ExecuteAction", 3)
	if err != nil || full.State != "stale" {
		t.Fatalf("full hash failed to detect change: %#v %v", full, err)
	}
}
