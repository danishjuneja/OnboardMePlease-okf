package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/repository"
)

const ExtractorVersion = "text-go-ast-v1"

type Chunk struct {
	ID          string `json:"evidence_id"`
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Language    string `json:"language"`
	SourceKind  string `json:"source_kind"`
	Content     string `json:"snippet"`
	ContentHash string `json:"content_hash"`
}

type Symbol struct {
	ID            string `json:"symbol_id"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Language      string `json:"language"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}

type Relation struct {
	ID         string `json:"relation_id"`
	FromID     string `json:"from_id"`
	ToID       string `json:"to_id"`
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
}

type Capability struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	Text      string `json:"text"`
	Syntax    string `json:"syntax"`
	Semantic  string `json:"semantic"`
	Framework string `json:"framework"`
	Reason    string `json:"reason"`
}

type GoCall struct {
	FromID     string `json:"from_id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Line       int    `json:"line"`
	PackageKey string `json:"package_key"`
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func classify(path string) (language, sourceKind string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		language = "go"
	case ".ts", ".tsx":
		language = "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		language = "javascript"
	case ".py":
		language = "python"
	case ".java":
		language = "java"
	case ".sql":
		language = "sql"
	case ".yaml", ".yml":
		language = "yaml"
	case ".json":
		language = "json"
	case ".md", ".mdx":
		language = "markdown"
	default:
		language = "text"
	}
	sourceKind = "code"
	name := strings.ToLower(path)
	switch {
	case strings.HasSuffix(name, "_test.go"), strings.Contains(name, ".test."), strings.HasPrefix(name, "test/"):
		sourceKind = "test"
	case language == "markdown":
		sourceKind = "documentation"
	case language == "yaml", language == "json":
		sourceKind = "configuration"
	case language == "sql":
		sourceKind = "configuration"
	}
	return
}

func chunks(snapshotID, path string, data []byte) ([]Chunk, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("not UTF-8 text")
	}
	language, kind := classify(path)
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	for _, line := range lines {
		if len(line) > 16384 {
			return nil, errors.New("line exceeds indexing limit")
		}
	}
	var result []Chunk
	for start := 0; start < len(lines); {
		end, size := start, 0
		for end < len(lines) && end-start < 80 && (size+len(lines[end]) <= 8192 || end == start) {
			size += len(lines[end]) + 1
			end++
		}
		content := strings.Join(lines[start:end], "\n")
		contentHash := sha256.Sum256([]byte(content))
		result = append(result, Chunk{ID: stableID(snapshotID, path, strconv.Itoa(start+1), strconv.Itoa(end), ExtractorVersion),
			Path: path, StartLine: start + 1, EndLine: end, Language: language, SourceKind: kind,
			Content: content, ContentHash: hex.EncodeToString(contentHash[:])})
		start = end
	}
	return result, nil
}

func extractGo(snapshotID, path string, data []byte) ([]Symbol, []GoCall, string) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.AllErrors)
	if err != nil {
		return nil, nil, "go_parse_failed"
	}
	packageKey := filepath.ToSlash(filepath.Dir(path)) + "/" + file.Name.Name
	var symbols []Symbol
	var calls []GoCall
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			name := node.Name.Name
			kind := "function"
			if node.Recv != nil {
				kind = "method"
			}
			start, end := fset.Position(node.Pos()).Line, fset.Position(node.End()).Line
			id := stableID(snapshotID, path, name, strconv.Itoa(start), ExtractorVersion)
			symbols = append(symbols, Symbol{ID: id, Path: path, Name: name, QualifiedName: packageKey + "." + name,
				Kind: kind, Language: "go", StartLine: start, EndLine: end})
			if node.Body != nil && node.Recv == nil {
				ast.Inspect(node.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					identifier, ok := call.Fun.(*ast.Ident)
					if ok {
						calls = append(calls, GoCall{FromID: id, Name: identifier.Name, Path: path,
							Line: fset.Position(call.Pos()).Line, PackageKey: packageKey})
					}
					return true
				})
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				decl, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				start, end := fset.Position(decl.Pos()).Line, fset.Position(decl.End()).Line
				name := decl.Name.Name
				symbols = append(symbols, Symbol{ID: stableID(snapshotID, path, name, strconv.Itoa(start), ExtractorVersion),
					Path: path, Name: name, QualifiedName: packageKey + "." + name, Kind: "type", Language: "go",
					StartLine: start, EndLine: end})
			}
		}
	}
	return symbols, calls, ""
}

func recordIndexFailure(ctx context.Context, pool *pgxpool.Pool, snapshotID, path, reason string) error {
	language, _ := classify(path)
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(context.Background())
	if _, err = transaction.Exec(ctx, `DELETE FROM relations WHERE snapshot_id=$1 AND
		(path=$2 OR from_id IN (SELECT id FROM symbols WHERE snapshot_id=$1 AND path=$2)
		OR to_id IN (SELECT id FROM symbols WHERE snapshot_id=$1 AND path=$2))`, snapshotID, path); err != nil {
		return err
	}
	if _, err = transaction.Exec(ctx, `DELETE FROM symbols WHERE snapshot_id=$1 AND path=$2`, snapshotID, path); err != nil {
		return err
	}
	if _, err = transaction.Exec(ctx, `DELETE FROM evidence_chunks WHERE snapshot_id=$1 AND path=$2`, snapshotID, path); err != nil {
		return err
	}
	if _, err = transaction.Exec(ctx, `UPDATE artifacts SET status='failed',reason_codes=ARRAY[$3] WHERE snapshot_id=$1 AND path=$2`, snapshotID, path, reason); err != nil {
		return err
	}
	if _, err = transaction.Exec(ctx, `INSERT INTO artifact_capabilities
		(snapshot_id,path,language,text_status,syntax_status,semantic_status,framework_status,reason,extractor_version)
		VALUES ($1,$2,$3,'failed','unavailable','unavailable','unavailable',$4,$5)
		ON CONFLICT (snapshot_id,path) DO UPDATE SET text_status='failed',syntax_status='unavailable',
		semantic_status='unavailable',framework_status='unavailable',reason=excluded.reason,
		extractor_version=excluded.extractor_version`, snapshotID, path, language, reason, ExtractorVersion); err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

// Build indexes each approved file independently. A parser failure affects only that artifact.
// Text chunks remain available even when syntax extraction fails.
func Build(ctx context.Context, pool *pgxpool.Pool, dataDir, repositoryID, snapshotID string) error {
	var ready bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM snapshots s JOIN repositories r ON r.id=s.repository_id
		WHERE s.id=$1 AND s.repository_id=$2 AND s.state='ready' AND r.source_kind='github')`, snapshotID, repositoryID).Scan(&ready); err != nil || !ready {
		return errors.New("ready snapshot unavailable")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(dataDir, "snapshots", snapshotID, "manifest.json"))
	if err != nil {
		return errors.New("snapshot manifest unavailable")
	}
	var manifest repository.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.SnapshotID != snapshotID || manifest.RepositoryID != repositoryID {
		return errors.New("snapshot manifest mismatch")
	}
	var allSymbols []Symbol
	var allCalls []GoCall
	for _, artifact := range manifest.Artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if artifact.Status != "pending" {
			language, _ := classify(artifact.Path)
			_, err := pool.Exec(ctx, `INSERT INTO artifact_capabilities
				(snapshot_id,path,language,text_status,syntax_status,semantic_status,framework_status,reason,extractor_version)
				VALUES ($1,$2,$3,'unavailable','unavailable','unavailable','unavailable',$4,$5)
				ON CONFLICT (snapshot_id,path) DO NOTHING`, snapshotID, artifact.Path, language, strings.Join(artifact.ReasonCodes, ","), ExtractorVersion)
			if err != nil {
				return err
			}
			continue
		}
		filePath := filepath.Join(dataDir, "snapshots", snapshotID, "files", filepath.FromSlash(artifact.Path))
		fileRoot := filepath.Join(dataDir, "snapshots", snapshotID, "files")
		rel, relErr := filepath.Rel(fileRoot, filePath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return errors.New("snapshot path escapes private storage")
		}
		data, err := os.ReadFile(filePath)
		if err == nil {
			_, err = privacy.Clear(artifact.Path, data, nil)
		}
		if err == nil {
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != artifact.ContentHash {
				err = errors.New("captured file hash mismatch")
			}
		}
		if err != nil {
			if dbErr := recordIndexFailure(ctx, pool, snapshotID, artifact.Path, "index_input_unavailable"); dbErr != nil {
				return dbErr
			}
			continue
		}
		pieces, err := chunks(snapshotID, artifact.Path, data)
		if err != nil {
			if dbErr := recordIndexFailure(ctx, pool, snapshotID, artifact.Path, "text_index_failed"); dbErr != nil {
				return dbErr
			}
			continue
		}
		language, _ := classify(artifact.Path)
		capability := Capability{Path: artifact.Path, Language: language, Text: "available", Syntax: "unavailable", Semantic: "unavailable", Framework: "unavailable"}
		var symbols []Symbol
		var calls []GoCall
		if language == "go" {
			var reason string
			symbols, calls, reason = parseGoIsolated(ctx, snapshotID, artifact.Path, data)
			if reason == "" {
				capability.Syntax = "available"
			} else {
				capability.Syntax = "failed"
				capability.Reason = reason
			}
		}
		transaction, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		for _, piece := range pieces {
			_, err = transaction.Exec(ctx, `INSERT INTO evidence_chunks
				(id,snapshot_id,path,start_line,end_line,language,source_kind,content,content_hash,extractor_version)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				ON CONFLICT (snapshot_id,path,start_line,end_line) DO UPDATE SET
				language=excluded.language,source_kind=excluded.source_kind,content=excluded.content,
				content_hash=excluded.content_hash,extractor_version=excluded.extractor_version`,
				piece.ID, snapshotID, piece.Path, piece.StartLine, piece.EndLine, piece.Language, piece.SourceKind, piece.Content, piece.ContentHash, ExtractorVersion)
			if err != nil {
				break
			}
		}
		if err == nil {
			_, err = transaction.Exec(ctx, `INSERT INTO artifact_capabilities
			(snapshot_id,path,language,text_status,syntax_status,semantic_status,framework_status,reason,extractor_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (snapshot_id,path) DO UPDATE SET
			language=excluded.language,text_status=excluded.text_status,syntax_status=excluded.syntax_status,
			semantic_status=CASE WHEN artifact_capabilities.semantic_status='available' THEN 'available' ELSE excluded.semantic_status END,
			framework_status=excluded.framework_status,
			reason=excluded.reason,extractor_version=excluded.extractor_version`,
				snapshotID, capability.Path, capability.Language, capability.Text, capability.Syntax, capability.Semantic, capability.Framework, capability.Reason, ExtractorVersion)
		}
		if err == nil {
			_, err = transaction.Exec(ctx, `UPDATE artifacts SET status='analyzed',reason_codes='{}' WHERE snapshot_id=$1 AND path=$2`, snapshotID, artifact.Path)
		}
		if err != nil {
			_ = transaction.Rollback(ctx)
			return err
		}
		if err := transaction.Commit(ctx); err != nil {
			return err
		}
		allSymbols = append(allSymbols, symbols...)
		allCalls = append(allCalls, calls...)
	}
	return publishGraph(ctx, pool, snapshotID, allSymbols, allCalls)
}

