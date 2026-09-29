package engine

import "os"

// Audit also detects new sources that can introduce callers or change a negative
// finding without touching an existing citation. This intentionally favors
// correctness over metadata-only latency in the initial CLI.
func (s *Store) Audit() (bool, error) {
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
