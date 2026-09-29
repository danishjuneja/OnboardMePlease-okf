package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Store struct {
	DB *sql.DB
	W  Workspace
	Q  interface {
		Query(string, ...any) (*sql.Rows, error)
		QueryRow(string, ...any) *sql.Row
	}
}

const ddl = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS stages (path TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chunks (id TEXT PRIMARY KEY, path TEXT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS chunk_path ON chunks(path);
CREATE INDEX IF NOT EXISTS chunk_symbol ON chunks(json_extract(payload,'$.symbol'));
CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(id UNINDEXED, kind UNINDEXED, text, tokenize='unicode61');
CREATE TABLE IF NOT EXISTS edges (id TEXT PRIMARY KEY, src TEXT NOT NULL, dst TEXT NOT NULL, kind TEXT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS edge_src ON edges(src);
CREATE INDEX IF NOT EXISTS edge_dst ON edges(dst);
CREATE TABLE IF NOT EXISTS units (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS documents (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS claims (id TEXT PRIMARY KEY, document TEXT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS claim_document ON claims(document);
`

func Open(w Workspace, create bool) (*Store, error) {
	for _, rel := range []string{".onboard", ".onboard/cache", ".onboard/knowledge", ".onboard/cache/index.sqlite"} {
		if _, e := w.safe(rel); e != nil && !os.IsNotExist(e) {
			return nil, fmt.Errorf("unsafe_store: %w", e)
		}
	}
	p := filepath.Join(w.Root, ".onboard", "cache", "index.sqlite")
	if !create {
		if _, e := os.Stat(p); e != nil {
			return nil, fmt.Errorf("unavailable: run omp update: %w", e)
		}
	} else {
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return nil, e
		}
		ignore := filepath.Join(w.Root, ".onboard", ".gitignore")
		if _, e := os.Stat(ignore); os.IsNotExist(e) {
			if e = os.WriteFile(ignore, []byte("cache/\n"), 0644); e != nil {
				return nil, e
			}
		}
		cfg := filepath.Join(w.Root, ".onboard", "config.toml")
		if _, e := os.Stat(cfg); os.IsNotExist(e) {
			if e = os.WriteFile(cfg, []byte("# Additional repository-relative exclusions\nexclude = []\n"), 0644); e != nil {
				return nil, e
			}
		}
	}
	db, e := sql.Open("sqlite", filepath.ToSlash(p))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, W: w, Q: db}
	if _, e = db.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;"); e != nil {
		db.Close()
		return nil, e
	}
	if create {
		_, e = db.Exec(ddl)
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Meta(k string) string {
	var v string
	s.Q.QueryRow("SELECT value FROM meta WHERE key=?", k).Scan(&v)
	return v
}
func (s *Store) Chunk(id string) (Chunk, error) {
	var c Chunk
	var b string
	e := s.Q.QueryRow("SELECT payload FROM chunks WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &c)
	}
	return c, e
}
func (s *Store) Files() (map[string]File, error) {
	r, e := s.Q.Query("SELECT payload FROM files ORDER BY path")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	m := map[string]File{}
	for r.Next() {
		var b string
		var f File
		if e = r.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &f); e != nil {
			return nil, e
		}
		// Inventory and graph traversal need ranges/IDs, not a second in-memory
		// copy of all source. The chunks table owns retrievable source text.
		for i := range f.Chunks {
			f.Chunks[i].Text = ""
		}
		m[f.Path] = f
	}
	return m, r.Err()
}
func (s *Store) Envelope(state string, data any) Envelope {
	return Envelope{1, s.Meta("generation"), state, []string{}, []string{}, false, data}
}
func (s *Store) Lock() (func(), error) {
	p := filepath.Join(s.W.Root, ".onboard", "cache", "writer.lock")
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, fmt.Errorf("writer_locked: another writer or interrupted process; inspect cache/writer.lock before removing it")
	}
	fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(p) }, nil
}
