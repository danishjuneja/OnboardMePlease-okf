package engine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Workspace struct {
	Root       string
	Excludes   []string
	ConfigHash string
}

func git(root string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	b, e := c.Output()
	return string(b), e
}
func Discover() (Workspace, error) {
	cwd, e := os.Getwd()
	if e != nil {
		return Workspace{}, e
	}
	r, e := git(cwd, "rev-parse", "--show-toplevel")
	if e != nil {
		return Workspace{}, fmt.Errorf("not_git: run inside a Git working tree")
	}
	w := Workspace{Root: strings.TrimSpace(r)}
	if _, err := w.safe(".onboard/config.toml"); err != nil && !os.IsNotExist(err) {
		return w, fmt.Errorf("unsafe_config: %w", err)
	}
	b, e := os.ReadFile(filepath.Join(w.Root, ".onboard", "config.toml"))
	if e != nil && !os.IsNotExist(e) {
		return w, e
	}
	w.ConfigHash = ID(Version, string(b), runtime.GOOS, runtime.GOARCH, runtime.Version())
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "exclude = ") {
			return w, fmt.Errorf("invalid_config: supported setting is exclude = [\"path\"]")
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "exclude = "))
		if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
			return w, fmt.Errorf("invalid_config")
		}
		for _, v := range strings.Split(strings.Trim(raw, "[]"), ",") {
			if strings.TrimSpace(v) == "" {
				continue
			}
			s, e := strconv.Unquote(strings.TrimSpace(v))
			if e != nil {
				return w, e
			}
			w.Excludes = append(w.Excludes, s)
		}
	}
	return w, nil
}
func (w Workspace) Head() string {
	v, _ := git(w.Root, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(v)
}
func (w Workspace) Paths() ([]string, error) {
	v, e := git(w.Root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if e != nil {
		return nil, e
	}
	set := map[string]bool{}
	for _, p := range strings.Split(v, "\x00") {
		if p != "" && !w.excluded(p) {
			if _, e := os.Lstat(filepath.Join(w.Root, filepath.FromSlash(p))); os.IsNotExist(e) {
				continue
			}
			set[p] = true
		}
	}
	out := []string{}
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
func (w Workspace) excluded(p string) bool {
	b := strings.ToLower(path.Base(p))
	if b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".pem") || strings.HasSuffix(b, ".key") || strings.HasSuffix(b, ".p12") || strings.HasSuffix(b, ".pfx") {
		return true
	}
	for _, v := range strings.Split(strings.ToLower(p), "/") {
		switch v {
		case ".git", ".onboard", ".codex", ".claude", "graphify-out", "node_modules", "vendor", "secrets", ".ssh", ".aws":
			return true
		}
	}
	for _, v := range w.Excludes {
		m, _ := path.Match(v, p)
		if m || p == strings.TrimSuffix(v, "/") || strings.HasPrefix(p, strings.TrimSuffix(v, "/")+"/") {
			return true
		}
	}
	return false
}

// Reject symlinks in every path component, including directory junctions exposed
// as symlinks by Go. Never follow repository links outside the inventory.
func (w Workspace) safe(p string) (string, error) {
	if p == "" || path.IsAbs(p) || strings.Contains(p, "\\") || p == ".." || strings.HasPrefix(p, "../") || path.Clean(p) != p {
		return "", fmt.Errorf("unsafe_path")
	}
	cur := w.Root
	for _, part := range strings.Split(p, "/") {
		cur = filepath.Join(cur, part)
		s, e := os.Lstat(cur)
		if e != nil {
			return "", e
		}
		if s.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink")
		}
	}
	return cur, nil
}

var secrets = []*regexp.Regexp{regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`), regexp.MustCompile(`(?:ghp_|github_pat_)[A-Za-z0-9_]{16,}`), regexp.MustCompile(`(?i)(?:api[_-]?key|password|secret)\s*[:=]\s*["'][^\s"']{12,}["']`)}

func (w Workspace) Read(p string) ([]byte, os.FileInfo, error) {
	if w.excluded(p) {
		return nil, nil, fmt.Errorf("excluded")
	}
	f, e := w.safe(p)
	if e != nil {
		return nil, nil, e
	}
	s, e := os.Stat(f)
	if e != nil {
		return nil, nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > 2<<20 {
		return nil, s, fmt.Errorf("not_regular_or_over_2MiB")
	}
	b, e := os.ReadFile(f)
	if e != nil {
		return nil, s, e
	}
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return nil, s, fmt.Errorf("binary")
	}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(line) > 16384 {
			return nil, s, fmt.Errorf("oversized_line")
		}
	}
	for _, rx := range secrets {
		if rx.Match(b) {
			return nil, s, fmt.Errorf("secret_pattern")
		}
	}
	return b, s, nil
}
func (w Workspace) Current(c Chunk) bool {
	b, _, e := w.Read(c.Path)
	return e == nil && ID(string(b)) == c.Hash
}
