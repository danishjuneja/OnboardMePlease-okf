package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"onboardmeplease/internal/graph"
	"onboardmeplease/internal/index"
	"onboardmeplease/internal/jobs"
	"onboardmeplease/internal/knowledge"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
)

func (server *Server) queueEmbeddings(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Provider string `json:"provider"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_input", "Invalid embedding request")
		return
	}
	adapter, err := providers.ConfiguredEmbedding(server.Config, input.Provider)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "provider_unavailable", err.Error())
		return
	}
	var mode string
	err = server.DB.QueryRow(r.Context(), `SELECT privacy_mode FROM repositories WHERE id=$1`, r.PathValue("repositoryId")).Scan(&mode)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "repository_not_found", "Repository not found")
		return
	}
	if err := privacy.Authorize(privacy.ModelRequest{Mode: server.Config.ModelMode, ProviderKind: adapter.Kind, ProviderURL: adapter.URL,
		RepoOptedIn: mode == "cloud_opt_in", ScanSucceeded: true, Sanitized: true}); err != nil {
		writeProblem(w, http.StatusForbidden, "embedding_denied", "Embedding provider denied by repository policy")
		return
	}
	inserted, err := server.Jobs.Insert(r.Context(), jobs.EmbedArgs{RepositoryID: r.PathValue("repositoryId"),
		SnapshotID: r.PathValue("snapshotId"), Provider: input.Provider}, nil)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "embedding_queue_failed", "Cannot queue embedding job")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"repository_id": r.PathValue("repositoryId"), "snapshot_id": r.PathValue("snapshotId"),
		"job_id": strconv.FormatInt(inserted.Job.ID, 10), "state": "queued"})
}

func (server *Server) importSCIP(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	if r.Header.Get("Content-Type") != "application/octet-stream" {
		writeProblem(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Upload a .scip protobuf as application/octet-stream")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_scip", "SCIP index exceeds the 16 MiB limit")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := index.ImportSCIP(ctx, server.DB, server.Config.DataDir, r.PathValue("repositoryId"), r.PathValue("snapshotId"), raw)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "invalid_scip", err.Error())
		return
	}
	// A semantic import changes the source graph used by the overview. Keep
	// generation idempotent; an unavailable overview does not discard the import.
	_, _ = knowledge.Build(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId"))
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) listRepositories(w http.ResponseWriter, r *http.Request) {
	rows, err := server.DB.Query(r.Context(), `SELECT repo.id,repo.source_locator,s.id,s.state,s.commit_oid,s.created_at
		FROM repositories repo JOIN LATERAL (
			SELECT id,state,commit_oid,created_at FROM snapshots WHERE repository_id=repo.id
			ORDER BY created_at DESC LIMIT 1
		) s ON true WHERE repo.source_kind='github' ORDER BY s.created_at DESC LIMIT 30`)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "repositories_unavailable", "Repositories unavailable")
		return
	}
	defer rows.Close()
	type item struct {
		RepositoryID string    `json:"repository_id"`
		URL          string    `json:"url"`
		SnapshotID   string    `json:"snapshot_id"`
		State        string    `json:"state"`
		CommitOID    *string   `json:"commit_oid"`
		CreatedAt    time.Time `json:"created_at"`
	}
	items := make([]item, 0)
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.RepositoryID, &value.URL, &value.SnapshotID, &value.State, &value.CommitOID, &value.CreatedAt); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "repositories_unavailable", "Repositories unavailable")
			return
		}
		items = append(items, value)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "repositories_unavailable", "Repositories unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": items})
}

func (server *Server) snapshotExists(r *http.Request) bool {
	var exists bool
	err := server.DB.QueryRow(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM snapshots s JOIN repositories repo ON repo.id=s.repository_id
		WHERE s.id=$1 AND s.repository_id=$2 AND s.state='ready' AND repo.source_kind='github')`,
		r.PathValue("snapshotId"), r.PathValue("repositoryId")).Scan(&exists)
	return err == nil && exists
}

func (server *Server) indexSnapshot(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	inserted, err := server.Jobs.Insert(ctx, jobs.IngestArgs{RepositoryID: r.PathValue("repositoryId"), SnapshotID: r.PathValue("snapshotId")}, nil)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "index_queue_failed", "Cannot queue snapshot indexing")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"repository_id": r.PathValue("repositoryId"), "snapshot_id": r.PathValue("snapshotId"), "job_id": strconv.FormatInt(inserted.Job.ID, 10), "state": "queued"})
}

