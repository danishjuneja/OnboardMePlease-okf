package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSyntaxProbeCacheInvalidatesChangedAndAddedDependencies(t *testing.T) {
	dir := t.TempDir()
	dependency := filepath.Join(dir, "grammar.bin")
	missing := filepath.Join(dir, "missing-grammar")
	if err := os.WriteFile(dependency, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	stamps, err := syntaxDependencyStamps([]string{dependency, missing})
	if err != nil {
		t.Fatal(err)
	}
	if !syntaxDependenciesCurrent(stamps) {
		t.Fatal("unchanged dependency reported stale")
	}
	if err := os.WriteFile(dependency, []byte("new version"), 0600); err != nil {
		t.Fatal(err)
	}
	if syntaxDependenciesCurrent(stamps) {
		t.Fatal("modified grammar not detected")
	}
	stamps, err = syntaxDependencyStamps([]string{dependency, missing})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, []byte("installed"), 0600); err != nil {
		t.Fatal(err)
	}
	if syntaxDependenciesCurrent(stamps) {
		t.Fatal("new dependency not detected")
	}
}

func TestSyntaxProbeRechecksWhenDependencyChanges(t *testing.T) {
	if os.Getenv("OMP_SYNTAX_PYTHON") == "" {
		t.Skip("optional tree-sitter environment not configured")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".onboard", "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	w := Workspace{Root: root}
	_, first, firstKey, err := configureSyntax(w, false)
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(root, ".onboard", "cache", "syntax-probe.json")
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var cache syntaxProbeCache
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "new-grammar")
	cache.Probe.Dependencies = append(cache.Probe.Dependencies, missing)
	cache.Probe.Python = "stale-probe"
	cache.Stamps, err = syntaxDependencyStamps(cache.Probe.Dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(encode(cache)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(missing, []byte("installed"), 0600); err != nil {
		t.Fatal(err)
	}
	_, refreshed, refreshedKey, err := configureSyntax(w, false)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Python != first.Python || refreshedKey != firstKey {
		t.Fatal("changed dependency did not trigger a fresh probe")
	}
	_, cached, cachedKey, err := configureSyntax(w, false)
	if err != nil || cached.Python != first.Python || cachedKey != firstKey {
		t.Fatalf("unchanged probe cache invalid: %v", err)
	}
	raw, err = os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cache); err != nil {
		t.Fatal(err)
	}
	cache.Probe.Python = "forged-cached-probe"
	if err := os.WriteFile(cachePath, []byte(encode(cache)), 0600); err != nil {
		t.Fatal(err)
	}
	_, verified, verifiedKey, err := configureSyntax(w, true)
	if err != nil || verified.Python != first.Python || verifiedKey != firstKey {
		t.Fatalf("verification did not re-probe: %v", err)
	}
}
