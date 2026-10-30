package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Audit also detects new sources that can introduce callers or change a negative
// finding without touching an existing citation. This intentionally favors
// correctness over metadata-only latency in the initial CLI.
func (s *Store) Audit() (bool, error) {
	if s.sessionActive {
		return s.sessionGeneration != "" && s.Meta("generation") == s.sessionGeneration && s.Meta("config") == s.W.ConfigHash, nil
	}
	if s.Fast {
		return s.auditMetadata()
	}
	files, e := s.Files()
	if e != nil {
		return false, e
	}
	paths, e := s.W.Paths()
	if e != nil {
		return false, e
	}
	if len(paths) != len(files) || s.W.ConfigHash != s.Meta("config") {
		return false, nil
	}
	for _, p := range paths {
		f, ok := files[p]
		if !ok {
			return false, nil
		}
		b, st, err := s.W.Read(p)
		if f.Reason != "" {
			if err == nil {
				return false, nil
			}
			continue
		}
		if err != nil || ID(string(b)) != f.Hash || uint32(st.Mode()) != f.Mode {
			return false, nil
		}
	}
	return true, nil
}

// BeginSession verifies once; subsequent reads use the captured generation.
// Without a change journal it cannot guarantee that the worktree stays current.
func (s *Store) BeginSession() error {
	s.sessionActive = true
	return s.RefreshSession()
}

func (s *Store) InvalidateSession() { s.sessionGeneration = "" }

func (s *Store) RefreshSession() error {
	s.sessionActive = false
	s.sessionGeneration = ""
	fresh, err := s.Audit()
	s.sessionActive = true
	if err != nil {
		return err
	}
	if !fresh {
		return fmt.Errorf("stale_session: run omp update")
	}
	s.sessionGeneration = s.Meta("generation")
	if s.sessionGeneration == "" {
		return fmt.Errorf("unavailable: run omp update")
	}
	return nil
}

func (s *Store) SessionResult(r Envelope) Envelope {
	if !s.sessionActive {
		return r
	}
	r.Verification = "session_snapshot"
	r.Warnings = append(r.Warnings, "The inventory was checked at session refresh, not for this request; changes may be missed. Refresh before relying on repository-wide results.")
	r.Reasons = append(r.Reasons, "session_freshness_not_revalidated")
	if r.State == "matched" || r.State == "no_match" || r.State == "current" {
		r.State = "partial"
	}
	return r
}

// Metadata checks are explicitly weaker than content hashes. Return sources
// and claim dependencies still undergo their normal content checks.
func (s *Store) auditMetadata() (bool, error) {
	if s.W.ConfigHash != s.Meta("config") {
		return false, nil
	}
	type metadata struct {
		size, mtime int64
		mode        uint32
		reason      string
	}
	files := map[string]metadata{}
	rows, err := s.Q.Query("SELECT path, coalesce(json_extract(payload,'$.size'),0), coalesce(json_extract(payload,'$.mtime'),0), coalesce(json_extract(payload,'$.mode'),0), coalesce(json_extract(payload,'$.excluded_reason'),'') FROM files")
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var p string
		var m metadata
		if err = rows.Scan(&p, &m.size, &m.mtime, &m.mode, &m.reason); err != nil {
			break
		}
		files[p] = m
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, err
	}
	paths, err := git(s.W.Root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	matched := 0
	for _, p := range strings.Split(paths, "\x00") {
		if p == "" || seen[p] || s.W.excluded(p) {
			continue
		}
		seen[p] = true
		m, ok := files[p]
		if !ok {
			if _, err := os.Lstat(filepath.Join(s.W.Root, filepath.FromSlash(p))); os.IsNotExist(err) {
				continue
			}
			return false, nil
		}
		matched++
		if m.reason != "" {
			if _, _, err := s.W.Read(p); err == nil || os.IsNotExist(err) {
				return false, nil
			}
			continue
		}
		_, st, err := s.W.safeInfo(p)
		if err != nil {
			return false, nil
		}
		if !st.Mode().IsRegular() || st.Size() != m.size || st.ModTime().UnixNano() != m.mtime || uint32(st.Mode()) != m.mode {
			return false, nil
		}
	}
	return matched == len(files), nil
}
func sourceReason(e error) string {
	if os.IsNotExist(e) {
		return "missing"
	}
	if os.IsPermission(e) {
		return "permission_denied"
	}
	switch e.Error() {
	case "symlink", "excluded", "unsafe_path", "not_regular_or_over_2MiB", "binary", "secret_pattern", "oversized_line":
		return e.Error()
	}
	return "read_failed"
}