func (server *Server) searchEvidence(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 200 {
		writeProblem(w, http.StatusBadRequest, "invalid_query", "Search query must be 1-200 bytes")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "semantic" {
		server.semanticSearch(w, r, query)
		return
	}
	if mode != "" && mode != "lexical" {
		writeProblem(w, http.StatusBadRequest, "invalid_mode", "Search mode must be lexical or semantic")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			writeProblem(w, http.StatusBadRequest, "invalid_limit", "Limit must be 1-50")
			return
		}
		limit = parsed
	}
	rows, err := server.DB.Query(r.Context(), `SELECT id,path,start_line,end_line,language,source_kind,content,content_hash
		FROM evidence_chunks WHERE snapshot_id=$1 AND
		(strpos(lower(content),lower($2))>0 OR strpos(lower(path),lower($2))>0 OR search_vector @@ plainto_tsquery('simple',$2))
		ORDER BY (strpos(lower(content),lower($2))>0) DESC,
			(strpos(lower(path),lower($2))>0) DESC,
			ts_rank(search_vector,plainto_tsquery('simple',$2)) DESC,path,start_line LIMIT $3`,
		r.PathValue("snapshotId"), query, limit)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "search_unavailable", "Search unavailable")
		return
	}
	defer rows.Close()
	results := make([]index.Chunk, 0)
	for rows.Next() {
		var chunk index.Chunk
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.StartLine, &chunk.EndLine, &chunk.Language, &chunk.SourceKind, &chunk.Content, &chunk.ContentHash); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "search_unavailable", "Search unavailable")
			return
		}
		if _, err := privacy.Clear(chunk.Path, []byte(chunk.Content), nil); err != nil {
			continue
		}
		results = append(results, chunk)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "search_unavailable", "Search unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "query": query, "results": results, "retrieval": "exact_lexical"})
}

func (server *Server) semanticSearch(w http.ResponseWriter, r *http.Request, query string) {
	provider := r.URL.Query().Get("provider")
	adapter, err := providers.ConfiguredEmbedding(server.Config, provider)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "provider_unavailable", err.Error())
		return
	}
	var privacyMode string
	if err := server.DB.QueryRow(r.Context(), `SELECT privacy_mode FROM repositories WHERE id=$1`, r.PathValue("repositoryId")).Scan(&privacyMode); err != nil {
		writeProblem(w, http.StatusNotFound, "repository_not_found", "Repository not found")
		return
	}
	request := privacy.ModelRequest{Mode: server.Config.ModelMode, RepoOptedIn: privacyMode == "cloud_opt_in"}
	vector, err := adapter.Embed(r.Context(), request, query)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "semantic_unavailable", err.Error())
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			writeProblem(w, http.StatusBadRequest, "invalid_limit", "Limit must be 1-50")
			return
		}
		limit = parsed
	}
	rows, err := server.DB.Query(r.Context(), `SELECT e.id,e.path,e.start_line,e.end_line,e.language,e.source_kind,e.content,e.content_hash
		FROM evidence_embeddings b JOIN evidence_chunks e ON e.id=b.evidence_id
		WHERE e.snapshot_id=$1 AND b.provider=$2 AND b.model=$3 AND b.dimensions=$4 AND b.content_hash=e.content_hash
		ORDER BY b.embedding <=> $5::vector LIMIT $6`, r.PathValue("snapshotId"), adapter.Kind, adapter.Model,
		adapter.Dimensions, jobs.VectorLiteral(vector), limit)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "semantic_unavailable", "Semantic search unavailable")
		return
	}
	defer rows.Close()
	results := make([]index.Chunk, 0)
	for rows.Next() {
		var chunk index.Chunk
		if err := rows.Scan(&chunk.ID, &chunk.Path, &chunk.StartLine, &chunk.EndLine, &chunk.Language, &chunk.SourceKind, &chunk.Content, &chunk.ContentHash); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "semantic_unavailable", "Semantic search unavailable")
			return
		}
		if _, err := privacy.Clear(chunk.Path, []byte(chunk.Content), nil); err != nil {
			continue
		}
		results = append(results, chunk)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "semantic_unavailable", "Semantic search unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "query": query, "results": results,
		"retrieval": "semantic", "provider": adapter.Kind, "model": adapter.Model, "dimensions": adapter.Dimensions})
}

func (server *Server) getEvidence(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	var chunk index.Chunk
	err := server.DB.QueryRow(r.Context(), `SELECT id,path,start_line,end_line,language,source_kind,content,content_hash
		FROM evidence_chunks WHERE snapshot_id=$1 AND id=$2`, r.PathValue("snapshotId"), r.PathValue("evidenceId")).Scan(
		&chunk.ID, &chunk.Path, &chunk.StartLine, &chunk.EndLine, &chunk.Language, &chunk.SourceKind, &chunk.Content, &chunk.ContentHash)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "evidence_not_found", "Evidence not found")
		return
	}
	if _, err := privacy.Clear(chunk.Path, []byte(chunk.Content), nil); err != nil {
		writeProblem(w, http.StatusNotFound, "evidence_not_found", "Evidence not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence_id": chunk.ID, "repository_id": r.PathValue("repositoryId"),
		"snapshot_id": r.PathValue("snapshotId"), "source_kind": chunk.SourceKind,
		"source_ranges": []map[string]any{{"artifact_id": chunk.Path, "start_line": chunk.StartLine, "end_line": chunk.EndLine}},
		"extractor_id":  "line_chunker", "extractor_version": index.ExtractorVersion, "resolution": "resolved",
		"sanitized": true, "snippet": chunk.Content})
}

