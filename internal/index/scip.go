package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	scip "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"

	"onboardmeplease/internal/privacy"
)

const SCIPExtractorVersion = "scip-v1"

type SCIPResult struct {
	ImportedDocuments int `json:"imported_documents"`
	SkippedDocuments  int `json:"skipped_documents"`
	Symbols           int `json:"symbols"`
	Relations         int `json:"relations"`
}

type scipDocument struct {
	path        string
	language    string
	occurrences []*scip.Occurrence
	definitions []Symbol
}

func scipLineRange(occurrence *scip.Occurrence) (int, int, bool) {
	if typed := occurrence.GetSingleLineRange(); typed != nil {
		line := int(typed.GetLine()) + 1
		return line, line, line > 0 && typed.GetEndCharacter() >= typed.GetStartCharacter()
	}
	if typed := occurrence.GetMultiLineRange(); typed != nil {
		start, end := int(typed.GetStartLine())+1, int(typed.GetEndLine())+1
		if typed.GetEndCharacter() == 0 && end > start {
			end--
		}
		return start, end, start > 0 && end >= start
	}
	rangeValues := occurrence.GetRange()
	if len(rangeValues) == 3 {
		line := int(rangeValues[0]) + 1
		return line, line, line > 0 && rangeValues[2] >= rangeValues[1]
	}
	if len(rangeValues) == 4 {
		start, end := int(rangeValues[0])+1, int(rangeValues[2])+1
		if rangeValues[3] == 0 && end > start {
			end--
		}
		return start, end, start > 0 && end >= start
	}
	return 0, 0, false
}

func scipDefinitionRange(occurrence *scip.Occurrence) (int, int, bool) {
	if typed := occurrence.GetSingleLineEnclosingRange(); typed != nil {
		line := int(typed.GetLine()) + 1
		return line, line, line > 0
	}
	if typed := occurrence.GetMultiLineEnclosingRange(); typed != nil {
		start, end := int(typed.GetStartLine())+1, int(typed.GetEndLine())+1
		if typed.GetEndCharacter() == 0 && end > start {
			end--
		}
		return start, end, start > 0 && end >= start
	}
	values := occurrence.GetEnclosingRange()
	if len(values) == 3 {
		line := int(values[0]) + 1
		return line, line, line > 0
	}
	if len(values) == 4 {
		start, end := int(values[0])+1, int(values[2])+1
		if values[3] == 0 && end > start {
			end--
		}
		return start, end, start > 0 && end >= start
	}
	return scipLineRange(occurrence)
}

func scipKey(path, symbol string) string {
	if strings.HasPrefix(symbol, "local ") {
		return path + "\x00" + symbol
	}
	return symbol
}

func scipName(symbol string, information *scip.SymbolInformation) string {
	if information != nil {
		if display := strings.TrimSpace(information.GetDisplayName()); display != "" && len(display) <= 256 {
			return display
		}
	}
	if len(symbol) > 256 {
		return symbol[len(symbol)-256:]
	}
	return symbol
}

func scipKind(information *scip.SymbolInformation) string {
	if information == nil {
		return "other"
	}
	value := strings.ToLower(information.GetKind().String())
	switch {
	case strings.Contains(value, "method"):
		return "method"
	case strings.Contains(value, "function"):
		return "function"
	case strings.Contains(value, "class"):
		return "class"
	case strings.Contains(value, "type"), strings.Contains(value, "struct"), strings.Contains(value, "interface"):
		return "type"
	case strings.Contains(value, "variable"), strings.Contains(value, "field"):
		return "variable"
	default:
		return "other"
	}
}

func validSCIPPath(path string) bool {
	if path == "" || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") || filepath.IsAbs(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validLanguage(value string) bool {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '+' || char == '-' || char == '.' || char == '#' {
			continue
		}
		return false
	}
	return value != ""
}

