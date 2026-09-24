package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}

func TestGitHubInputOnly(t *testing.T) {
	valid, err := ValidateInput(Input{Kind: "github", URL: "https://github.com/example/project", PrivacyMode: "strict_local"})
	if err != nil || valid.URL != "https://github.com/example/project.git" {
		t.Fatalf("canonical GitHub input = %+v, %v", valid, err)
	}
	for _, input := range []Input{
		{Kind: "local", URL: "C:/dev/project", PrivacyMode: "strict_local"},
		{Kind: "github", URL: "https://user:secret@github.com/a/b", PrivacyMode: "strict_local"},
		{Kind: "github", URL: "https://evil.example/a/b", PrivacyMode: "strict_local"},
		{Kind: "github", URL: "https://github.com/a/b", Ref: "--upload-pack=evil", PrivacyMode: "strict_local"},
		{Kind: "github", URL: "file:///tmp/repo", PrivacyMode: "strict_local"},
	} {
		if _, err := ValidateInput(input); err == nil {
			t.Fatalf("accepted unsupported input: %+v", input)
		}
	}
}

func TestCommittedObjectsIgnoreWorkingTreeChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "Test")
	file := filepath.Join(root, "src", "main.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "src/main.txt")
	runGit(t, root, "commit", "-m", "initial")
	oidBytes, err := git(context.Background(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	oid := strings.TrimSpace(string(oidBytes))
	if err := os.WriteFile(file, []byte("dirty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := listFiles(context.Background(), root, oid)
	if err != nil || len(files) != 1 || files[0].path != "src/main.txt" {
		t.Fatalf("committed inventory = %+v, %v", files, err)
	}
	content, reason := readCommittedFile(context.Background(), root, oid, files[0].path)
	if reason != "" || string(content) != "committed\n" {
		t.Fatalf("committed content = %q, %s", content, reason)
	}
}
