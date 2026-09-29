package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func tokenize(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1]))) {
			b.WriteByte(' ')
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(unicode.ToLower(c))
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func queryTerms(q string) []string {
	stop := " a an the what how when where does do is are to of in on for and or with happens which from can explain please "
	out := []string{}
	seen := map[string]bool{}
	for _, t := range strings.Fields(tokenize(q)) {
		if !strings.Contains(stop, " "+t+" ") && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	return out
}

type Hit struct {
	Kind    string       `json:"kind"`
	ID      string       `json:"id"`
	Reason  string       `json:"match_reason"`
	Source  *Chunk       `json:"source,omitempty"`
	Claim   *StoredClaim `json:"knowledge,omitempty"`
	Support []Chunk      `json:"support,omitempty"`
}

func (s *Store) Search(q string, limit int) (Envelope, error) {
	fresh, err := s.Audit()
	if err != nil {
		return Envelope{}, err
	}
	terms := queryTerms(q)
	r := s.Envelope("no_match", []Hit{})
	if len(terms) == 0 {
		return r, nil
	}
	if len(terms) > 32 {
		return r, fmt.Errorf("invalid_query: at most 32 significant terms")
	}
	fts := []string{}
	for _, t := range terms {
		fts = append(fts, "\""+t+"\"")
	}
	rows, e := s.Q.Query("SELECT id,kind FROM search WHERE search MATCH ? ORDER BY bm25(search),id LIMIT 200", strings.Join(fts, " AND "))
	if e != nil {
		return r, e
	}
	type candidate struct{ id, kind string }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.id, &c.kind); e != nil {
			rows.Close()
			return r, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	exactRows, e := s.Q.Query("SELECT id FROM chunks WHERE path=? OR json_extract(payload,'$.symbol')=? ORDER BY path,id LIMIT 20", q, q)
	if e != nil {
		return r, e
	}
	exactCandidates := []candidate{}
	exactIDs := map[string]bool{}
	for exactRows.Next() {
		var id string
		if e = exactRows.Scan(&id); e != nil {
			exactRows.Close()
			return r, e
		}
		exactCandidates = append(exactCandidates, candidate{id, "source"})
		exactIDs[id] = true
	}
	e = exactRows.Err()
	exactRows.Close()
	if e != nil {
		return r, e
	}
	for _, c := range candidates {
		if !exactIDs[c.id] {
			exactCandidates = append(exactCandidates, c)
		}
	}
	candidates = exactCandidates
	hits := []Hit{}
	usedBytes := 0
	stale := false
	for _, c := range candidates {
		h := Hit{ID: c.id, Kind: c.kind}
		var text string
		if c.kind == "source" {
			chunk, er := s.Chunk(c.id)
			if er != nil {
				return r, er
			}
			if !s.W.Current(chunk) {
				stale = true
				continue
			}
			text = chunk.Path + " " + chunk.Symbol + " " + chunk.Text
			h.Source = &chunk
		} else {
			claim, er := s.claim(c.id)
			if er != nil {
				return r, er
			}
			if claim.Stale || !s.claimCurrent(claim) {
				stale = true
				continue
			}
			h.Claim = &claim
			text = claim.Claim.Text + " " + strings.Join(claim.Claim.Conditions, " ")
			for _, id := range claim.Claim.Evidence {
				chunk, er := s.Chunk(id)
				if er != nil {
					return r, er
				}
				h.Support = append(h.Support, chunk)
			}
		}
		tokens := " " + tokenize(text) + " "
		matched := 0
		for _, t := range terms {
			if strings.Contains(tokens, " "+t+" ") {
				matched++
			}
		}
		exact := h.Source != nil && (strings.EqualFold(q, h.Source.Symbol) || strings.EqualFold(q, h.Source.Path))
		if matched != len(terms) && !exact {
			continue
		}
		cost := len(encode(h))
		if usedBytes+cost > 65536 {
			r.Truncated = true
			continue
		}
		usedBytes += cost
		h.Reason = "all_significant_terms"
		if exact {
			h.Reason = "exact_symbol_or_path"
		}
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Reason == "exact_symbol_or_path" && hits[j].Reason != "exact_symbol_or_path"
	})
	if len(hits) > limit {
		hits = hits[:limit]
		r.Truncated = true
	}
	r.Data = hits
	if len(hits) > 0 {
		r.State = "matched"
	}
	if stale {
		r.Reasons = append(r.Reasons, "stale_candidates_removed")
		if len(hits) > 0 {
			r.State = "partial"
		} else {
			r.State = "stale"
		}
	}
	if len(hits) == 0 {
		r.Reasons = append(r.Reasons, "direct_source_investigation_required")
	}
	if len(candidates) == 200 {
		r.Truncated = true
		r.Reasons = append(r.Reasons, "candidate_budget_reached")
	}
	if r.Truncated {
		r.Reasons = append(r.Reasons, "result_budget_reached")
	}
	if r.Truncated && len(hits) > 0 {
		r.State = "partial"
	}
	if !fresh {
		r.Reasons = append(r.Reasons, "index_inputs_changed: run update")
		if len(hits) > 0 {
			r.State = "partial"
		} else {
			r.State = "stale"
		}
	}
	r.Warnings = append(r.Warnings, "Lexical eligibility requires all significant query terms; this is not semantic confidence. Refine with discovered identifiers or investigate source directly.")
	return r, nil
}

type Visit struct {
	ID     string `json:"id"`
	Depth  int    `json:"depth"`
	Offset int    `json:"edge_offset,omitempty"`
}
type Cursor struct {
	Generation string   `json:"generation"`
	Root       string   `json:"root"`
	Direction  string   `json:"direction"`
	Kinds      string   `json:"kinds"`
	MaxDepth   int      `json:"max_depth"`
	Queue      []Visit  `json:"queue"`
	Seen       []string `json:"seen"`
}
type Traversal struct {
	Sources  []Chunk `json:"sources"`
	Edges    []Edge  `json:"relationships"`
	Frontier []Visit `json:"frontier"`
	Cursor   string  `json:"cursor,omitempty"`
}

func (s *Store) Evidence(id, dir, kinds string, depth, nodes, bytes int, continuation string) (Envelope, error) {
	r := s.Envelope("matched", nil)
	fresh, err := s.Audit()
	if err != nil {
		return r, err
	}
	if !fresh {
		r.State = "stale"
		r.Reasons = append(r.Reasons, "index_inputs_changed: run update before traversal")
		return r, nil
	}
	c := Cursor{Generation: s.Meta("generation"), Root: id, Direction: dir, Kinds: kinds, MaxDepth: depth, Queue: []Visit{{ID: id, Depth: 0}}, Seen: []string{}}
	if continuation != "" {
		if len(continuation) != 64 || strings.Trim(continuation, "0123456789abcdef") != "" {
			return r, fmt.Errorf("invalid_cursor")
		}
		cursorPath, e := s.W.safe(".onboard/cache/cursors/" + continuation + ".json")
		if e != nil {
			return r, fmt.Errorf("cursor_expired: run a new evidence request")
		}
		b, e := os.ReadFile(cursorPath)
		if e != nil || len(b) > 4<<20 || ID(string(b)) != continuation {
			return r, fmt.Errorf("invalid_cursor")
		}
		if e = json.Unmarshal(b, &c); e != nil {
			return r, e
		}
		if c.Generation != s.Meta("generation") || c.Root != id {
			return r, fmt.Errorf("stale_cursor")
		}
		if c.MaxDepth < 0 || c.MaxDepth > 8 || len(c.Seen) > 10000 || len(c.Queue) > 10000 {
			return r, fmt.Errorf("invalid_cursor")
		}
	}
	seen := map[string]bool{}
	for _, id := range c.Seen {
		seen[id] = true
	}
	out := Traversal{Sources: []Chunk{}, Edges: []Edge{}, Frontier: []Visit{}}
	used := 0
	for len(c.Queue) > 0 && len(out.Sources) < nodes {
		v := c.Queue[0]
		if seen[v.ID] {
			c.Queue = c.Queue[1:]
			continue
		}
		chunk, e := s.Chunk(v.ID)
		if e != nil {
			return r, fmt.Errorf("unknown_evidence: %s", v.ID)
		}
		if !s.W.Current(chunk) {
			r.State = "stale"
			r.Reasons = append(r.Reasons, "source_hash_mismatch")
			return r, nil
		}
		if used+len(chunk.Text) > bytes {
			break
		}
		c.Queue = c.Queue[1:]
		out.Sources = append(out.Sources, chunk)
		used += len(chunk.Text)
		if v.Depth >= c.MaxDepth {
			seen[v.ID] = true
			c.Seen = append(c.Seen, v.ID)
			out.Frontier = append(out.Frontier, v)
			continue
		}
		query := "SELECT payload FROM edges WHERE src=? OR dst=? ORDER BY kind,id"
		args := []any{v.ID, v.ID}
		if c.Direction == "outgoing" {
			query = "SELECT payload FROM edges WHERE src=? ORDER BY kind,id"
			args = []any{v.ID}
		} else if c.Direction == "incoming" {
			query = "SELECT payload FROM edges WHERE dst=? ORDER BY kind,id"
			args = []any{v.ID}
		}
		query += " LIMIT 513 OFFSET ?"
		args = append(args, v.Offset)
		rows, e := s.Q.Query(query, args...)
		if e != nil {
			return r, e
		}
		consumed := 0
		paused := false
		for rows.Next() {
			if len(out.Edges) >= 512 || consumed >= 512 {
				paused = true
				break
			}
			consumed++
			var raw string
			var edge Edge
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return r, e
			}
			if e = json.Unmarshal([]byte(raw), &edge); e != nil {
				rows.Close()
				return r, e
			}
			if c.Kinds != "" && !strings.Contains(","+c.Kinds+",", ","+edge.Kind+",") {
				continue
			}
			out.Edges = append(out.Edges, edge)
			next := edge.To
			if next == v.ID {
				next = edge.From
			}
			if next != "" && !seen[next] {
				c.Queue = append(c.Queue, Visit{ID: next, Depth: v.Depth + 1})
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return r, e
		}
		if paused {
			v.Offset += consumed
			c.Queue = append([]Visit{v}, c.Queue...)
			r.Truncated = true
			r.Reasons = append(r.Reasons, "relationship_budget_reached")
			break
		}
		seen[v.ID] = true
		c.Seen = append(c.Seen, v.ID)
	}
	if len(out.Frontier) > 0 {
		r.Truncated = true
		r.State = "partial"
		r.Reasons = append(r.Reasons, "depth_boundary: inspect frontier with a new evidence request")
	}
	out.Frontier = append(out.Frontier, c.Queue...)
	if len(c.Queue) > 0 {
		r.Truncated = true
		r.State = "partial"
		if len(c.Seen) < 10000 && len(c.Queue) < 10000 {
			out.Cursor, err = s.saveCursor(c)
			if err != nil {
				return r, err
			}
		}
	}
	if len(out.Sources) == 0 && len(c.Queue) > 0 {
		r.Reasons = append(r.Reasons, "increase_source_byte_budget")
	}
	r.Data = out
	return r, nil
}
func (s *Store) saveCursor(c Cursor) (string, error) {
	dir := filepath.Join(s.W.Root, ".onboard", "cache", "cursors")
	if _, e := s.W.safe(".onboard/cache/cursors"); e != nil && !os.IsNotExist(e) {
		return "", e
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	b := []byte(encode(c))
	id := ID(string(b))
	if e := atomicFile(filepath.Join(dir, id+".json"), b); e != nil {
		return "", e
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return "", e
	}
	if len(entries) > 128 {
		sort.Slice(entries, func(i, j int) bool {
			a, ea := entries[i].Info()
			b, eb := entries[j].Info()
			if ea != nil || eb != nil {
				return entries[i].Name() < entries[j].Name()
			}
			return a.ModTime().Before(b.ModTime())
		})
		for _, entry := range entries[:len(entries)-128] {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	return id, nil
}
func (s *Store) Status() (Envelope, error) {
	fresh, auditErr := s.Audit()
	if auditErr != nil {
		return Envelope{}, auditErr
	}
	files, e := s.Files()
	if e != nil {
		return Envelope{}, e
	}
	paths, e := s.W.Paths()
	if e != nil {
		return Envelope{}, e
	}
	changed := 0
	excluded := 0
	syntax := 0
	for _, p := range paths {
		f, ok := files[p]
		if !ok {
			changed++
			continue
		}
		if f.Reason != "" {
			excluded++
			continue
		}
		b, _, err := s.W.Read(p)
		if err != nil || ID(string(b)) != f.Hash {
			changed++
		}
		if f.Syntax == "go_ast" {
			syntax++
		}
	}
	present := map[string]bool{}
	for _, p := range paths {
		present[p] = true
	}
	for p := range files {
		if !present[p] {
			changed++
		}
	}
	if s.W.ConfigHash != s.Meta("config") {
		changed++
	}
	state := "current"
	if changed > 0 || !fresh {
		state = "stale"
	}
	claims, e := s.allClaims()
	if e != nil {
		return Envelope{}, e
	}
	current := 0
	for _, c := range claims {
		if !c.Stale && s.claimCurrent(c) {
			current++
		}
	}
	var gaps []string
	json.Unmarshal([]byte(s.Meta("capability_gaps")), &gaps)
	r := s.Envelope(state, map[string]any{"changed_files": changed, "inventory_files": len(files), "excluded_files": excluded, "go_syntax_files": syntax, "current_claims": current, "stale_claims": len(claims) - current, "head": s.Meta("head"), "verified_at": s.Meta("verified_at"), "verification": s.Meta("verification"), "embeddings": "not_implemented_phase_7", "capability_gaps": gaps})
	return r, nil
}
