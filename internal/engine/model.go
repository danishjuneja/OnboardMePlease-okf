package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const Version = "omp-v1.0.0"

type Chunk struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Hash      string `json:"file_hash"`
	Start     int    `json:"start_line"`
	End       int    `json:"end_line"`
	Kind      string `json:"source_kind"`
	Symbol    string `json:"symbol,omitempty"`
	SymbolID  string `json:"symbol_id,omitempty"`
	Signature string `json:"signature,omitempty"`
	Text      string `json:"text"`
	Generated bool   `json:"generated"`
}
type Edge struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	To         string `json:"to,omitempty"`
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Target     string `json:"target"`
}
type File struct {
	Path      string  `json:"path"`
	Hash      string  `json:"hash"`
	Size      int64   `json:"size"`
	Mtime     int64   `json:"mtime"`
	Mode      uint32  `json:"mode"`
	Reason    string  `json:"excluded_reason,omitempty"`
	Chunks    []Chunk `json:"chunks,omitempty"`
	Syntax    string  `json:"syntax"`
	Extractor string  `json:"extractor"`
}
type Unit struct {
	ID          string   `json:"id"`
	Fingerprint string   `json:"fingerprint"`
	Paths       []string `json:"paths"`
	Evidence    []string `json:"evidence_ids"`
	Reason      string   `json:"reason"`
	Prior       []string `json:"prior_document_ids,omitempty"`
}
type Claim struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Kind        string   `json:"kind"`
	Evidence    []string `json:"evidence_ids"`
	Relations   []string `json:"relation_ids"`
	Conditions  []string `json:"conditions"`
	Assumptions []string `json:"assumptions"`
	Scopes      []string `json:"dependency_scopes"`
	Conflicts   []string `json:"conflicts_with"`
}
type Result struct {
	Schema      int               `json:"schema_version"`
	Generation  string            `json:"generation"`
	Unit        string            `json:"unit_id"`
	Fingerprint string            `json:"fingerprint"`
	Title       string            `json:"title"`
	Reviewed    []string          `json:"reviewed_paths"`
	Assessment  string            `json:"assessment"`
	Claims      []Claim           `json:"claims"`
	Unresolved  []string          `json:"unresolved"`
	Facets      map[string]string `json:"facets"`
}
type StoredClaim struct {
	Claim         Claim             `json:"claim"`
	Document      string            `json:"document"`
	Dependencies  map[string]string `json:"dependencies"`
	Stale         bool              `json:"stale"`
	Configuration string            `json:"configuration"`
}
type Envelope struct {
	Schema     int      `json:"schema_version"`
	Generation string   `json:"generation"`
	State      string   `json:"state"`
	Reasons    []string `json:"reason_codes"`
	Warnings   []string `json:"warnings"`
	Truncated  bool     `json:"truncated"`
	Data       any      `json:"data"`
}

func ID(parts ...string) string {
	s := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(s[:])
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
