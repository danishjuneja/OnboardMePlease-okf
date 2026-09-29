package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strings"
	"time"
)

func parseFile(p string, b []byte) File {
	f := File{Path: p, Hash: ID(string(b)), Syntax: "text_only", Extractor: Version}
	text := string(b)
	lines := strings.SplitAfter(text, "\n")
	kind := "implementation"
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".rst", ".txt":
		kind = "documentation"
	case ".json", ".yaml", ".yml", ".toml", ".sql":
		kind = "configuration"
	}
	if strings.HasSuffix(p, "_test.go") || strings.Contains(p, ".test.") || strings.Contains(p, "/test/") {
		kind = "test"
	}
	generated := strings.Contains(text, "Code generated") || strings.Contains(text, "DO NOT EDIT")
	cuts := map[int]string{1: ""}
	signatures := map[int]string{}
	if strings.HasSuffix(p, ".go") {
		fs := token.NewFileSet()
		a, e := parser.ParseFile(fs, p, b, parser.ParseComments)
		if e == nil {
			f.Syntax = "go_ast"
			for _, d := range a.Decls {
				start := fs.Position(d.Pos()).Line
				name := ""
				switch n := d.(type) {
				case *ast.FuncDecl:
					name = n.Name.Name
					if n.Recv != nil {
						var recv bytes.Buffer
						printer.Fprint(&recv, fs, n.Recv.List[0].Type)
						name = recv.String() + "." + name
					}
					if n.Doc != nil {
						start = fs.Position(n.Doc.Pos()).Line
					}
					var sig bytes.Buffer
					printer.Fprint(&sig, fs, n.Type)
					signatures[start] = sig.String()
				case *ast.GenDecl:
					if n.Doc != nil {
						start = fs.Position(n.Doc.Pos()).Line
					}
				}
				cuts[start] = name
			}
		} else {
			f.Syntax = "go_parse_error"
		}
	}
	starts := []int{}
	for n := range cuts {
		starts = append(starts, n)
	}
	sort.Ints(starts)
	starts = append(starts, len(lines)+1)
	for i := 0; i < len(starts)-1; i++ {
		end := starts[i+1] - 1
		for start := starts[i]; start <= end; {
			last := start
			size := 0
			for last <= end && last-start < 80 && size+len(lines[last-1]) <= 16384 {
				size += len(lines[last-1])
				last++
			}
			if last == start {
				last++
			}
			snippet := strings.Join(lines[start-1:last-1], "")
			if strings.TrimSpace(snippet) != "" {
				c := Chunk{Path: p, Hash: f.Hash, Start: start, End: last - 1, Kind: kind, Symbol: cuts[starts[i]], Text: snippet, Generated: generated}
				if c.Symbol != "" {
					c.Signature = signatures[starts[i]]
					c.SymbolID = ID("symbol", p, c.Symbol, c.Signature)
				}
				c.ID = ID(Version, p, f.Hash, fmt.Sprint(c.Start), fmt.Sprint(c.End))
				f.Chunks = append(f.Chunks, c)
			}
			start = last
		}
	}
	return f
}
func manifest(files map[string]File, config string) string {
	keys := []string{}
	for p := range files {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	parts := []string{config}
	for _, p := range keys {
		f := files[p]
		parts = append(parts, p, f.Hash, fmt.Sprint(f.Mode), f.Reason)
	}
	return ID(parts...)
}
func makeUnits(files map[string]File) []Unit {
	paths := []string{}
	for p, f := range files {
		if len(f.Chunks) > 0 {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	out := []Unit{}
	var u Unit
	anchors := []string{}
	size := 0
	flush := func() {
		if len(u.Evidence) == 0 {
			return
		}
		u.ID = ID("unit", strings.Join(anchors, "\x00"))
		parts := []string{}
		for _, p := range u.Paths {
			parts = append(parts, p, files[p].Hash)
		}
		u.Fingerprint = ID(parts...)
		u.Reason = "unreviewed_or_changed"
		out = append(out, u)
		u = Unit{}
		anchors = nil
		size = 0
	}
	dir := ""
	for _, p := range paths {
		if path.Dir(p) != dir {
			flush()
		}
		dir = path.Dir(p)
		for _, c := range files[p].Chunks {
			if len(u.Evidence) >= 40 || size+c.End-c.Start+1 > 800 {
				flush()
			}
			if len(u.Paths) == 0 || u.Paths[len(u.Paths)-1] != p {
				u.Paths = append(u.Paths, p)
			}
			anchors = append(anchors, p+":"+fmt.Sprint(c.Start))
			u.Evidence = append(u.Evidence, c.ID)
			size += c.End - c.Start + 1
		}
	}
	flush()
	return out
}
func (s *Store) Update(verify bool) (Envelope, error) {
	unlock, e := s.Lock()
	if e != nil {
		return Envelope{}, e
	}
	defer unlock()
	old, e := s.Files()
	if e != nil {
		return Envelope{}, e
	}
	paths, e := s.W.Paths()
	if e != nil {
		return Envelope{}, e
	}
	head := s.W.Head()
	files := map[string]File{}
	changed, reused := 0, 0
	for _, p := range paths {
		b, st, err := s.W.Read(p)
		if err != nil {
			files[p] = File{Path: p, Reason: sourceReason(err), Syntax: "excluded"}
			continue
		}
		hash := ID(string(b))
		f, ok := old[p]
		if ok && f.Hash == hash && f.Extractor == Version && s.Meta("config") == s.W.ConfigHash {
			reused++
		} else {
			var cached string
			if s.Q.QueryRow("SELECT payload FROM stages WHERE path=?", p).Scan(&cached) == nil {
				var stage File
				json.Unmarshal([]byte(cached), &stage)
				if stage.Hash == hash && stage.Extractor == Version {
					f = stage
				} else {
					f = parseFile(p, b)
				}
			} else {
				f = parseFile(p, b)
			}
			changed++
		}
		f.Size = st.Size()
		f.Mtime = st.ModTime().UnixNano()
		f.Mode = uint32(st.Mode())
		if !ok || f.Hash != old[p].Hash {
			if _, e = s.DB.Exec("INSERT OR REPLACE INTO stages VALUES (?,?)", p, encode(f)); e != nil {
				return Envelope{}, e
			}
		}
		for i := range f.Chunks {
			f.Chunks[i].Text = ""
		}
		files[p] = f
	}
	generation := manifest(files, s.W.ConfigHash)
	edges := []Edge{}
	warnings := []string{}
	if generation != s.Meta("generation") {
		edges, warnings = extractGraph(s.W, files)
	}
	// A second complete content audit prevents publishing mixed inputs while an
	// editor, branch switch or generator is changing the tree.
	again, e := s.W.Paths()
	if e != nil {
		return Envelope{}, e
	}
	if strings.Join(paths, "\x00") != strings.Join(again, "\x00") || head != s.W.Head() {
		return Envelope{}, fmt.Errorf("tree_changed: rerun update; parsed checkpoints retained")
	}
	for p, f := range files {
		b, _, er := s.W.Read(p)
		if f.Reason == "" && (er != nil || ID(string(b)) != f.Hash) {
			return Envelope{}, fmt.Errorf("tree_changed: %s", p)
		}
		if f.Reason != "" && er == nil {
			return Envelope{}, fmt.Errorf("tree_changed: %s", p)
		}
	}
	if generation == s.Meta("generation") {
		_, e = s.DB.Exec("INSERT OR REPLACE INTO meta VALUES ('verified_at',?),('head',?)", time.Now().UTC().Format(time.RFC3339), head)
		r := s.Envelope("current", map[string]any{"changed_files": 0, "reused_files": reused, "verification": "full_hash", "requested_verify": verify})
		if e == nil {
			e = s.restoreDocuments()
		}
		return r, e
	}
	claims, e := s.allClaims()
	if e != nil {
		return Envelope{}, e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return Envelope{}, e
	}
	defer tx.Rollback()
	run := func(q string, a ...any) error { _, er := tx.Exec(q, a...); return er }
	for p, f := range old {
		n, ok := files[p]
		if ok && f.Hash == n.Hash && f.Reason == n.Reason && f.Mode == n.Mode && f.Extractor == n.Extractor {
			continue
		}
		if e = run("DELETE FROM search WHERE id IN (SELECT id FROM chunks WHERE path=?)", p); e != nil {
			return Envelope{}, e
		}
		if e = run("DELETE FROM chunks WHERE path=?", p); e != nil {
			return Envelope{}, e
		}
		if e = run("DELETE FROM files WHERE path=?", p); e != nil {
			return Envelope{}, e
		}
	}
	for p, f := range files {
		if oldf, ok := old[p]; ok && oldf.Hash == f.Hash && oldf.Reason == f.Reason && oldf.Mode == f.Mode && oldf.Extractor == f.Extractor {
			continue
		}
		if f.Reason == "" {
			b, _, err := s.W.Read(p)
			if err != nil || ID(string(b)) != f.Hash {
				return Envelope{}, fmt.Errorf("tree_changed: %s", p)
			}
			lines := strings.SplitAfter(string(b), "\n")
			for i := range f.Chunks {
				c := &f.Chunks[i]
				c.Text = strings.Join(lines[c.Start-1:c.End], "")
			}
		}
		if e = run("INSERT OR REPLACE INTO files VALUES (?,?)", p, encode(f)); e != nil {
			return Envelope{}, e
		}
		for _, c := range f.Chunks {
			if e = run("INSERT OR REPLACE INTO chunks VALUES (?,?,?)", c.ID, p, encode(c)); e != nil {
				return Envelope{}, e
			}
			if e = run("INSERT INTO search VALUES (?,?,?)", c.ID, "source", tokenize(c.Path+" "+c.Symbol+" "+c.Text)); e != nil {
				return Envelope{}, e
			}
		}
	}
	if e = run("DELETE FROM edges"); e != nil {
		return Envelope{}, e
	}
	for _, edge := range edges {
		if e = run("INSERT INTO edges VALUES (?,?,?,?,?)", edge.ID, edge.From, edge.To, edge.Kind, encode(edge)); e != nil {
			return Envelope{}, e
		}
	}
	if e = run("DELETE FROM units"); e != nil {
		return Envelope{}, e
	}
	units := makeUnits(files)
	for _, u := range units {
		if e = run("INSERT INTO units VALUES (?,?)", u.ID, encode(u)); e != nil {
			return Envelope{}, e
		}
	}
	for _, c := range claims {
		c.Stale = c.Configuration != s.W.ConfigHash || !dependenciesMatch(c.Dependencies, files)
		if e = run("UPDATE claims SET payload=? WHERE id=?", encode(c), c.Claim.ID); e != nil {
			return Envelope{}, e
		}
		if e = run("DELETE FROM search WHERE id=? AND kind='claim'", c.Claim.ID); e != nil {
			return Envelope{}, e
		}
		if !c.Stale {
			if e = run("INSERT INTO search VALUES (?,?,?)", c.Claim.ID, "claim", tokenize(c.Claim.Text+" "+strings.Join(c.Claim.Conditions, " "))); e != nil {
				return Envelope{}, e
			}
		}
	}
	for k, v := range map[string]string{"generation": generation, "config": s.W.ConfigHash, "head": head, "verified_at": time.Now().UTC().Format(time.RFC3339), "verification": "full_hash", "capability_gaps": encode(warnings)} {
		if e = run("INSERT OR REPLACE INTO meta VALUES (?,?)", k, v); e != nil {
			return Envelope{}, e
		}
	}
	if e = run("DELETE FROM stages"); e != nil {
		return Envelope{}, e
	}
	if e = tx.Commit(); e != nil {
		return Envelope{}, e
	}
	if e = s.restoreDocuments(); e != nil {
		return Envelope{}, e
	}
	r := s.Envelope("current", map[string]any{"changed_files": changed, "reused_files": reused, "inventory_files": len(files), "units": len(units), "relationships": len(edges), "verification": "full_hash"})
	r.Warnings = warnings
	return r, nil
}
