package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"onboardmeplease/internal/config"
	"onboardmeplease/internal/graph"
	"onboardmeplease/internal/grounding"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
)

type ConceptHit struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Markdown    string   `json:"markdown"`
	EvidenceIDs []string `json:"evidence_ids"`
}
type Retrieval struct {
	Sources  []grounding.Source `json:"sources"`
	Concepts []ConceptHit       `json:"concepts"`
	Edges    []graph.Edge       `json:"edges"`
	Gaps     []string           `json:"gaps"`
}
type RetrievalOptions struct{ Knowledge, Graph, Vectors bool }
type candidate struct {
	id    string
	score float64
}

func vectorLiteral(v []float32) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.FormatFloat(float64(n), 'g', -1, 32)
	}
	return "[" + strings.Join(s, ",") + "]"
}

// Retrieve fuses ranked candidates, then spends a single bounded source/graph
// budget. Knowledge hits reopen their original source and cannot prove claims.
func Retrieve(ctx context.Context, pool *pgxpool.Pool, c config.Config, policy privacy.ModelRequest, snapshot string, queries, priorIDs []string, options RetrievalOptions) (Retrieval, error) {
	result := Retrieval{Sources: []grounding.Source{}, Concepts: []ConceptHit{}, Edges: []graph.Edge{}, Gaps: []string{}}
	if len(queries) > 4 {
		queries = queries[:4]
	}
	ranks := map[string]float64{}
	addRows := func(query string, args ...any) error {
		rows, err := pool.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		i := 0
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				return err
			}
			i++
			ranks[id] += 1 / float64(60+i)
		}
		return rows.Err()
	}
	for _, q := range queries {
		if len(strings.TrimSpace(q)) == 0 {
			continue
		}
		if err := addRows(`SELECT 's:'||id FROM evidence_chunks WHERE snapshot_id=$1 AND (strpos(lower(content),lower($2))>0 OR strpos(lower(path),lower($2))>0) ORDER BY (strpos(lower(path),lower($2))>0) DESC,path,start_line LIMIT 20`, snapshot, q); err != nil {
			return result, err
		}
		if err := addRows(`SELECT 's:'||id FROM evidence_chunks WHERE snapshot_id=$1 AND search_vector @@ websearch_to_tsquery('simple',$2) ORDER BY ts_rank(search_vector,websearch_to_tsquery('simple',$2)) DESC,id LIMIT 20`, snapshot, q); err != nil {
			return result, err
		}
		if options.Knowledge {
			if err := addRows(`SELECT 'k:'||id FROM knowledge_concepts WHERE snapshot_id=$1 AND version=(SELECT max(version) FROM overview_versions WHERE snapshot_id=$1) AND (search_vector @@ websearch_to_tsquery('simple',$2) OR strpos(lower(title),lower($2))>0) ORDER BY (strpos(lower(title),lower($2))>0) DESC,ts_rank(search_vector,websearch_to_tsquery('simple',$2)) DESC,id LIMIT 12`, snapshot, q); err != nil {
				return result, err
			}
		}
	}
	if options.Vectors {
		kind := c.GenerationProvider
		adapter, err := providers.ConfiguredEmbedding(c, kind)
		if err != nil {
			result.Gaps = append(result.Gaps, "Vector retrieval unavailable: no matching embedding provider configured.")
		} else {
			// Avoid spending a query embedding when this snapshot has no vectors.
			var count int
			err = pool.QueryRow(ctx, `SELECT count(*) FROM evidence_embeddings b JOIN evidence_chunks e ON e.id=b.evidence_id WHERE e.snapshot_id=$1 AND b.provider=$2 AND b.model=$3 AND b.dimensions=$4 AND b.content_hash=e.content_hash`, snapshot, adapter.Kind, adapter.Model, adapter.Dimensions).Scan(&count)
			if err != nil {
				return result, err
			}
			if count == 0 {
				result.Gaps = append(result.Gaps, "Vector retrieval unavailable: this snapshot has not been embedded.")
			} else {
				vec, e := adapter.Embed(ctx, policy, strings.Join(queries, "\n"))
				if e != nil {
					result.Gaps = append(result.Gaps, "Vector query failed; exact and lexical retrieval were used.")
				} else {
					literal := vectorLiteral(vec)
					if err = addRows(`SELECT 's:'||e.id FROM evidence_embeddings b JOIN evidence_chunks e ON e.id=b.evidence_id WHERE e.snapshot_id=$1 AND b.provider=$2 AND b.model=$3 AND b.dimensions=$4 AND b.content_hash=e.content_hash ORDER BY b.embedding <=> $5::vector,e.id LIMIT 20`, snapshot, adapter.Kind, adapter.Model, adapter.Dimensions, literal); err != nil {
						return result, err
					}
					if options.Knowledge {
						if err = addRows(`SELECT 'k:'||k.id FROM knowledge_embeddings b JOIN knowledge_concepts k ON (k.snapshot_id,k.version,k.id)=(b.snapshot_id,b.version,b.concept_id) WHERE k.snapshot_id=$1 AND k.version=(SELECT max(version) FROM overview_versions WHERE snapshot_id=$1) AND b.provider=$2 AND b.model=$3 AND b.dimensions=$4 AND b.content_hash=k.content_hash ORDER BY b.embedding <=> $5::vector,k.id LIMIT 12`, snapshot, adapter.Kind, adapter.Model, adapter.Dimensions, literal); err != nil {
							return result, err
						}
					}
				}
			}
		}
	}
	// Natural-language plans can be too conjunctive for code identifiers. If no
	// store returned candidates, relax conjunctions while retaining PostgreSQL's
	// parsed query syntax and the same candidate/context limits.
	if len(ranks) == 0 {
		for _, q := range queries {
			if strings.TrimSpace(q) == "" {
				continue
			}
			if err := addRows(`WITH query AS (SELECT to_tsquery('simple',replace(websearch_to_tsquery('simple',$2)::text,' & ',' | ')) AS terms) SELECT 's:'||e.id FROM evidence_chunks e CROSS JOIN query WHERE e.snapshot_id=$1 AND e.search_vector @@ query.terms ORDER BY ts_rank(e.search_vector,query.terms) DESC,e.id LIMIT 20`, snapshot, q); err != nil {
				return result, err
			}
			if options.Knowledge {
				if err := addRows(`WITH query AS (SELECT to_tsquery('simple',replace(websearch_to_tsquery('simple',$2)::text,' & ',' | ')) AS terms) SELECT 'k:'||k.id FROM knowledge_concepts k CROSS JOIN query WHERE k.snapshot_id=$1 AND k.version=(SELECT max(version) FROM overview_versions WHERE snapshot_id=$1) AND k.search_vector @@ query.terms ORDER BY ts_rank(k.search_vector,query.terms) DESC,k.id LIMIT 12`, snapshot, q); err != nil {
					return result, err
				}
			}
		}
		if len(ranks) > 0 {
			result.Gaps = append(result.Gaps, "No complete phrase matched; lexical retrieval broadened to individual query terms within the same source budget.")
		}
	}
	candidates := []candidate{}
	for id, score := range ranks {
		candidates = append(candidates, candidate{id, score})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].score > candidates[j].score
	})
	ids := []string{}
	seen := map[string]bool{}
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	knowledgeBytes := 0
	// Expand concept dependencies and direct sources in fused rank order.
	// Large concepts can consume the source budget; report any omitted sources.
	for _, candidate := range candidates {
		id := candidate.id[2:]
		if strings.HasPrefix(candidate.id, "s:") {
			add(id)
		} else if len(result.Concepts) < 6 {
			var hit ConceptHit
			err := pool.QueryRow(ctx, `SELECT id,title,markdown,evidence_ids FROM knowledge_concepts WHERE snapshot_id=$1 AND version=(SELECT max(version) FROM overview_versions WHERE snapshot_id=$1) AND id=$2`, snapshot, id).Scan(&hit.ID, &hit.Title, &hit.Markdown, &hit.EvidenceIDs)
			if err != nil {
				return result, err
			}
			if knowledgeBytes+len(hit.Markdown) > 24000 {
				result.Gaps = append(result.Gaps, "Knowledge context reached its shared byte budget.")
				continue
			}
			knowledgeBytes += len(hit.Markdown)
			result.Concepts = append(result.Concepts, hit)
			for _, e := range hit.EvidenceIDs {
				add(e)
			}
		}
	}
	for _, id := range priorIDs {
		add(id)
	}
	if len(ids) > 120 {
		ids = ids[:120]
		result.Gaps = append(result.Gaps, "Candidate retrieval reached its 120-source limit.")
	}
	if len(ids) == 0 {
		return result, nil
	}
	sources, err := ReadSources(ctx, pool, snapshot, ids)
	if err != nil {
		return result, err
	}
	byID := map[string]grounding.Source{}
	for _, s := range sources {
		byID[s.ID] = s
	}
	budget := 0
	used := map[string]bool{}
	appendSource := func(s grounding.Source) bool {
		if used[s.ID] {
			return true
		}
		if len(result.Sources) >= 28 || budget+len(s.Content) > 65000 {
			return false
		}
		used[s.ID] = true
		budget += len(s.Content)
		result.Sources = append(result.Sources, s)
		return true
	}
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			if !appendSource(s) {
				result.Gaps = append(result.Gaps, "Source reads reached their shared byte/chunk budget.")
				continue
			}
		}
	}
	// One adjacent chunk on each side can complete a split function even when
	// this language has no parser. This is not recursive and shares the budget.
	seeds := []string{}
	for _, source := range result.Sources {
		seeds = append(seeds, source.ID)
	}
	if len(seeds) > 0 {
		rows, e := pool.Query(ctx, `SELECT DISTINCT n.id FROM evidence_chunks s JOIN evidence_chunks n ON n.snapshot_id=s.snapshot_id AND n.path=s.path AND (n.start_line=s.end_line+1 OR n.end_line=s.start_line-1) WHERE s.snapshot_id=$1 AND s.id=ANY($2::text[]) ORDER BY n.id LIMIT 56`, snapshot, seeds)
		if e != nil {
			return result, e
		}
		neighbors := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			neighbors = append(neighbors, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		if len(neighbors) > 0 {
			extra, e := ReadSources(ctx, pool, snapshot, neighbors)
			if e != nil {
				return result, e
			}
			for _, source := range extra {
				if !appendSource(source) {
					result.Gaps = append(result.Gaps, "Some adjacent source chunks could not be read within the shared budget.")
					break
				}
			}
		}
	}
	if options.Graph && len(result.Sources) > 0 {
		paths := []string{}
		for _, s := range result.Sources {
			paths = append(paths, s.Path)
		}
		rows, e := pool.Query(ctx, `SELECT id FROM symbols WHERE snapshot_id=$1 AND path=ANY($2::text[]) ORDER BY path,start_line LIMIT 8`, snapshot, paths)
		if e != nil {
			return result, e
		}
		roots := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			roots = append(roots, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		expanded := map[string]bool{}
		edgeIDs := map[string]bool{}
		fetch := func(ctx context.Context, node string, limit int) ([]graph.Edge, error) {
			if expanded[node] {
				return nil, nil
			}
			if len(expanded) >= 60 {
				return nil, nil
			}
			expanded[node] = true
			rows, e := pool.Query(ctx, `SELECT id,from_id,to_id,kind,resolution,path,line FROM relations WHERE snapshot_id=$1 AND from_id=$2 AND resolution='resolved' ORDER BY id LIMIT $3`, snapshot, node, limit)
			if e != nil {
				return nil, e
			}
			defer rows.Close()
			out := []graph.Edge{}
			for rows.Next() {
				var edge graph.Edge
				if e = rows.Scan(&edge.ID, &edge.FromID, &edge.ToID, &edge.Kind, &edge.Resolution, &edge.Path, &edge.Line); e != nil {
					return nil, e
				}
				out = append(out, edge)
			}
			return out, rows.Err()
		}
		nodes := []string{}
		for _, root := range roots {
			walk, e := graph.Traverse(ctx, root, graph.Limits{Hops: 3, Nodes: 60, Outgoing: 8}, fetch)
			if e != nil {
				return result, e
			}
			nodes = append(nodes, walk.NodeIDs...)
			if walk.Truncated {
				result.Gaps = append(result.Gaps, "Graph exploration was truncated by hop/node/fan-out limits.")
			}
			for _, edge := range walk.Edges {
				if !edgeIDs[edge.ID] && len(result.Edges) < 48 {
					edgeIDs[edge.ID] = true
					result.Edges = append(result.Edges, edge)
				}
			}
		}
		if len(expanded) >= 60 {
			result.Gaps = append(result.Gaps, "Graph expansion reached its shared 60-node limit.")
		}
		rows, e = pool.Query(ctx, `SELECT DISTINCT e.id FROM evidence_chunks e JOIN symbols s ON s.snapshot_id=e.snapshot_id AND s.path=e.path AND s.start_line BETWEEN e.start_line AND e.end_line WHERE s.snapshot_id=$1 AND s.id=ANY($2::text[]) ORDER BY e.id LIMIT 60`, snapshot, nodes)
		if e != nil {
			return result, e
		}
		extraIDs := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			extraIDs = append(extraIDs, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		if len(extraIDs) > 0 {
			extra, e := ReadSources(ctx, pool, snapshot, extraIDs)
			if e != nil {
				return result, e
			}
			for _, s := range extra {
				if !appendSource(s) {
					result.Gaps = append(result.Gaps, "Some graph targets could not be read within the source budget.")
					break
				}
			}
		}
	}
	// Only expose derived documents whose dependencies were actually reopened.
	filtered := []ConceptHit{}
	for _, hit := range result.Concepts {
		complete := true
		for _, id := range hit.EvidenceIDs {
			if !used[id] {
				complete = false
			}
		}
		if complete {
			filtered = append(filtered, hit)
		}
	}
	result.Concepts = filtered
	if len(result.Concepts) == 0 && options.Knowledge {
		result.Gaps = append(result.Gaps, "No current knowledge concept with fully reopened evidence matched this question.")
	}
	result.Gaps = uniqueStrings(result.Gaps)
	return result, nil
}

func uniqueStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range values {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func retrievalSummary(r Retrieval) string {
	return fmt.Sprintf("%d source chunks, %d OKF concepts, %d static graph links", len(r.Sources), len(r.Concepts), len(r.Edges))
}