func (server *Server) getCapabilities(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	rows, err := server.DB.Query(r.Context(), `SELECT path,language,text_status,syntax_status,semantic_status,framework_status,reason
		FROM artifact_capabilities WHERE snapshot_id=$1 ORDER BY path LIMIT 10001`, r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "capabilities_unavailable", "Capabilities unavailable")
		return
	}
	defer rows.Close()
	items := make([]index.Capability, 0)
	for rows.Next() {
		var item index.Capability
		if err := rows.Scan(&item.Path, &item.Language, &item.Text, &item.Syntax, &item.Semantic, &item.Framework, &item.Reason); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "capabilities_unavailable", "Capabilities unavailable")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "capabilities_unavailable", "Capabilities unavailable")
		return
	}
	truncated := len(items) > 10000
	if truncated {
		items = items[:10000]
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "items": items, "limit": 10000, "truncated": truncated})
}

func (server *Server) getSymbols(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 200 {
		writeProblem(w, http.StatusBadRequest, "invalid_query", "Query too long")
		return
	}
	rows, err := server.DB.Query(r.Context(), `SELECT id,path,name,qualified_name,kind,language,start_line,end_line
		FROM symbols WHERE snapshot_id=$1 AND ($2='' OR strpos(lower(name),lower($2))>0)
		ORDER BY name,path,start_line LIMIT 101`, r.PathValue("snapshotId"), query)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "symbols_unavailable", "Symbols unavailable")
		return
	}
	defer rows.Close()
	items := make([]index.Symbol, 0)
	for rows.Next() {
		var item index.Symbol
		if err := rows.Scan(&item.ID, &item.Path, &item.Name, &item.QualifiedName, &item.Kind, &item.Language, &item.StartLine, &item.EndLine); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "symbols_unavailable", "Symbols unavailable")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "symbols_unavailable", "Symbols unavailable")
		return
	}
	truncated := len(items) > 100
	if truncated {
		items = items[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "symbols": items, "truncated": truncated})
}

func (server *Server) getGraph(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	root := r.URL.Query().Get("symbol_id")
	if root == "" || len(root) > 128 {
		writeProblem(w, http.StatusBadRequest, "invalid_symbol", "A symbol_id is required")
		return
	}
	var rootSymbol index.Symbol
	err := server.DB.QueryRow(r.Context(), `SELECT id,path,name,qualified_name,kind,language,start_line,end_line
		FROM symbols WHERE snapshot_id=$1 AND id=$2`, r.PathValue("snapshotId"), root).Scan(
		&rootSymbol.ID, &rootSymbol.Path, &rootSymbol.Name, &rootSymbol.QualifiedName, &rootSymbol.Kind, &rootSymbol.Language, &rootSymbol.StartLine, &rootSymbol.EndLine)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "symbol_not_found", "Symbol not found")
		return
	}
	const maxHops, maxNodes, maxOutgoing = 6, 200, 20
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := graph.Traverse(ctx, root, graph.Limits{Hops: maxHops, Nodes: maxNodes, Outgoing: maxOutgoing}, func(ctx context.Context, id string, limit int) ([]graph.Edge, error) {
		rows, err := server.DB.Query(ctx, `SELECT id,from_id,to_id,kind,resolution,path,line FROM relations
			WHERE snapshot_id=$1 AND from_id=$2 ORDER BY id LIMIT $3`, r.PathValue("snapshotId"), id, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		outgoing := make([]graph.Edge, 0)
		for rows.Next() {
			var edge graph.Edge
			if err := rows.Scan(&edge.ID, &edge.FromID, &edge.ToID, &edge.Kind, &edge.Resolution, &edge.Path, &edge.Line); err != nil {
				return nil, err
			}
			outgoing = append(outgoing, edge)
		}
		return outgoing, rows.Err()
	})
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "graph_unavailable", "Graph traversal unavailable or timed out")
		return
	}
	nodes := []index.Symbol{rootSymbol}
	for _, id := range result.NodeIDs[1:] {
		var symbol index.Symbol
		err := server.DB.QueryRow(r.Context(), `SELECT id,path,name,qualified_name,kind,language,start_line,end_line
			FROM symbols WHERE snapshot_id=$1 AND id=$2`, r.PathValue("snapshotId"), id).Scan(
			&symbol.ID, &symbol.Path, &symbol.Name, &symbol.QualifiedName, &symbol.Kind, &symbol.Language, &symbol.StartLine, &symbol.EndLine)
		if err == nil {
			nodes = append(nodes, symbol)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "root_id": root,
		"nodes": nodes, "edges": result.Edges, "truncated": result.Truncated, "limits": map[string]int{"hops": maxHops, "nodes": maxNodes, "outgoing": maxOutgoing}})
}
