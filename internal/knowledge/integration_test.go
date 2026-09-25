package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"onboardmeplease/internal/config"
	"onboardmeplease/internal/grounding"
	"onboardmeplease/internal/index"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
	"onboardmeplease/internal/repository"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "parse-go" {
		if index.RunParser(os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Only opt-in integration runs access a database. Fixtures use unique IDs and
// cleanup deletes only the test repository, never user snapshots.
func integrationFixture(t *testing.T) (context.Context, *pgxpool.Pool, config.Config, string, string) {
	t.Helper()
	if os.Getenv("OMP_TEST_DB") != "1" {
		t.Skip("set OMP_TEST_DB=1 with a migrated test database")
	}
	ctx := context.Background()
	c, err := config.Load()
	if err != nil {
		t.Fatal("test configuration unavailable")
	}
	pool, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	t.Cleanup(pool.Close)
	repo, _ := repository.NewID()
	snapshot, _ := repository.NewID()
	if _, err = pool.Exec(ctx, `INSERT INTO repositories(id,source_kind,source_locator,privacy_mode) VALUES($1,'github','https://github.com/onboardmeplease/synthetic-evaluation','cloud_opt_in')`, repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, e := pool.Exec(context.Background(), `DELETE FROM repositories WHERE id=$1`, repo)
		if e != nil {
			t.Error("test fixture cleanup failed")
		}
	})
	if _, err = pool.Exec(ctx, `INSERT INTO snapshots(id,repository_id,state,source_kind,commit_oid) VALUES($1,$2,'ready','commit',$3)`, snapshot, repo, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	root := os.Getenv("OMP_FIXTURE_ROOT")
	if root == "" {
		root = filepath.Join("..", "..", "testdata", "mixed-monolith")
	}
	data := t.TempDir()
	files := filepath.Join(data, "snapshots", snapshot, "files")
	manifest := repository.Manifest{SnapshotID: snapshot, RepositoryID: repo, CommitOID: strings.Repeat("a", 40)}
	err = filepath.WalkDir(root, func(file string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, file)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		// Explicitly test useful analysis without a README.
		if strings.EqualFold(name, "README.md") {
			return nil
		}
		content, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(content)
		digest := hex.EncodeToString(hash[:])
		status := "pending"
		reasons := []string{}
		if _, e = privacy.Clear(name, content, nil); e != nil {
			status = "excluded"
			reasons = []string{"privacy_excluded"}
		}
		artifact := repository.Artifact{Path: name, ContentHash: digest, ByteSize: int64(len(content)), Status: status, ReasonCodes: reasons}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
		if _, e = pool.Exec(ctx, `INSERT INTO artifacts(snapshot_id,path,content_hash,byte_size,status,reason_codes) VALUES($1,$2,$3,$4,$5,$6)`, snapshot, name, digest, len(content), status, reasons); e != nil {
			return e
		}
		if status == "pending" {
			target := filepath.Join(files, rel)
			if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
				return e
			}
			return os.WriteFile(target, content, 0600)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(data, "snapshots", snapshot, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = index.Build(ctx, pool, data, repo, snapshot); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, c, repo, snapshot
}

type fixtureModel struct{ UnitCalls int }

func (m *fixtureModel) JSON(_ context.Context, _ privacy.ModelRequest, instructions string, input any, schema json.RawMessage) ([]byte, error) {
	raw, _ := json.Marshal(input)
	var data struct {
		Sources []grounding.Source `json:"source_evidence"`
		Claims  []grounding.Claim  `json:"claims"`
	}
	json.Unmarshal(raw, &data)
	if strings.Contains(string(schema), `"assessments"`) {
		a := []grounding.Assessment{}
		for i := range data.Claims {
			a = append(a, grounding.Assessment{Index: i, Status: "supported", Reason: "Fixture source"})
		}
		return json.Marshal(map[string]any{"assessments": a})
	}
	if strings.Contains(string(schema), `"queries"`) {
		return []byte(`{"queries":["cancel OR refund OR saveOrderState"]}`), nil
	}
	if strings.Contains(instructions, "partial source units") {
		m.UnitCalls++
	}
	claims := []grounding.Claim{}
	for _, s := range data.Sources {
		if strings.HasSuffix(s.Path, "server/store.go") {
			claims = append(claims, grounding.Claim{Text: "saveOrderState is a stub; a real database write is not established.", Section: "data", Concept: "Settlement policy", EvidenceIDs: []string{s.ID}})
		}
		if strings.HasSuffix(s.Path, "server/cancel.go") {
			claims = append(claims, grounding.Claim{Text: "The paid-order handler returns 202 only after processCancellation succeeds.", Section: "purpose", Concept: "Settlement policy", EvidenceIDs: []string{s.ID}})
		}
		if strings.HasSuffix(s.Path, "worker/refund_worker.py") {
			claims = append(claims, grounding.Claim{Text: "The worker requests a refund using the attempt ID for idempotency.", Section: "flows", Concept: "Settlement policy", EvidenceIDs: []string{s.ID}})
		}
	}
	return json.Marshal(grounding.Result{Claims: claims, Gaps: []string{"Broker delivery remains unresolved."}})
}

func TestDatabaseSynthesisKnowledgeRetrievalFollowups(t *testing.T) {
	ctx, pool, c, repo, snapshot := integrationFixture(t)
	model := &fixtureModel{}
	policy := privacy.ModelRequest{Mode: "strict_local"}
	overview, err := SynthesizeWith(ctx, pool, model, policy, "test", "fixture", repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Synthesis.ReviewedChunks != overview.Synthesis.TotalChunks || len(overview.Concepts) == 0 {
		t.Fatalf("incomplete fixture analysis: %+v", overview.Synthesis)
	}
	if len(overview.Sections[0].ClaimIDs) == 0 {
		t.Fatal("no purpose without README")
	}
	calls := model.UnitCalls
	again, err := SynthesizeWith(ctx, pool, model, policy, "test", "fixture", repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if model.UnitCalls != calls {
		t.Fatal("successful units were not cached")
	}
	got, err := Build(ctx, pool, repo, snapshot)
	if err != nil || got.Version != again.Version || got.Synthesis == nil {
		t.Fatal("baseline overwrote synthesized knowledge")
	}
	result, err := Retrieve(ctx, pool, c, policy, snapshot, []string{"Settlement policy"}, nil, RetrievalOptions{Knowledge: true, Graph: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Concepts) == 0 || len(result.Sources) < 3 {
		t.Fatal("knowledge hit did not reopen underlying source")
	}
	// A natural question may contain terms absent from every file. Preserve
	// useful partial lexical matches instead of requiring a vector index.
	fallback, err := Retrieve(ctx, pool, c, policy, snapshot, []string{"cancel refund settlement zzz_unmatched_term"}, nil, RetrievalOptions{Knowledge: true})
	if err != nil || len(fallback.Sources) == 0 || len(fallback.Concepts) == 0 {
		t.Fatalf("natural-language lexical fallback lost source or OKF: %v", err)
	}
	if _, err = Export(ctx, pool, repo, snapshot); err != nil {
		t.Fatal(err)
	}
	answer, err := AskWith(ctx, pool, c, model, policy, repo, snapshot, "What happens after paid cancellation?", "", RetrievalOptions{Knowledge: true, Graph: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Claims) == 0 || len(answer.Sources) == 0 {
		t.Fatal("empty grounded answer")
	}
	if _, err = AskWith(ctx, pool, c, model, policy, repo, snapshot, "What about retries?", answer.ID, RetrievalOptions{Knowledge: true, Graph: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = AskWith(ctx, pool, c, model, policy, repo, snapshot, "Follow up", "another-snapshot-turn", RetrievalOptions{}); err == nil {
		t.Fatal("cross-snapshot follow-up accepted")
	}
	// Changing the source index invalidates knowledge; retrieval must not use
	// old concepts while the replacement generation has not run.
	if _, err = pool.Exec(ctx, `UPDATE evidence_chunks SET content_hash='changed-for-test' WHERE snapshot_id=$1`, snapshot); err != nil {
		t.Fatal(err)
	}
	fresh, err := Build(ctx, pool, repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Synthesis != nil {
		t.Fatal("stale knowledge remained current")
	}
	result, err = Retrieve(ctx, pool, c, policy, snapshot, []string{"Settlement policy"}, nil, RetrievalOptions{Knowledge: true})
	if err != nil || len(result.Concepts) != 0 {
		t.Fatal("stale knowledge was retrieved")
	}
}

func TestDatabaseAdjacentSourceReadIsOneHop(t *testing.T) {
	ctx, pool, c, repo, snapshot := integrationFixture(t)
	var seed string
	if err := pool.QueryRow(ctx, `SELECT id FROM evidence_chunks WHERE snapshot_id=$1 AND source_kind='code' ORDER BY path,start_line LIMIT 1`, snapshot).Scan(&seed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE evidence_chunks SET content='adjacency_probe_marker' WHERE id=$1`, seed); err != nil {
		t.Fatal(err)
	}
	previous := seed
	for _, id := range []string{stableID(snapshot, "adjacent"), stableID(snapshot, "beyond-adjacent")} {
		if _, err := pool.Exec(ctx, `INSERT INTO evidence_chunks(id,snapshot_id,path,start_line,end_line,language,source_kind,content,content_hash,extractor_version) SELECT $1,snapshot_id,path,end_line+1,end_line+1,language,source_kind,'return result','test-hash',extractor_version FROM evidence_chunks WHERE id=$2`, id, previous); err != nil {
			t.Fatal(err)
		}
		previous = id
	}
	policy, err := Policy(ctx, pool, c, repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Retrieve(ctx, pool, c, policy, snapshot, []string{"adjacency_probe_marker"}, nil, RetrievalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, source := range got.Sources {
		seen[source.ID] = true
	}
	if !seen[seed] || !seen[stableID(snapshot, "adjacent")] || seen[stableID(snapshot, "beyond-adjacent")] {
		t.Fatal("adjacent source expansion lost its seed, missed the continuation, or recursed")
	}
}

// Live evaluation is separately opt-in; mocked tests never satisfy this gate.
func TestLiveArchitectureEvaluation(t *testing.T) {
	if os.Getenv("OMP_LIVE_EVAL") != "1" {
		t.Skip("explicit live-model evaluation only")
	}
	ctx, pool, c, repo, snapshot := integrationFixture(t)
	started := time.Now()
	overview, err := Synthesize(ctx, pool, c, repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sections[0].ClaimIDs) == 0 {
		t.Error("live model produced no source-derived purpose")
	}
	if diagnostic, e := json.MarshalIndent(overview, "", "  "); e == nil {
		_ = os.WriteFile(os.Getenv("OMP_EVAL_OUTPUT")+".overview.json", diagnostic, 0600)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("OMP_EVAL_ROOT"), "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		Cases []struct {
			ID, Question    string
			RequiredAnchors []struct{ Path, Contains string } `json:"required_anchors"`
		}
	}
	if json.Unmarshal(raw, &suite) != nil {
		t.Fatal("invalid eval cases")
	}
	policy, err := Policy(ctx, pool, c, repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Embed the same fixture and current knowledge once for an actual vector
	// ablation; do not label the no-vector fallback as a vector measurement.
	embedder, err := providers.ConfiguredEmbedding(c, c.GenerationProvider)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := ReadSources(ctx, pool, snapshot, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		vector, e := embedder.Embed(ctx, policy, source.Content)
		if e != nil {
			t.Fatal(e)
		}
		_, e = pool.Exec(ctx, `INSERT INTO evidence_embeddings(evidence_id,provider,model,dimensions,content_hash,embedding) SELECT id,$2,$3,$4,content_hash,$5::vector FROM evidence_chunks WHERE id=$1`, source.ID, embedder.Kind, embedder.Model, embedder.Dimensions, vectorLiteral(vector))
		if e != nil {
			t.Fatal(e)
		}
	}
	rows, err := pool.Query(ctx, `SELECT version,id,markdown,content_hash FROM knowledge_concepts WHERE snapshot_id=$1`, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var version int
		var id, markdown, hash string
		if rows.Scan(&version, &id, &markdown, &hash) != nil {
			t.Fatal("cannot read concepts")
		}
		vector, e := embedder.Embed(ctx, policy, markdown)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO knowledge_embeddings(snapshot_id,version,concept_id,provider,model,dimensions,content_hash,embedding) VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector)`, snapshot, version, id, embedder.Kind, embedder.Model, embedder.Dimensions, hash, vectorLiteral(vector)); e != nil {
			t.Fatal(e)
		}
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	rows.Close()
	report := map[string]any{"model": c.GenerationModel, "overview": overview, "review_status": "requires_human_semantic_review"}
	results := []map[string]any{}
	for _, test := range suite.Cases {
		if test.ID != "paid-cancel-technical-trace" && test.ID != "event-consumer-gap" && test.ID != "guarded-cycle" {
			continue
		}
		answer, e := Ask(ctx, pool, c, repo, snapshot, test.Question, "")
		if e != nil {
			t.Fatal(e)
		}
		comparisons := map[string]any{}
		for name, options := range map[string]RetrievalOptions{"source": {}, "source_graph": {Graph: true}, "source_vectors": {Vectors: true}, "all_without_graph": {Knowledge: true, Vectors: true}, "source_okf": {Knowledge: true}, "all": {Knowledge: true, Graph: true, Vectors: true}} {
			r, e := Retrieve(ctx, pool, c, policy, snapshot, []string{test.Question, "cancel OR refund OR saveOrderState"}, nil, options)
			if e != nil {
				t.Fatal(e)
			}
			found := 0
			for _, anchor := range test.RequiredAnchors {
				for _, s := range r.Sources {
					if s.Path == anchor.Path && strings.Contains(s.Content, anchor.Contains) {
						found++
						break
					}
				}
			}
			comparisons[name] = map[string]any{"anchor_recall": fmt.Sprintf("%d/%d", found, len(test.RequiredAnchors)), "source_chunks": len(r.Sources), "concepts": len(r.Concepts), "gaps": r.Gaps}
		}
		results = append(results, map[string]any{"case": test.ID, "answer": answer, "retrieval_comparison": comparisons})
	}
	report["cases"] = results
	report["elapsed_seconds"] = time.Since(started).Seconds()
	encoded, _ := json.MarshalIndent(report, "", "  ")
	output := os.Getenv("OMP_EVAL_OUTPUT")
	if output == "" {
		t.Fatal("OMP_EVAL_OUTPUT is required")
	}
	if err = os.WriteFile(output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
