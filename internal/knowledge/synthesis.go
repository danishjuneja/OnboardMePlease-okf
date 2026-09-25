package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/grounding"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
)

const UnitBytes = 24000
const UnitsPerRun = 8

// Partition uses complete chunks and covers every eligible chunk. Oversize
// chunks become explicit uncovered gaps; they are never silently truncated.
func Partition(sources []grounding.Source) ([][]grounding.Source, int) {
	units := [][]grounding.Source{}
	batch := []grounding.Source{}
	size, oversize := 0, 0
	for _, s := range sources {
		if len(s.Content) > UnitBytes {
			oversize++
			continue
		}
		if size+len(s.Content) > UnitBytes || len(batch) >= 12 {
			units = append(units, batch)
			batch = []grounding.Source{}
			size = 0
		}
		batch = append(batch, s)
		size += len(s.Content)
	}
	if len(batch) > 0 {
		units = append(units, batch)
	}
	return units, oversize
}

func ReadSources(ctx context.Context, pool *pgxpool.Pool, snapshot string, ids []string) ([]grounding.Source, error) {
	rows, err := pool.Query(ctx, `SELECT e.id,e.path,e.start_line,e.end_line,e.content,e.source_kind FROM evidence_chunks e JOIN artifacts a ON a.snapshot_id=e.snapshot_id AND a.path=e.path WHERE e.snapshot_id=$1 AND a.status='analyzed' AND ($2::text[] IS NULL OR e.id=ANY($2)) ORDER BY e.path,e.start_line`, snapshot, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []grounding.Source{}
	for rows.Next() {
		var s grounding.Source
		if err = rows.Scan(&s.ID, &s.Path, &s.Start, &s.End, &s.Content, &s.Kind); err != nil {
			return nil, err
		}
		if _, err = privacy.Clear(s.Path, []byte(s.Content), nil); err != nil {
			return nil, errors.New("stored source failed privacy scan")
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func Policy(ctx context.Context, pool *pgxpool.Pool, c config.Config, repo, snapshot string) (privacy.ModelRequest, error) {
	var mode string
	err := pool.QueryRow(ctx, `SELECT r.privacy_mode FROM repositories r JOIN snapshots s ON s.repository_id=r.id WHERE r.id=$1 AND s.id=$2 AND s.state='ready' AND r.source_kind='github'`, repo, snapshot).Scan(&mode)
	return privacy.ModelRequest{Mode: c.ModelMode, RepoOptedIn: mode == "cloud_opt_in"}, err
}

func Synthesize(ctx context.Context, pool *pgxpool.Pool, c config.Config, repo, snapshot string) (Overview, error) {
	adapter, err := providers.ConfiguredGeneration(c)
	if err != nil {
		return Overview{}, err
	}
	policy, err := Policy(ctx, pool, c, repo, snapshot)
	if err != nil {
		return Overview{}, err
	}
	policy.ProviderKind = adapter.Kind
	policy.ProviderURL = adapter.URL
	policy.ScanSucceeded = true
	policy.Sanitized = true
	if err = privacy.Authorize(policy); err != nil {
		return Overview{}, err
	}
	return SynthesizeWith(ctx, pool, adapter, policy, adapter.Kind, adapter.Model, repo, snapshot)
}

// SynthesizeWith allows the same pipeline to be evaluated with a deterministic
// test provider. Production always supplies the privacy-enforcing adapter.
func SynthesizeWith(ctx context.Context, pool *pgxpool.Pool, model providers.Generator, policy privacy.ModelRequest, provider, modelName, repo, snapshot string) (Overview, error) {
	lock, err := pool.Acquire(ctx)
	if err != nil {
		return Overview{}, err
	}
	defer lock.Release()
	var acquired bool
	if err = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(151,hashtext($1))`, snapshot).Scan(&acquired); err != nil {
		return Overview{}, err
	}
	if !acquired {
		return Overview{}, errors.New("analysis already running for snapshot")
	}
	defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(151,hashtext($1))`, snapshot)
	input, err := load(ctx, pool, repo, snapshot)
	if err != nil {
		return Overview{}, err
	}
	units, sizes, oversize, err := sourceUnits(ctx, pool, snapshot)
	if err != nil {
		return Overview{}, err
	}
	fp := fingerprint(input)
	overview := Generate(input, time.Now())
	overview.Claims = []Claim{}
	for i := range overview.Sections {
		overview.Sections[i].ClaimIDs = []string{}
		if title, ok := map[string]string{"runtime": "Runtime behavior", "components": "Components", "deployment": "Deployment", "data": "Data and state", "flows": "Technical flows"}[overview.Sections[i].Kind]; ok {
			overview.Sections[i].Title = title
		}
	}
	overview.Limitations = []Limitation{{Code: "static_analysis", Message: "Claims were assessed against captured source by a model. No deployment or runtime execution was observed."}}
	overview.Synthesis = &Synthesis{Provider: provider, Model: modelName, TotalChunks: len(input.Evidence), TotalUnits: len(units)}
	accepted := []grounding.Claim{}
	summaryClaims := map[string]bool{}
	newUnits := 0
	for _, unit := range units {
		encoded, _ := json.Marshal(unit)
		key := stableID(fp, provider, modelName, grounding.Fingerprint(), string(encoded))
		var cached []byte
		err = pool.QueryRow(ctx, `SELECT result FROM analysis_units WHERE snapshot_id=$1 AND cache_key=$2`, snapshot, key).Scan(&cached)
		var reviewed grounding.Assessed
		if errors.Is(err, pgx.ErrNoRows) {
			if newUnits >= UnitsPerRun {
				continue
			}
			unitSources, e := ReadSources(ctx, pool, snapshot, unit)
			if e != nil {
				return Overview{}, e
			}
			var draft grounding.Result
			err = grounding.Run(ctx, model, policy, "P01", "synthesis-result", map[string]any{"source_evidence": unitSources}, &draft)
			if err != nil {
				return Overview{}, err
			}
			reviewed, err = grounding.Assess(ctx, model, policy, draft, unitSources)
			if err != nil {
				return Overview{}, err
			}
			cached, err = json.Marshal(reviewed)
			if err != nil {
				return Overview{}, err
			}
			ids := unit
			if _, err = pool.Exec(ctx, `INSERT INTO analysis_units(snapshot_id,cache_key,evidence_ids,result) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, snapshot, key, ids, cached); err != nil {
				return Overview{}, err
			}
			newUnits++
		} else if err != nil {
			return Overview{}, err
		} else if err = json.Unmarshal(cached, &reviewed); err != nil {
			return Overview{}, err
		}
		overview.Synthesis.ReviewedUnits++
		overview.Synthesis.ReviewedChunks += len(unit)
		for i := range reviewed.Claims {
			if reviewed.Claims[i].Section == "purpose" {
				reviewed.Claims[i].Section = "components"
			}
		}
		accepted = append(accepted, reviewed.Claims...)
		// Gaps are findings to investigate, not established negative claims.
		for _, g := range reviewed.Gaps {
			if len(overview.Limitations) < 30 {
				overview.Limitations = append(overview.Limitations, Limitation{Code: "analysis_gap", Message: "Within source batch " + key[:8] + ": " + g})
			}
		}
	}
	if len(accepted) > 0 {
		// A bounded rollup can read only part of a very large analysis. The source
		// coverage ledger remains independent of rollup/display coverage.
		selected := []grounding.Claim{}
		rollupIDs := []string{}
		used := map[string]bool{}
		size := 0
		for _, claim := range accepted {
			extra := len(claim.Text)
			for _, id := range claim.EvidenceIDs {
				if !used[id] {
					extra += sizes[id]
				}
			}
			if size+extra > 65000 || len(selected) >= 48 {
				continue
			}
			selected = append(selected, claim)
			size += extra
			for _, id := range claim.EvidenceIDs {
				if !used[id] {
					used[id] = true
					rollupIDs = append(rollupIDs, id)
				}
			}
		}
		rollupSources, e := ReadSources(ctx, pool, snapshot, rollupIDs)
		if e != nil {
			return Overview{}, e
		}
		var draft grounding.Result
		err = grounding.Run(ctx, model, policy, "P04", "synthesis-result", map[string]any{"derived_claims": selected, "source_evidence": rollupSources, "coverage": overview.Synthesis}, &draft)
		if err != nil {
			return Overview{}, err
		}
		reviewed, e := grounding.Assess(ctx, model, policy, draft, rollupSources, grounding.Coverage{TotalChunks: overview.Synthesis.TotalChunks, ReviewedChunks: overview.Synthesis.ReviewedChunks, TotalUnits: overview.Synthesis.TotalUnits, ReviewedUnits: overview.Synthesis.ReviewedUnits})
		if e != nil {
			return Overview{}, e
		}
		for _, gap := range reviewed.Gaps {
			overview.Limitations = append(overview.Limitations, Limitation{Code: "summary_gap", Message: gap})
		}
		for _, claim := range reviewed.Claims {
			summaryClaims[stableID(claim.Text, strings.Join(claim.EvidenceIDs, ","))] = true
		}
		accepted = append(reviewed.Claims, accepted...)
		if len(selected) < len(accepted)-len(reviewed.Claims) {
			overview.Limitations = append(overview.Limitations, Limitation{Code: "rollup_budget", Message: "The repository-wide summary used a bounded subset of reviewed findings. Other reviewed capabilities remain in the knowledge concepts."})
		}
	}
	concepts := map[string]int{}
	seen := map[string]bool{}
	for _, a := range accepted {
		if len(overview.Claims) >= 600 {
			overview.Limitations = append(overview.Limitations, Limitation{Code: "publication_budget", Message: "The published overview reached 600 claims; additional reviewed units remain cached."})
			break
		}
		id := stableID(a.Text, strings.Join(a.EvidenceIDs, ","))
		if seen[id] {
			continue
		}
		seen[id] = true
		claim := Claim{ID: id, Text: a.Text, Kind: "fact", EvidenceIDs: a.EvidenceIDs, ProfileIDs: []string{}, Conditions: []string{}, SupportStatus: "supported", Assumption: a.Assumption}
		if a.Assumption != "" {
			claim.Kind = "inference"
			claim.SupportStatus = "partial"
		}
		overview.Claims = append(overview.Claims, claim)
		section := a.Section
		found := false
		for _, s := range overview.Sections {
			if s.Kind == section {
				found = true
			}
		}
		if !found {
			section = "components"
		}
		for i := range overview.Sections {
			if overview.Sections[i].Kind == section && summaryClaims[id] {
				overview.Sections[i].ClaimIDs = append(overview.Sections[i].ClaimIDs, id)
			}
		}
		title := strings.TrimSpace(a.Concept)
		if title == "" {
			title = "Implementation details"
		}
		key := stableID(strings.ToLower(title))
		i, ok := concepts[key]
		if !ok {
			i = len(overview.Concepts)
			concepts[key] = i
			overview.Concepts = append(overview.Concepts, Section{Kind: key, Title: title, ClaimIDs: []string{}})
		}
		overview.Concepts[i].ClaimIDs = append(overview.Concepts[i].ClaimIDs, id)
	}
	overview.State = "ready"
	if len(overview.Sections[0].ClaimIDs) == 0 {
		overview.State = "partial"
		overview.Limitations = append(overview.Limitations, Limitation{Code: "purpose_unresolved", Message: "The reviewed source did not yield an accepted repository-purpose statement; inspect supported components and sources."})
	}
	if len(accepted) > 600 {
		overview.State = "partial"
	}
	if overview.Synthesis.ReviewedChunks < len(input.Evidence) || oversize > 0 || len(overview.Claims) == 0 {
		overview.State = "partial"
		overview.Limitations = append(overview.Limitations, Limitation{Code: "unexamined_source", Message: fmt.Sprintf("Reviewed %d of %d indexed chunks; %d oversized chunks need smaller extraction. Resume analysis for remaining work units.", overview.Synthesis.ReviewedChunks, len(input.Evidence), oversize)})
	}
	if overview.Inventory.Total > overview.Inventory.Statuses["analyzed"] {
		overview.State = "partial"
		overview.Limitations = append(overview.Limitations, Limitation{Code: "inventory_gaps", Message: "Excluded, unsupported, pending or failed artifacts were not semantically analyzed; see inventory."})
	}
	// Reindexing can change evidence while the model runs. Never publish old IDs.
	current, err := load(ctx, pool, repo, snapshot)
	if err != nil {
		return Overview{}, err
	}
	if fingerprint(current) != fp {
		return Overview{}, errors.New("source index changed during analysis; retry")
	}
	var version int
	if err = pool.QueryRow(ctx, `SELECT COALESCE(max(version),0)+1 FROM overview_versions WHERE snapshot_id=$1`, snapshot).Scan(&version); err != nil {
		return Overview{}, err
	}
	return publish(ctx, pool, input, overview, fp, version)
}

func storeConcepts(ctx context.Context, tx pgx.Tx, overview Overview, input Input, remote string) error {
	if len(overview.Concepts) == 0 {
		return nil
	}
	bundle, err := RenderOKF(overview, input, remote)
	if err != nil {
		return err
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return err
	}
	docs := map[string]string{}
	for _, file := range archive.File {
		r, e := file.Open()
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(r, 2<<20))
		r.Close()
		if e != nil {
			return e
		}
		docs[file.Name] = string(b)
	}
	claims := map[string]Claim{}
	for _, c := range overview.Claims {
		claims[c.ID] = c
	}
	for _, c := range overview.Concepts {
		ids := []string{}
		seen := map[string]bool{}
		for _, id := range c.ClaimIDs {
			for _, e := range claims[id].EvidenceIDs {
				if !seen[e] {
					seen[e] = true
					ids = append(ids, e)
				}
			}
		}
		doc := docs["concepts/"+c.Kind+".md"]
		if doc == "" {
			return errors.New("concept missing from rendered OKF")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO knowledge_concepts(snapshot_id,version,id,title,markdown,evidence_ids,content_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, overview.SnapshotID, overview.Version, c.Kind, c.Title, doc, ids, stableID(doc)); err != nil {
			return err
		}
	}
	return nil
}

func sourceUnits(ctx context.Context, pool *pgxpool.Pool, snapshot string) ([][]string, map[string]int, int, error) {
	rows, err := pool.Query(ctx, `SELECT e.id,octet_length(e.content) FROM evidence_chunks e JOIN artifacts a ON a.snapshot_id=e.snapshot_id AND a.path=e.path WHERE e.snapshot_id=$1 AND a.status='analyzed' ORDER BY (e.source_kind='code') DESC,e.path,e.start_line`, snapshot)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	units := [][]string{}
	batch := []string{}
	sizes := map[string]int{}
	total, oversize := 0, 0
	for rows.Next() {
		var id string
		var size int
		if err = rows.Scan(&id, &size); err != nil {
			return nil, nil, 0, err
		}
		sizes[id] = size
		if size > UnitBytes {
			oversize++
			continue
		}
		if total+size > UnitBytes || len(batch) >= 12 {
			units = append(units, batch)
			batch = []string{}
			total = 0
		}
		batch = append(batch, id)
		total += size
	}
	if len(batch) > 0 {
		units = append(units, batch)
	}
	return units, sizes, oversize, rows.Err()
}
