package main

import (
	"encoding/json"
	"fmt"
	"onboardmeplease/internal/engine"
	"os"
	"strconv"
	"strings"
)

const help = `omp — local repository evidence for your agent

  omp update [--verify] [--json]
  omp status [--json]
  omp search "query" [--limit 10] [--json]
  omp evidence <id> [--direction both|incoming|outgoing] [--kinds calls,references,imports,next]
      [--depth 1] [--nodes 20] [--bytes 65536] [--cursor <cursor>] [--json]
  omp knowledge pending [--limit 20] [--cursor <cursor>] [--json]
  omp knowledge apply <result.json> [--dry-run] [--json]

Run from any directory in a Git working tree. No model, API, server or background process.
`

func main() {
	args := os.Args[1:]
	jsonMode := false
	flags := map[string]string{}
	pos := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			jsonMode = true
			continue
		}
		if a == "--help" || a == "-h" {
			fmt.Print(help)
			return
		}
		if strings.HasPrefix(a, "--") {
			if a == "--verify" || a == "--dry-run" {
				flags[a] = "true"
			} else {
				if i+1 >= len(args) {
					fail(jsonMode, "invalid_arguments: missing flag value")
				}
				i++
				flags[a] = args[i]
			}
		} else {
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		fmt.Print(help)
		return
	}
	r, e := run(pos, flags)
	if e != nil {
		fail(jsonMode, e.Error())
	}
	if jsonMode {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		if e = enc.Encode(r); e != nil {
			fail(false, e.Error())
		}
	} else {
		fmt.Printf("%s (generation %s)\n", r.State, r.Generation)
		b, _ := json.MarshalIndent(r.Data, "", "  ")
		fmt.Println(string(b))
		for _, w := range r.Warnings {
			fmt.Fprintln(os.Stderr, w)
		}
	}
}
func fail(js bool, msg string) {
	if js {
		json.NewEncoder(os.Stdout).Encode(engine.Envelope{Schema: 1, State: "unavailable", Reasons: []string{strings.SplitN(msg, ":", 2)[0]}, Warnings: []string{}, Data: map[string]string{"error": msg}})
	}
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
func run(p []string, f map[string]string) (engine.Envelope, error) {
	empty := engine.Envelope{}
	cmd := p[0]
	if cmd == "knowledge" && len(p) > 1 {
		cmd += " " + p[1]
	}
	allowed := map[string]string{"update": "--verify", "status": "", "search": "--limit", "evidence": "--direction --kinds --depth --nodes --bytes --cursor", "knowledge pending": "--limit --cursor", "knowledge apply": "--dry-run"}
	opts, ok := allowed[cmd]
	if !ok {
		return empty, fmt.Errorf("invalid_command")
	}
	for k := range f {
		if !strings.Contains(" "+opts+" ", " "+k+" ") {
			return empty, fmt.Errorf("invalid_flag: %s", k)
		}
	}
	want := 1
	switch cmd {
	case "search", "evidence", "knowledge pending":
		want = 2
	case "knowledge apply":
		want = 3
	}
	if len(p) != want {
		return empty, fmt.Errorf("invalid_arguments: run omp --help")
	}
	num := func(key string, def, max int) (int, error) {
		if f[key] == "" {
			return def, nil
		}
		n, e := strconv.Atoi(f[key])
		if e != nil || n < 1 || n > max {
			return 0, fmt.Errorf("invalid_limit: %s must be 1..%d", key, max)
		}
		return n, nil
	}
	limit, e := num("--limit", 10, 100)
	if e != nil {
		return empty, e
	}
	w, e := engine.Discover()
	if e != nil {
		return empty, e
	}
	s, e := engine.Open(w, cmd == "update")
	if e != nil {
		return empty, e
	}
	defer s.DB.Close()
	if cmd == "update" { // Config may have been initialized by Open.
		w, e = engine.Discover()
		if e != nil {
			return empty, e
		}
		s.W = w
		r, e := s.Update(f["--verify"] != "")
		if e == nil {
			r.Warnings = append(r.Warnings, s.ImportPortable()...)
		}
		return r, e
	}
	if cmd != "knowledge apply" {
		tx, err := s.DB.Begin()
		if err != nil {
			return empty, err
		}
		defer tx.Rollback()
		s.Q = tx
	}
	if s.Meta("generation") == "" {
		return empty, fmt.Errorf("unavailable: run omp update")
	}
	if cmd != "status" && s.Meta("config") != s.W.ConfigHash {
		return empty, fmt.Errorf("stale_configuration: run omp update")
	}
	switch cmd {
	case "status":
		return s.Status()
	case "search":
		return s.Search(p[1], limit)
	case "evidence":
		depth, e := num("--depth", 1, 8)
		if e != nil {
			return empty, e
		}
		nodes, e := num("--nodes", 20, 200)
		if e != nil {
			return empty, e
		}
		bytes, e := num("--bytes", 65536, 1<<20)
		if e != nil {
			return empty, e
		}
		dir := f["--direction"]
		if dir == "" {
			dir = "both"
		}
		if dir != "both" && dir != "incoming" && dir != "outgoing" {
			return empty, fmt.Errorf("invalid_direction")
		}
		return s.Evidence(p[1], dir, f["--kinds"], depth, nodes, bytes, f["--cursor"])
	case "knowledge pending":
		return s.Pending(limit, f["--cursor"])
	case "knowledge apply":
		r, e := engine.ReadResult(p[2])
		if e != nil {
			return empty, e
		}
		return s.Apply(r, f["--dry-run"] != "")
	}
	return empty, fmt.Errorf("invalid_command")
}