// ImportSCIP accepts only documents whose supplied text exactly matches the captured
// snapshot. This avoids attaching a stale external semantic index to current source.
func ImportSCIP(ctx context.Context, pool *pgxpool.Pool, dataDir, repositoryID, snapshotID string, raw []byte) (SCIPResult, error) {
	var result SCIPResult
	if len(raw) == 0 || len(raw) > 16<<20 {
		return result, errors.New("SCIP index size must be 1-16 MiB")
	}
	var ready bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM snapshots s JOIN repositories r ON r.id=s.repository_id
		WHERE s.id=$1 AND s.repository_id=$2 AND s.state='ready' AND r.source_kind='github')`, snapshotID, repositoryID).Scan(&ready)
	if err != nil || !ready {
		return result, errors.New("ready GitHub snapshot unavailable")
	}
	var source scip.Index
	if err := proto.Unmarshal(raw, &source); err != nil {
		return result, errors.New("invalid SCIP protobuf")
	}
	if len(source.GetDocuments()) > 10000 {
		return result, errors.New("SCIP index has too many documents")
	}
	var documents []scipDocument
	definitionByKey := make(map[string][]Symbol)
	seenPaths := make(map[string]bool)
	for _, document := range source.GetDocuments() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := document.GetRelativePath()
		if !validSCIPPath(path) || seenPaths[path] || len(document.GetOccurrences()) > 100000 {
			result.SkippedDocuments++
			continue
		}
		seenPaths[path] = true
		var artifactHash string
		err := pool.QueryRow(ctx, `SELECT content_hash FROM artifacts WHERE snapshot_id=$1 AND path=$2 AND status='analyzed'`, snapshotID, path).Scan(&artifactHash)
		if err != nil {
			result.SkippedDocuments++
			continue
		}
		filePath := filepath.Join(dataDir, "snapshots", snapshotID, "files", filepath.FromSlash(path))
		content, err := os.ReadFile(filePath)
		if err != nil || !bytes.Equal(content, []byte(document.GetText())) {
			result.SkippedDocuments++
			continue
		}
		hash := sha256.Sum256(content)
		if hex.EncodeToString(hash[:]) != artifactHash {
			result.SkippedDocuments++
			continue
		}
		if _, err := privacy.Clear(path, content, nil); err != nil {
			result.SkippedDocuments++
			continue
		}
		contentLines := bytes.Count(content, []byte{'\n'}) + 1
		if len(content) > 0 && content[len(content)-1] == '\n' {
			contentLines--
		}
		if contentLines < 1 {
			contentLines = 1
		}
		language := document.GetLanguage()
		if language == "" || len(language) > 64 || !validLanguage(language) {
			language, _ = classify(path)
		}
		information := make(map[string]*scip.SymbolInformation)
		for _, info := range document.GetSymbols() {
			information[info.GetSymbol()] = info
		}
		accepted := scipDocument{path: path, language: language, occurrences: document.GetOccurrences()}
		for _, occurrence := range document.GetOccurrences() {
			if occurrence.GetSymbol() == "" || len(occurrence.GetSymbol()) > 2000 || occurrence.GetSymbolRoles()&int32(scip.SymbolRole_Definition) == 0 {
				continue
			}
			start, end, ok := scipDefinitionRange(occurrence)
			if !ok || end > contentLines {
				continue
			}
			key := scipKey(path, occurrence.GetSymbol())
			if _, err := privacy.Clear("symbol", []byte(key), nil); err != nil {
				continue
			}
			name := scipName(occurrence.GetSymbol(), information[occurrence.GetSymbol()])
			if _, err := privacy.Clear("symbol-name", []byte(name), nil); err != nil {
				continue
			}
			symbol := Symbol{ID: stableID(snapshotID, path, key, strconv.Itoa(start), SCIPExtractorVersion), Path: path,
				Name: name, QualifiedName: occurrence.GetSymbol(), Kind: scipKind(information[occurrence.GetSymbol()]),
				Language: language, StartLine: start, EndLine: end}
			accepted.definitions = append(accepted.definitions, symbol)
			definitionByKey[key] = append(definitionByKey[key], symbol)
		}
		documents = append(documents, accepted)
		result.ImportedDocuments++
	}
	if result.ImportedDocuments == 0 {
		return result, errors.New("no SCIP documents matched the captured snapshot")
	}
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Exec(ctx, `DELETE FROM relations WHERE snapshot_id=$1 AND extractor_version=$2`, snapshotID, SCIPExtractorVersion); err != nil {
		return result, err
	}
	if _, err := transaction.Exec(ctx, `DELETE FROM symbols WHERE snapshot_id=$1 AND extractor_version=$2`, snapshotID, SCIPExtractorVersion); err != nil {
		return result, err
	}
	for _, document := range documents {
		for _, symbol := range document.definitions {
			command, err := transaction.Exec(ctx, `INSERT INTO symbols
				(id,snapshot_id,path,name,qualified_name,kind,language,start_line,end_line,extractor_version,external_key)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`,
				symbol.ID, snapshotID, symbol.Path, symbol.Name, symbol.QualifiedName, symbol.Kind, symbol.Language,
				symbol.StartLine, symbol.EndLine, SCIPExtractorVersion, symbol.QualifiedName)
			if err != nil {
				return result, err
			}
			result.Symbols += int(command.RowsAffected())
		}
		if _, err := transaction.Exec(ctx, `UPDATE artifact_capabilities SET semantic_status='available'
			WHERE snapshot_id=$1 AND path=$2`, snapshotID, document.path); err != nil {
			return result, err
		}
	}
	for _, document := range documents {
		for _, occurrence := range document.occurrences {
			if occurrence.GetSymbol() == "" || occurrence.GetSymbolRoles()&int32(scip.SymbolRole_Definition) != 0 {
				continue
			}
			line, _, ok := scipLineRange(occurrence)
			if !ok {
				continue
			}
			targets := definitionByKey[scipKey(document.path, occurrence.GetSymbol())]
			if len(targets) != 1 {
				continue
			}
			var enclosing *Symbol
			for i := range document.definitions {
				candidate := &document.definitions[i]
				if candidate.StartLine <= line && candidate.EndLine >= line && (enclosing == nil || candidate.EndLine-candidate.StartLine < enclosing.EndLine-enclosing.StartLine) {
					enclosing = candidate
				}
			}
			if enclosing == nil || enclosing.ID == targets[0].ID {
				continue
			}
			id := stableID(snapshotID, enclosing.ID, targets[0].ID, document.path, strconv.Itoa(line), "scip-reference")
			command, err := transaction.Exec(ctx, `INSERT INTO relations
				(id,snapshot_id,from_id,to_id,kind,resolution,path,line,extractor_version)
				VALUES ($1,$2,$3,$4,'references','resolved',$5,$6,$7) ON CONFLICT (id) DO NOTHING`,
				id, snapshotID, enclosing.ID, targets[0].ID, document.path, line, SCIPExtractorVersion)
			if err != nil {
				return result, err
			}
			result.Relations += int(command.RowsAffected())
		}
	}
	return result, transaction.Commit(ctx)
}