func resolveCalls(snapshotID string, symbols []Symbol, calls []GoCall) []Relation {
	definitions := make(map[string][]Symbol)
	for _, symbol := range symbols {
		if symbol.Kind == "function" {
			definitions[symbol.QualifiedName] = append(definitions[symbol.QualifiedName], symbol)
		}
	}
	var relations []Relation
	for _, call := range calls {
		targets := definitions[call.PackageKey+"."+call.Name]
		if len(targets) != 1 {
			continue
		}
		toID := targets[0].ID
		relations = append(relations, Relation{ID: stableID(snapshotID, call.FromID, toID, call.Path, strconv.Itoa(call.Line), "calls"),
			FromID: call.FromID, ToID: toID, Kind: "calls", Resolution: "resolved", Path: call.Path, Line: call.Line})
	}
	return relations
}

func publishGraph(ctx context.Context, pool *pgxpool.Pool, snapshotID string, symbols []Symbol, calls []GoCall) error {
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(context.Background())
	if _, err = transaction.Exec(ctx, `DELETE FROM relations WHERE snapshot_id=$1 AND extractor_version=$2`, snapshotID, ExtractorVersion); err != nil {
		return err
	}
	if _, err = transaction.Exec(ctx, `DELETE FROM symbols WHERE snapshot_id=$1 AND extractor_version=$2`, snapshotID, ExtractorVersion); err != nil {
		return err
	}
	for _, symbol := range symbols {
		_, err = transaction.Exec(ctx, `INSERT INTO symbols
			(id,snapshot_id,path,name,qualified_name,kind,language,start_line,end_line,extractor_version)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (id) DO NOTHING`, symbol.ID, snapshotID,
			symbol.Path, symbol.Name, symbol.QualifiedName, symbol.Kind, symbol.Language, symbol.StartLine, symbol.EndLine, ExtractorVersion)
		if err != nil {
			return err
		}
	}
	for _, relation := range resolveCalls(snapshotID, symbols, calls) {
		_, err = transaction.Exec(ctx, `INSERT INTO relations
			(id,snapshot_id,from_id,to_id,kind,resolution,path,line,extractor_version)
			VALUES ($1,$2,$3,$4,'calls','resolved',$5,$6,$7) ON CONFLICT (id) DO NOTHING`,
			relation.ID, snapshotID, relation.FromID, relation.ToID, relation.Path, relation.Line, ExtractorVersion)
		if err != nil {
			return err
		}
	}
	return transaction.Commit(ctx)
}
