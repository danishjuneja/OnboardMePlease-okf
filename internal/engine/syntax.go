package engine

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed syntax.py
var syntaxScript string

type syntaxDeclaration struct {
	Line int    `json:"line"`
	Name string `json:"name"`
}
type syntaxCall struct {
	Line int    `json:"line"`
	Name string `json:"name"`
}
type syntaxRequest struct {
	Line   int    `json:"line"`
	Method string `json:"method"`
	Route  string `json:"route"`
}
type syntaxEvent struct {
	Line  int    `json:"line"`
	Topic string `json:"topic"`
}
type syntaxAnalysis struct {
	Syntax       string              `json:"syntax"`
	Declarations []syntaxDeclaration `json:"declarations"`
	Calls        []syntaxCall        `json:"calls"`
	Requests     []syntaxRequest     `json:"requests"`
	Events       []syntaxEvent       `json:"events"`
}
type syntaxProbe struct {
	Python       string                     `json:"python"`
	TreeSitter   string                     `json:"tree_sitter"`
	Grammars     map[string]json.RawMessage `json:"grammars"`
	Dependencies []string                   `json:"dependencies"`
	Digests      map[string]string          `json:"digests"`
}

type syntaxDependencyStamp struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Mode     uint32 `json:"mode"`
}

type syntaxProbeCache struct {
	Key    string                  `json:"key"`
	Probe  syntaxProbe             `json:"probe"`
	Stamps []syntaxDependencyStamp `json:"stamps"`
}

func syntaxDependencyStamps(paths []string) ([]syntaxDependencyStamp, error) {
	if len(paths) == 0 || len(paths) > 4096 {
		return nil, fmt.Errorf("syntax_probe_invalid_dependencies")
	}
	stamps := make([]syntaxDependencyStamp, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) || strings.HasPrefix(filepath.ToSlash(p), "//") {
			return nil, fmt.Errorf("syntax_probe_invalid_dependency")
		}
		stamp := syntaxDependencyStamp{Path: p}
		info, err := os.Stat(p)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			stamp.Exists, stamp.Size, stamp.Modified, stamp.Mode = true, info.Size(), info.ModTime().UnixNano(), uint32(info.Mode())
		}
		stamps = append(stamps, stamp)
	}
	return stamps, nil
}

func syntaxDependenciesCurrent(stamps []syntaxDependencyStamp) bool {
	paths := make([]string, len(stamps))
	for i, stamp := range stamps {
		paths[i] = stamp.Path
	}
	current, err := syntaxDependencyStamps(paths)
	if err != nil {
		return false
	}
	for i := range stamps {
		if current[i] != stamps[i] {
			return false
		}
	}
	return true
}

type syntaxInput struct {
	Path     string         `json:"path"`
	Source   []byte         `json:"source,omitempty"`
	Analysis syntaxAnalysis `json:"analysis,omitempty"`
}

type boundedSyntaxOutput struct{ bytes.Buffer }

func (b *boundedSyntaxOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 32<<20 {
		return 0, fmt.Errorf("syntax_adapter_output_too_large")
	}
	return b.Buffer.Write(p)
}

// The interpreter is an explicit user-selected dependency. Isolated mode keeps
// the target repository off Python's import path; source is always JSON data.
func syntaxCommand(python string, input []byte, probe bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := []string{"-I", "-B", "-c", syntaxScript}
	if probe {
		args = append(args, "--probe")
	}
	cmd := exec.CommandContext(ctx, python, args...)
	cmd.Stdin = bytes.NewReader(input)
	var output boundedSyntaxOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("syntax_adapter_failed: %w", err)
	}
	return output.Bytes(), nil
}

func configureSyntax(w Workspace, verify bool) (string, syntaxProbe, string, error) {
	python := os.Getenv("OMP_SYNTAX_PYTHON")
	if python == "" {
		return "", syntaxProbe{}, "disabled", nil
	}
	resolved, err := exec.LookPath(python)
	if err != nil {
		return "", syntaxProbe{}, "", fmt.Errorf("syntax_python_unavailable: %w", err)
	}
	cacheKey := ID(resolved, syntaxScript)
	cachePath := filepath.Join(w.Root, ".onboard", "cache", "syntax-probe.json")
	if _, err := w.safe(".onboard/cache/syntax-probe.json"); !verify && err == nil {
		if info, err := os.Stat(cachePath); err == nil && info.Mode().IsRegular() && info.Size() <= 2<<20 {
			if raw, err := os.ReadFile(cachePath); err == nil {
				var cached syntaxProbeCache
				if json.Unmarshal(raw, &cached) == nil && cached.Key == cacheKey && cached.Probe.TreeSitter != "" && len(cached.Stamps) == len(cached.Probe.Dependencies) {
					matching := true
					for i, stamp := range cached.Stamps {
						if stamp.Path != cached.Probe.Dependencies[i] {
							matching = false
							break
						}
					}
					if matching && syntaxDependenciesCurrent(cached.Stamps) {
						return resolved, cached.Probe, ID(resolved, syntaxScript, encode(cached.Probe)), nil
					}
				}
			}
		}
	}
	raw, err := syntaxCommand(resolved, nil, true)
	if err != nil {
		return "", syntaxProbe{}, "", fmt.Errorf("syntax_probe_failed: install tree-sitter packages: %w", err)
	}
	var p syntaxProbe
	if err = json.Unmarshal(raw, &p); err != nil || p.TreeSitter == "" {
		return "", syntaxProbe{}, "", fmt.Errorf("syntax_probe_invalid")
	}
	if stamps, err := syntaxDependencyStamps(p.Dependencies); err == nil {
		if _, err = w.safe(".onboard/cache"); err == nil {
			if _, err = w.safe(".onboard/cache/syntax-probe.json"); err == nil || os.IsNotExist(err) {
				_ = atomicFile(cachePath, []byte(encode(syntaxProbeCache{Key: cacheKey, Probe: p, Stamps: stamps})))
			}
		}
	}
	return resolved, p, ID(resolved, syntaxScript, encode(p)), nil
}

func syntaxSupported(p syntaxProbe, file string) bool {
	_, ok := p.Grammars[strings.ToLower(path.Ext(file))]
	return ok
}

func analyzeSyntax(python string, inputs []syntaxInput) (map[string]syntaxAnalysis, error) {
	out := map[string]syntaxAnalysis{}
	for len(inputs) > 0 {
		n, size := 0, 0
		for n < len(inputs) && n < 32 && (n == 0 || size+len(inputs[n].Source) <= 8<<20) {
			size += len(inputs[n].Source)
			n++
		}
		raw, err := json.Marshal(inputs[:n])
		if err != nil {
			return nil, err
		}
		result, err := syntaxCommand(python, raw, false)
		if err != nil {
			return nil, err
		}
		var parsed []syntaxInput
		if err = json.Unmarshal(result, &parsed); err != nil || len(parsed) != n {
			return nil, fmt.Errorf("syntax_adapter_invalid_output")
		}
		for i, item := range parsed {
			if item.Path != inputs[i].Path || item.Analysis.Syntax == "" {
				return nil, fmt.Errorf("syntax_adapter_invalid_output")
			}
			out[item.Path] = item.Analysis
		}
		inputs = inputs[n:]
	}
	return out, nil
}

var routeParameter = regexp.MustCompile(`\{[^/{}]*\}`)

func routeShape(route string) string {
	if !strings.HasPrefix(route, "/") || strings.HasPrefix(route, "//") || strings.ContainsAny(route, "?#") {
		return ""
	}
	return routeParameter.ReplaceAllString(route, "{}")
}
