package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) allClaims() ([]StoredClaim, error) {
	rows, e := s.Q.Query("SELECT payload FROM claims ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []StoredClaim{}
	for rows.Next() {
		var b string
		var c StoredClaim
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) claim(id string) (StoredClaim, error) {
	var c StoredClaim
	var b string
	e := s.Q.QueryRow("SELECT payload FROM claims WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &c)
	}
	return c, e
}
func inScope(p, scope string) bool {
	return scope == "." || p == scope || strings.HasPrefix(p, strings.TrimSuffix(scope, "/")+"/")
}
func scopeHash(scope string, files map[string]File) string {
	parts := []string{}
	for p, f := range files {
		if inScope(p, scope) {
			parts = append(parts, p+"\x00"+f.Hash+"\x00"+f.Reason)
		}
	}
	sort.Strings(parts)
	return ID(parts...)
}
func dependenciesMatch(deps map[string]string, files map[string]File) bool {
	for k, h := range deps {
		if strings.HasPrefix(k, "scope:") {
			if scopeHash(strings.TrimPrefix(k, "scope:"), files) != h {
				return false
			}
		} else {
			f, ok := files[k]
			if !ok || f.Hash != h || f.Reason != "" {
				return false
			}
		}
	}
	return true
}
func (s *Store) claimCurrent(c StoredClaim) bool {
	if c.Configuration != s.W.ConfigHash {
		return false
	}
	for p, h := range c.Dependencies {
		if strings.HasPrefix(p, "scope:") {
			paths, e := s.W.Paths()
			if e != nil {
				return false
			}
			files := map[string]File{}
			scope := strings.TrimPrefix(p, "scope:")
			for _, p := range paths {
				if inScope(p, scope) {
					b, _, e := s.W.Read(p)
					f := File{Path: p, Hash: ID(string(b))}
					if e != nil {
						f.Hash = ""
						f.Reason = sourceReason(e)
					}
					files[p] = f
				}
			}
			if scopeHash(scope, files) != h {
				return false
			}
		} else {
			b, _, e := s.W.Read(p)
			if e != nil || ID(string(b)) != h {
				return false
			}
		}
	}
	return true
}
func (s *Store) Pending(limit int, cursor string) (Envelope, error) {
	fresh, err := s.Audit()
	if err != nil {
		return Envelope{}, err
	}
	if !fresh {
		r := s.Envelope("stale", map[string]any{"units": []Unit{}, "cursor": ""})
		r.Reasons = append(r.Reasons, "index_inputs_changed: run update")
		return r, nil
	}
	after := ""
	if cursor != "" {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) != 2 || parts[0] != s.Meta("generation") {
			return Envelope{}, fmt.Errorf("stale_cursor")
		}
		after = parts[1]
	}
	rows, e := s.Q.Query("SELECT payload FROM units WHERE id>? ORDER BY id", after)
	if e != nil {
		return Envelope{}, e
	}
	units := []Unit{}
	for rows.Next() {
		var b string
		var u Unit
		if e = rows.Scan(&b); e != nil {
			rows.Close()
			return Envelope{}, e
		}
		if e = json.Unmarshal([]byte(b), &u); e != nil {
			rows.Close()
			return Envelope{}, e
		}
		units = append(units, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return Envelope{}, e
	}
	docs, e := s.documents()
	if e != nil {
		return Envelope{}, e
	}
	covered := map[string]bool{}
	claims, err := s.allClaims()
	if err != nil {
		return Envelope{}, err
	}
	invalid := map[string]bool{}
	for _, c := range claims {
		if c.Stale || !s.claimCurrent(c) {
			invalid[c.Document] = true
		}
	}
	for docID, d := range docs {
		if invalid[docID] {
			continue
		}
		unresolved := false
		for _, v := range d.Facets {
			if v == "unresolved" {
				unresolved = true
			}
		}
		if unresolved {
			continue
		}
		if len(d.Unresolved) > 0 {
			continue
		}
		paths := append([]string{}, d.Reviewed...)
		sort.Strings(paths)
		for _, u := range units {
			if d.Unit == u.ID && d.Fingerprint == u.Fingerprint && strings.Join(paths, "\x00") == strings.Join(u.Paths, "\x00") {
				covered[u.ID] = true
			}
		}
	}
	out := []Unit{}
	next := ""
	for _, u := range units {
		if covered[u.ID] {
			continue
		}
		for id, d := range docs {
			overlap := false
			for _, p := range d.Reviewed {
				for _, up := range u.Paths {
					if p == up {
						overlap = true
					}
				}
			}
			if overlap {
				u.Prior = append(u.Prior, id)
				u.Reason = "review_incomplete"
				if d.Fingerprint != u.Fingerprint || invalid[id] {
					u.Reason = "dependencies_changed"
				}
			}
		}
		sort.Strings(u.Prior)
		if len(out) == limit {
			next = s.Meta("generation") + ":" + out[len(out)-1].ID
			break
		}
		out = append(out, u)
	}
	r := s.Envelope("current", map[string]any{"units": out, "cursor": next})
	r.Truncated = next != ""
	return r, nil
}
func ReadResult(p string) (Result, error) {
	var r Result
	f, e := os.Open(p)
	if e != nil {
		return r, e
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 2<<20))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&r); e != nil {
		return r, e
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return r, fmt.Errorf("invalid_result: trailing data")
	}
	return r, nil
}
func (s *Store) Apply(r Result, dry bool) (Envelope, error) {
	unlock, e := s.Lock()
	if e != nil {
		return Envelope{}, e
	}
	defer unlock()
	return s.apply(r, dry)
}
func (s *Store) apply(r Result, dry bool) (Envelope, error) {
	out := s.Envelope("current", nil)
	if r.Schema != 1 || r.Generation != s.Meta("generation") {
		return out, fmt.Errorf("stale_result: schema or generation mismatch")
	}
	var raw string
	var unit Unit
	if e := s.Q.QueryRow("SELECT payload FROM units WHERE id=?", r.Unit).Scan(&raw); e != nil {
		return out, fmt.Errorf("unknown_unit")
	}
	if e := json.Unmarshal([]byte(raw), &unit); e != nil {
		return out, e
	}
	if unit.Fingerprint != r.Fingerprint {
		return out, fmt.Errorf("stale_result: fingerprint mismatch")
	}
	if r.Title == "" || r.Assessment != "agent_assessed" || len(r.Reviewed) == 0 || len(r.Claims) > 100 || len(r.Claims) == 0 && len(r.Unresolved) == 0 {
		return out, fmt.Errorf("invalid_result: title, assessment, reviewed paths and findings required")
	}
	files, e := s.Files()
	if e != nil {
		return out, e
	}
	for _, p := range r.Reviewed {
		f, ok := files[p]
		if !ok || f.Reason != "" {
			return out, fmt.Errorf("invalid_reviewed_path: %s", p)
		}
	}
	intersects := false
	for _, p := range r.Reviewed {
		for _, u := range unit.Paths {
			if p == u {
				intersects = true
			}
		}
	}
	if !intersects {
		return out, fmt.Errorf("invalid_scope: review must include an anchor in its unit")
	}
	for _, p := range unit.Paths {
		b, _, er := s.W.Read(p)
		if er != nil || ID(string(b)) != files[p].Hash {
			return out, fmt.Errorf("stale_result: %s", p)
		}
	}
	required := []string{"entry_points", "conditions", "state_changes", "transactions", "downstream", "failures"}
	for _, f := range required {
		switch r.Facets[f] {
		case "supported", "unresolved", "not_applicable":
		default:
			return out, fmt.Errorf("invalid_facets: %s", f)
		}
	}
	sort.Strings(r.Reviewed)
	docID := ID("document", r.Unit, r.Title, strings.Join(r.Reviewed, "\x00"))
	stored := []StoredClaim{}
	ids := map[string]bool{}
	for _, c := range r.Claims {
		if len(c.Text) > 8192 || len(c.Evidence) > 32 || len(c.Relations) > 32 || len(encode(c)) > 32768 {
			return out, fmt.Errorf("claim_budget_exceeded")
		}
		if c.ID == "" || ids[c.ID] || strings.TrimSpace(c.Text) == "" || len(c.Evidence) == 0 {
			return out, fmt.Errorf("invalid_claim: unique id, text and evidence required")
		}
		ids[c.ID] = true
		if c.Kind != "source_fact" && c.Kind != "inference" && c.Kind != "negative" {
			return out, fmt.Errorf("invalid_claim_kind")
		}
		if c.Kind == "inference" && len(c.Assumptions) == 0 {
			return out, fmt.Errorf("inference_requires_assumptions")
		}
		if c.Kind == "negative" && len(c.Scopes) == 0 {
			return out, fmt.Errorf("negative_requires_dependency_scope")
		}
		sc := StoredClaim{Claim: c, Document: docID, Dependencies: map[string]string{}, Configuration: s.W.ConfigHash}
		sc.Claim.ID = ID(docID, c.ID)
		for _, conflict := range c.Conflicts {
			if _, er := s.claim(conflict); er != nil {
				return out, fmt.Errorf("unknown_conflicting_claim: %s", conflict)
			}
		}
		for _, id := range c.Evidence {
			chunk, er := s.Chunk(id)
			if er != nil || !s.W.Current(chunk) {
				return out, fmt.Errorf("stale_or_unknown_evidence: %s", id)
			}
			sc.Dependencies[chunk.Path] = chunk.Hash
		}
		for _, id := range c.Relations {
			var b string
			var edge Edge
			if er := s.Q.QueryRow("SELECT payload FROM edges WHERE id=?", id).Scan(&b); er != nil {
				return out, fmt.Errorf("unknown_relation: %s", id)
			}
			if er := json.Unmarshal([]byte(b), &edge); er != nil {
				return out, er
			}
			if edge.To == "" {
				return out, fmt.Errorf("unresolved_relation_is_not_connection_evidence")
			}
			for _, ev := range []string{edge.From, edge.To} {
				chunk, er := s.Chunk(ev)
				if er != nil || !s.W.Current(chunk) {
					return out, fmt.Errorf("stale_relation")
				}
				sc.Dependencies[chunk.Path] = chunk.Hash
			} // Type resolution can depend on any local package or build input.
			sc.Dependencies["scope:."] = scopeHash(".", files)
		}
		for _, scope := range c.Scopes {
			if scope != "." && (path.Clean(scope) != scope || strings.HasPrefix(scope, "../") || path.IsAbs(scope) || strings.Contains(scope, "\\")) {
				return out, fmt.Errorf("invalid_dependency_scope")
			}
			sc.Dependencies["scope:"+scope] = scopeHash(scope, files)
		}
		if !s.claimCurrent(sc) {
			return out, fmt.Errorf("stale_dependencies")
		}
		stored = append(stored, sc)
	}
	// Revalidate the complete source inventory for publication, including additions
	// and changes in negative-claim scopes; reading a result must never bless it.
	paths, e := s.W.Paths()
	if e != nil {
		return out, e
	}
	if len(paths) != len(files) {
		return out, fmt.Errorf("stale_result: run update")
	}
	for _, p := range paths {
		f, ok := files[p]
		b, _, er := s.W.Read(p)
		if !ok || f.Reason == "" && (er != nil || ID(string(b)) != f.Hash) || f.Reason != "" && er == nil {
			return out, fmt.Errorf("stale_result: run update")
		}
	}
	out.Data = map[string]any{"document_id": docID, "claims": len(stored), "dry_run": dry, "validation": "structural_only"}
	if dry {
		return out, nil
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM search WHERE id IN (SELECT id FROM claims WHERE document=?)", docID); e != nil {
		return out, e
	}
	if _, e = tx.Exec("DELETE FROM claims WHERE document=?", docID); e != nil {
		return out, e
	}
	if _, e = tx.Exec("INSERT OR REPLACE INTO documents VALUES (?,?)", docID, encode(r)); e != nil {
		return out, e
	}
	for _, c := range stored {
		if _, e = tx.Exec("INSERT INTO claims VALUES (?,?,?)", c.Claim.ID, docID, encode(c)); e != nil {
			return out, e
		}
		if _, e = tx.Exec("INSERT INTO search VALUES (?,?,?)", c.Claim.ID, "claim", tokenize(c.Claim.Text+" "+strings.Join(c.Claim.Conditions, " "))); e != nil {
			return out, e
		}
	}
	if e = tx.Commit(); e != nil {
		return out, e
	}
	if e = s.restoreDocuments(); e != nil {
		return out, fmt.Errorf("knowledge_committed_export_pending: %w", e)
	}
	return out, nil
}
func (s *Store) documents() (map[string]Result, error) {
	rows, e := s.Q.Query("SELECT id,payload FROM documents ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]Result{}
	for rows.Next() {
		var id, b string
		var r Result
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &r); e != nil {
			return nil, e
		}
		out[id] = r
	}
	return out, rows.Err()
}
func atomicFile(p string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(p), ".omp-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, p)
}
func (s *Store) restoreDocuments() error {
	docs, e := s.documents()
	if e != nil {
		return e
	}
	if len(docs) == 0 {
		return nil
	}
	dir := filepath.Join(s.W.Root, ".onboard", "knowledge")
	if e = os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	claims, e := s.allClaims()
	if e != nil {
		return e
	}
	for id, d := range docs {
		docClaims := []StoredClaim{}
		for _, c := range claims {
			if c.Document == id {
				docClaims = append(docClaims, c)
			}
		}
		b, _ := json.MarshalIndent(Portable{1, d, docClaims}, "", "  ")
		if e = atomicFile(filepath.Join(dir, id+".json"), b); e != nil {
			return e
		}
		var md strings.Builder
		fmt.Fprintf(&md, "# %s\n\nAgent-assessed; structurally validated. Read current evidence before reuse.\n\n", d.Title)
		for _, c := range claims {
			if c.Document != id {
				continue
			}
			state := "current at last update"
			if c.Stale {
				state = "stale"
			}
			fmt.Fprintf(&md, "- [%s; %s] %s\n  Conditions: %s\n  Assumptions: %s\n  Evidence: %s\n", state, c.Claim.Kind, c.Claim.Text, strings.Join(c.Claim.Conditions, "; "), strings.Join(c.Claim.Assumptions, "; "), strings.Join(c.Claim.Evidence, ", "))
		}
		for _, q := range d.Unresolved {
			fmt.Fprintf(&md, "\nUnresolved: %s\n", q)
		}
		if e = atomicFile(filepath.Join(dir, id+".md"), []byte(md.String())); e != nil {
			return e
		}
	}
	ids := []string{}
	for id := range docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var nav strings.Builder
	nav.WriteString("# Knowledge navigation\n\nAgent-assessed documents; reopen source before reuse.\n\n")
	for _, id := range ids {
		d := docs[id]
		current, stale := 0, 0
		for _, c := range claims {
			if c.Document == id {
				if c.Stale {
					stale++
				} else {
					current++
				}
			}
		}
		fmt.Fprintf(&nav, "- [%s](%s.md): %d current at last update, %d stale claims. Scope: %s\n", d.Title, id, current, stale, strings.Join(d.Reviewed, ", "))
	}
	if e = atomicFile(filepath.Join(dir, "index.md"), []byte(nav.String())); e != nil {
		return e
	}
	return nil
}

// Portable result files are revalidated against the rebuilt cache. A stale file
// is retained for inspection and never admitted to current retrieval.
type Portable struct {
	Schema int           `json:"schema_version"`
	Result Result        `json:"result"`
	Claims []StoredClaim `json:"claims"`
}

func (s *Store) ImportPortable() []string {
	warnings := []string{}
	unlock, err := s.Lock()
	if err != nil {
		return []string{err.Error()}
	}
	defer unlock()
	paths, _ := filepath.Glob(filepath.Join(s.W.Root, ".onboard", "knowledge", "*.json"))
	files, err := s.Files()
	if err != nil {
		return []string{"portable_store_unavailable"}
	}
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".json")
		var exists string
		if s.Q.QueryRow("SELECT id FROM documents WHERE id=?", id).Scan(&exists) == nil {
			continue
		}
		st, e := os.Lstat(p)
		if e != nil || !st.Mode().IsRegular() || st.Size() > 4<<20 {
			warnings = append(warnings, "portable_invalid:"+filepath.Base(p))
			continue
		}
		b, e := os.ReadFile(p)
		if e != nil {
			warnings = append(warnings, "portable_unreadable:"+filepath.Base(p))
			continue
		}
		var d Portable
		if json.Unmarshal(b, &d) != nil || d.Schema != 1 || d.Result.Assessment != "agent_assessed" {
			warnings = append(warnings, "portable_invalid:"+filepath.Base(p))
			continue
		}
		r := d.Result
		sort.Strings(r.Reviewed)
		if id != ID("document", r.Unit, r.Title, strings.Join(r.Reviewed, "\x00")) {
			warnings = append(warnings, "portable_identity_mismatch:"+filepath.Base(p))
			continue
		}
		expected := map[string]Claim{}
		for _, c := range r.Claims {
			c.ID = ID(id, c.ID)
			expected[c.ID] = c
		}
		valid := true
		for i := range d.Claims {
			c := &d.Claims[i]
			ex, ok := expected[c.Claim.ID]
			if !ok || encode(ex) != encode(c.Claim) || c.Document != id || len(c.Dependencies) == 0 {
				valid = false
				break
			}
			c.Stale = c.Configuration != s.W.ConfigHash || !dependenciesMatch(c.Dependencies, files)
			for _, ev := range c.Claim.Evidence {
				chunk, er := s.Chunk(ev)
				if er != nil {
					c.Stale = true
					continue
				}
				if c.Dependencies[chunk.Path] != chunk.Hash {
					valid = false
				}
			}
			for _, scope := range c.Claim.Scopes {
				if c.Dependencies["scope:"+scope] == "" {
					valid = false
				}
			}
			for _, rel := range c.Claim.Relations {
				var raw string
				if s.Q.QueryRow("SELECT payload FROM edges WHERE id=?", rel).Scan(&raw) != nil {
					c.Stale = true
				}
				if c.Dependencies["scope:."] == "" {
					valid = false
				}
			}
		}
		if !valid || len(d.Claims) != len(r.Claims) {
			warnings = append(warnings, "portable_provenance_invalid:"+filepath.Base(p))
			continue
		}
		tx, e := s.DB.Begin()
		if e != nil {
			warnings = append(warnings, "portable_store_unavailable")
			continue
		}
		_, e = tx.Exec("INSERT INTO documents VALUES (?,?)", id, encode(r))
		for _, c := range d.Claims {
			if e != nil {
				break
			}
			_, e = tx.Exec("INSERT INTO claims VALUES (?,?,?)", c.Claim.ID, id, encode(c))
			if e == nil && !c.Stale {
				_, e = tx.Exec("INSERT INTO search VALUES (?,?,?)", c.Claim.ID, "claim", tokenize(c.Claim.Text+" "+strings.Join(c.Claim.Conditions, " ")))
			}
		}
		if e != nil {
			tx.Rollback()
			warnings = append(warnings, "portable_store_write_failed")
			continue
		}
		if e = tx.Commit(); e != nil {
			warnings = append(warnings, "portable_store_commit_failed")
		}
	}
	if e := s.restoreDocuments(); e != nil {
		warnings = append(warnings, "portable_export_pending")
	}
	return warnings
}
