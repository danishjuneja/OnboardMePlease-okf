package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"onboardmeplease/internal/privacy"
)

const GeneratorVersion = "source-analysis-v1"

type Claim struct {
	ID            string   `json:"claim_id"`
	Text          string   `json:"text"`
	Kind          string   `json:"kind"`
	EvidenceIDs   []string `json:"evidence_ids"`
	ProfileIDs    []string `json:"profile_ids"`
	Conditions    []string `json:"conditions"`
	SupportStatus string   `json:"support_status"`
	Assumption    string   `json:"assumption,omitempty"`
}

type Section struct {
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	ClaimIDs []string `json:"claim_ids"`
}

type Limitation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Inventory struct {
	Total        int            `json:"total"`
	Statuses     map[string]int `json:"statuses"`
	Categories   map[string]int `json:"categories"`
	Unclassified int            `json:"unclassified"`
}

type Note struct {
	ID        int64     `json:"id"`
	ClaimID   string    `json:"claim_id"`
	Text      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	Orphaned  bool      `json:"orphaned"`
}

type Synthesis struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	TotalChunks    int    `json:"total_chunks"`
	ReviewedChunks int    `json:"reviewed_chunks"`
	TotalUnits     int    `json:"total_units"`
	ReviewedUnits  int    `json:"reviewed_units"`
}

type Overview struct {
	Synthesis          *Synthesis   `json:"synthesis,omitempty"`
	Concepts           []Section    `json:"concepts,omitempty"`
	RepositoryID       string       `json:"repository_id"`
	SnapshotID         string       `json:"snapshot_id"`
	State              string       `json:"state"`
	Sections           []Section    `json:"sections"`
	Claims             []Claim      `json:"claims"`
	Limitations        []Limitation `json:"limitations"`
	CoverageSnapshotID string       `json:"coverage_snapshot_id"`
	Inventory          Inventory    `json:"inventory"`
	Notes              []Note       `json:"notes"`
	Version            int          `json:"version"`
	GeneratedAt        time.Time    `json:"generated_at"`
	CommitOID          string       `json:"commit_oid"`
	GeneratorVersion   string       `json:"generator_version"`
}

type Artifact struct {
	Path        string
	Status      string
	ContentHash string
	Reasons     []string
	ByteSize    int64
}

type Evidence struct {
	ID        string
	Path      string
	StartLine int
	EndLine   int
	Hash      string
}

type Symbol struct {
	ID            string
	Path          string
	Name          string
	QualifiedName string
	Kind          string
	StartLine     int
}

type Relation struct {
	ID         string
	FromID     string
	ToID       string
	Kind       string
	Resolution string
	Path       string
	Line       int
}

type Input struct {
	RepositoryID string
	SnapshotID   string
	CommitOID    string
	Artifacts    []Artifact
	Evidence     []Evidence
	Symbols      []Symbol
	Relations    []Relation
	Snippets     []SourceText
}

type SourceText struct {
	Path      string
	StartLine int
	Content   string
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func category(file string) string {
	name := strings.ToLower(path.Base(file))
	ext := strings.ToLower(path.Ext(name))
	switch {
	case strings.HasSuffix(name, "_test.go"), strings.Contains(name, ".test."), strings.Contains(name, ".spec."), strings.HasPrefix(strings.ToLower(file), "test/"):
		return "test"
	case name == "dockerfile", name == "compose.yaml", name == "compose.yml", name == "docker-compose.yml", name == "docker-compose.yaml", name == "package.json", name == "go.mod", name == "pyproject.toml", name == "pom.xml", name == "build.gradle", name == "cargo.toml", name == "makefile", ext == ".tf":
		return "manifest"
	case ext == ".md", ext == ".mdx", ext == ".rst", name == "readme", strings.HasPrefix(name, "readme."), name == "license", strings.HasPrefix(name, "license."), name == "contributing", strings.HasPrefix(name, "contributing."):
		return "documentation"
	case ext == ".go", ext == ".ts", ext == ".tsx", ext == ".js", ext == ".jsx", ext == ".py", ext == ".java", ext == ".rs", ext == ".cs", ext == ".rb", ext == ".php", ext == ".kt":
		return "source"
	case ext == ".sql":
		return "data"
	default:
		return "unclassified"
	}
}

func Generate(input Input, now time.Time) Overview {
	overview := Overview{RepositoryID: input.RepositoryID, SnapshotID: input.SnapshotID, State: "evidence_only",
		CoverageSnapshotID: input.SnapshotID, CommitOID: input.CommitOID, GeneratedAt: now.UTC(),
		GeneratorVersion: GeneratorVersion, Notes: []Note{}, Claims: []Claim{}, Limitations: []Limitation{},
		Inventory: Inventory{Total: len(input.Artifacts), Statuses: map[string]int{}, Categories: map[string]int{}},
		Sections: []Section{{Kind: "purpose", Title: "Purpose", ClaimIDs: []string{}},
			{Kind: "interfaces", Title: "Interfaces", ClaimIDs: []string{}},
			{Kind: "runtime", Title: "Runtime entry points", ClaimIDs: []string{}},
			{Kind: "components", Title: "Code areas", ClaimIDs: []string{}},
			{Kind: "startup", Title: "Startup", ClaimIDs: []string{}},
			{Kind: "deployment", Title: "Deployment declarations", ClaimIDs: []string{}},
			{Kind: "data", Title: "Data declarations", ClaimIDs: []string{}},
			{Kind: "flows", Title: "Resolved source relationships", ClaimIDs: []string{}},
			{Kind: "start_exploring", Title: "Where to start exploring", ClaimIDs: []string{}},
			{Kind: "coverage", Title: "Coverage and gaps", ClaimIDs: []string{}}},
	}
	artifactByPath := make(map[string]Artifact, len(input.Artifacts))
	evidenceByPath := make(map[string][]Evidence)
	for _, item := range input.Evidence {
		evidenceByPath[item.Path] = append(evidenceByPath[item.Path], item)
	}
	for file := range evidenceByPath {
		sort.Slice(evidenceByPath[file], func(i, j int) bool { return evidenceByPath[file][i].StartLine < evidenceByPath[file][j].StartLine })
	}
	citationFor := func(file string, line int) string {
		items := evidenceByPath[file]
		i := sort.Search(len(items), func(i int) bool { return items[i].EndLine >= line })
		if i < len(items) && items[i].StartLine <= line {
			return items[i].ID
		}
		return ""
	}
	for _, artifact := range input.Artifacts {
		artifactByPath[artifact.Path] = artifact
		overview.Inventory.Statuses[artifact.Status]++
		cat := category(artifact.Path)
		overview.Inventory.Categories[cat]++
		if cat == "unclassified" {
			overview.Inventory.Unclassified++
		}
	}
	if overview.Inventory.Statuses["pending"] > 0 || overview.Inventory.Statuses["failed"] > 0 {
		overview.State = "partial"
	}
	addClaim := func(section, statement, kind, support, assumption string, evidenceIDs []string) {
		if len(evidenceIDs) == 0 || len(overview.Claims) >= 80 {
			return
		}
		for _, id := range evidenceIDs {
			if id == "" {
				return
			}
		}
		if _, err := privacy.Clear("claim", []byte(statement), nil); err != nil {
			return
		}
		id := stableID(section, statement, strings.Join(evidenceIDs, ","))
		for _, existing := range overview.Claims {
			if existing.ID == id {
				return
			}
		}
		overview.Claims = append(overview.Claims, Claim{ID: id, Text: statement, Kind: kind, EvidenceIDs: evidenceIDs,
			ProfileIDs: []string{}, Conditions: []string{}, SupportStatus: support, Assumption: assumption})
		for i := range overview.Sections {
			if overview.Sections[i].Kind == section {
				overview.Sections[i].ClaimIDs = append(overview.Sections[i].ClaimIDs, id)
				break
			}
		}
	}
	add := func(section, statement string, evidenceID string) {
		addClaim(section, statement, "fact", "supported", "", []string{evidenceID})
	}
	// File-presence claims identify source material; they do not assert that a service runs.
	files := append([]Artifact(nil), input.Artifacts...)
	sort.Slice(files, func(i, j int) bool {
		left, right := strings.Count(files[i].Path, "/"), strings.Count(files[j].Path, "/")
		if left != right {
			return left < right
		}
		return files[i].Path < files[j].Path
	})
	seenManifest := 0
	seenData := 0
	seenSource := 0
	for _, artifact := range files {
		if artifact.Status != "analyzed" {
			continue
		}
		citation := citationFor(artifact.Path, 1)
		if citation == "" {
			continue
		}
		switch category(artifact.Path) {
		case "manifest":
			if seenManifest < 18 {
				name := strings.ToLower(path.Base(artifact.Path))
				switch name {
				case "dockerfile", "compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml":
					add("deployment", fmt.Sprintf("%s is a deployment-related declaration in the captured source; no deployment was observed.", artifact.Path), citation)
				default:
					add("startup", fmt.Sprintf("%s is a project or build manifest in the captured source.", artifact.Path), citation)
				}
				seenManifest++
			}
		case "data":
			if seenData < 10 {
				add("data", fmt.Sprintf("%s is a SQL source file; its execution and target database are not established by file presence.", artifact.Path), citation)
				seenData++
			}
		case "source":
			if seenSource < 8 {
				add("start_exploring", fmt.Sprintf("%s is an indexed source file that can be opened for implementation detail.", artifact.Path), citation)
				seenSource++
			}
		}
	}
	// Interpret only explicit declarations in small, already approved source files.
	// Documentation remains attributed as a statement made by the README.
	for _, snippet := range input.Snippets {
		lines := strings.Split(snippet.Content, "\n")
		name := strings.ToLower(path.Base(snippet.Path))
		for offset, raw := range lines {
			line := strings.TrimSpace(raw)
			lineNo := snippet.StartLine + offset
			if len(line) == 0 || len(line) > 220 {
				continue
			}
			citation := citationFor(snippet.Path, lineNo)
			if citation == "" {
				continue
			}
			if name == "dockerfile" {
				upper := strings.ToUpper(line)
				switch {
				case strings.HasPrefix(upper, "FROM "):
					add("deployment", fmt.Sprintf("%s declares %q as a Docker build instruction; this does not establish that the image was built.", snippet.Path, line), citation)
				case strings.HasPrefix(upper, "ENTRYPOINT "), strings.HasPrefix(upper, "CMD "):
					add("startup", fmt.Sprintf("%s declares the container startup instruction %q; execution was not observed.", snippet.Path, line), citation)
				case strings.HasPrefix(upper, "EXPOSE "):
					add("interfaces", fmt.Sprintf("%s contains %q; this documents an image port, not a verified reachable service.", snippet.Path, line), citation)
				}
			}
			if name == "go.mod" && strings.HasPrefix(line, "module ") {
				add("components", fmt.Sprintf("%s declares Go module %q.", snippet.Path, strings.TrimSpace(strings.TrimPrefix(line, "module "))), citation)
			}
		}
		if name == "package.json" {
			var manifest struct {
				Description string            `json:"description"`
				Scripts     map[string]string `json:"scripts"`
			}
			if err := json.Unmarshal([]byte(snippet.Content), &manifest); err == nil {
				for _, script := range []string{"start", "dev", "serve", "build", "test"} {
					command, ok := manifest.Scripts[script]
					if !ok || len(command) > 160 {
						continue
					}
					for offset, raw := range lines {
						trimmed := strings.TrimSpace(raw)
						if strings.HasPrefix(trimmed, strconv.Quote(script)) && strings.Contains(trimmed, ":") {
							add("startup", fmt.Sprintf("%s declares npm script %q as %q; whether it runs in a deployment is unresolved.", snippet.Path, script, command), citationFor(snippet.Path, snippet.StartLine+offset))
							break
						}
					}
				}
			}
		}
	}
	symbols := make(map[string]Symbol, len(input.Symbols))
	mainCount := 0
	packages := make(map[string]bool)
	for _, symbol := range input.Symbols {
		symbols[symbol.ID] = symbol
		if strings.HasSuffix(strings.ToLower(symbol.Path), ".go") && len(packages) < 16 {
			if dot := strings.LastIndex(symbol.QualifiedName, "."); dot > 0 {
				packageName := symbol.QualifiedName[:dot]
				if !packages[packageName] {
					citation := citationFor(symbol.Path, symbol.StartLine)
					if citation != "" {
						add("components", fmt.Sprintf("Go package %s has a parsed %s declaration %s in %s; this is a code area, not a separately deployed component.", packageName, symbol.Kind, symbol.Name, symbol.Path), citation)
						packages[packageName] = true
					}
				}
			}
		}
		if symbol.Name == "main" && symbol.Kind == "function" && strings.HasSuffix(strings.ToLower(symbol.Path), ".go") && mainCount < 15 {
			if _, ok := artifactByPath[symbol.Path]; !ok {
				continue
			}
			add("runtime", fmt.Sprintf("%s declares a Go main function; whether it is selected by a launch profile remains unresolved.", symbol.Path), citationFor(symbol.Path, symbol.StartLine))
			mainCount++
		}
	}
	orderedRelations := append([]Relation(nil), input.Relations...)
	sort.SliceStable(orderedRelations, func(i, j int) bool {
		fromI, fromJ := symbols[orderedRelations[i].FromID], symbols[orderedRelations[j].FromID]
		return fromI.Name == "main" && fromJ.Name != "main"
	})
	flowCount := 0
	for _, relation := range orderedRelations {
		if flowCount >= 12 {
			break
		}
		if relation.Resolution != "resolved" || (relation.Kind != "calls" && relation.Kind != "references") {
			continue
		}
		from, fromOK := symbols[relation.FromID]
		to, toOK := symbols[relation.ToID]
		if !fromOK || !toOK || from.Name == "" || to.Name == "" {
			continue
		}
		citation := citationFor(relation.Path, relation.Line)
		if citation == "" {
			continue
		}
		add("flows", fmt.Sprintf("%s at %s:%d has a resolved %s link to %s at %s; this is a static source relationship, not an observed execution.",
			from.Name, relation.Path, relation.Line, relation.Kind, to.Name, to.Path), citation)
		flowCount++
	}
	sectionCount := func(kind string) int {
		for _, section := range overview.Sections {
			if section.Kind == kind {
				return len(section.ClaimIDs)
			}
		}
		return 0
	}
	if sectionCount("purpose") == 0 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "purpose_unresolved", Message: "Source synthesis has not run. Configure a generation provider and analyze this snapshot to derive its purpose from implementation."})
	} else {
		hasExplicitPurpose := false
		for _, claim := range overview.Claims {
			if claim.Kind != "fact" {
				continue
			}
			for _, id := range overview.Sections[0].ClaimIDs {
				if claim.ID == id {
					hasExplicitPurpose = true
				}
			}
		}
		if !hasExplicitPurpose {
			overview.Limitations = append(overview.Limitations, Limitation{Code: "specific_purpose_unresolved", Message: "Source files establish technical scope, but do not establish the repository's specific product or business purpose."})
		}
	}
	if sectionCount("runtime") == 0 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "entry_point_unresolved", Message: "No supported runtime entry point was identified; launch behavior remains unresolved."})
	}
	if sectionCount("deployment") == 0 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "deployment_unresolved", Message: "No supported deployment declaration was identified; this repository may be a library or may use an undiscovered profile."})
	}
	if sectionCount("flows") == 0 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "flow_unresolved", Message: "The current extractors did not establish a source relationship suitable for this overview."})
	}
	if overview.Inventory.Statuses["pending"]+overview.Inventory.Statuses["failed"]+overview.Inventory.Statuses["unsupported"]+overview.Inventory.Statuses["excluded"] > 0 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "partial_analysis", Message: "Some inventoried artifacts were not analyzed. Inspect coverage before relying on this overview."})
	}
	if len(overview.Claims) >= 80 {
		overview.Limitations = append(overview.Limitations, Limitation{Code: "overview_claim_limit", Message: "The overview reached its 80-claim display limit. Search and the full inventory remain available for the rest of the snapshot."})
	}
	overview.Limitations = append(overview.Limitations, Limitation{Code: "configuration_unresolved", Message: "Required environment values and the selected launch profile have not been established; startup conditions remain unresolved."})
	overview.Limitations = append(overview.Limitations, Limitation{Code: "static_only", Message: "This overview reports selected static evidence. It does not prove deployment, UI behavior, runtime conditions, or that no other connections exist."})
	return overview
}

func load(ctx context.Context, pool *pgxpool.Pool, repositoryID, snapshotID string) (Input, error) {
	input := Input{RepositoryID: repositoryID, SnapshotID: snapshotID}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(s.commit_oid,'') FROM snapshots s JOIN repositories r ON r.id=s.repository_id
		WHERE s.id=$1 AND r.id=$2 AND s.state='ready' AND r.source_kind='github'`, snapshotID, repositoryID).Scan(&input.CommitOID); err != nil {
		return input, errors.New("ready GitHub snapshot unavailable")
	}
	rows, err := pool.Query(ctx, `SELECT path,status,COALESCE(content_hash,''),reason_codes,byte_size FROM artifacts WHERE snapshot_id=$1 ORDER BY path`, snapshotID)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.Path, &a.Status, &a.ContentHash, &a.Reasons, &a.ByteSize); err != nil {
			rows.Close()
			return input, err
		}
		input.Artifacts = append(input.Artifacts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return input, err
	}
	var snippetPaths []string
	for _, artifact := range input.Artifacts {
		if artifact.Status != "analyzed" || artifact.ByteSize > 65536 || len(snippetPaths) >= 200 {
			continue
		}
		name := strings.ToLower(path.Base(artifact.Path))
		if name == "dockerfile" || name == "package.json" || name == "go.mod" {
			snippetPaths = append(snippetPaths, artifact.Path)
		}
	}
	if len(snippetPaths) > 0 {
		rows, err = pool.Query(ctx, `SELECT path,start_line,content FROM evidence_chunks WHERE snapshot_id=$1 AND path=ANY($2::text[]) ORDER BY path,start_line`, snapshotID, snippetPaths)
		if err != nil {
			return input, err
		}
		contents := make(map[string]*strings.Builder)
		var order []string
		for rows.Next() {
			var snippet SourceText
			if err := rows.Scan(&snippet.Path, &snippet.StartLine, &snippet.Content); err != nil {
				rows.Close()
				return input, err
			}
			builder, ok := contents[snippet.Path]
			if !ok {
				builder = &strings.Builder{}
				contents[snippet.Path] = builder
				order = append(order, snippet.Path)
			}
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(snippet.Content)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return input, err
		}
		for _, name := range order {
			input.Snippets = append(input.Snippets, SourceText{Path: name, StartLine: 1, Content: contents[name].String()})
		}
	}
	rows, err = pool.Query(ctx, `SELECT id,path,start_line,end_line,content_hash FROM evidence_chunks WHERE snapshot_id=$1 ORDER BY path,start_line`, snapshotID)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var e Evidence
		if err := rows.Scan(&e.ID, &e.Path, &e.StartLine, &e.EndLine, &e.Hash); err != nil {
			rows.Close()
			return input, err
		}
		input.Evidence = append(input.Evidence, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return input, err
	}
	rows, err = pool.Query(ctx, `SELECT id,path,name,qualified_name,kind,start_line FROM symbols WHERE snapshot_id=$1 ORDER BY path,start_line,id`, snapshotID)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.ID, &s.Path, &s.Name, &s.QualifiedName, &s.Kind, &s.StartLine); err != nil {
			rows.Close()
			return input, err
		}
		input.Symbols = append(input.Symbols, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return input, err
	}
	rows, err = pool.Query(ctx, `SELECT id,from_id,to_id,kind,resolution,path,line FROM relations WHERE snapshot_id=$1 ORDER BY path,line,id`, snapshotID)
	if err != nil {
		return input, err
	}
	for rows.Next() {
		var relation Relation
		if err := rows.Scan(&relation.ID, &relation.FromID, &relation.ToID, &relation.Kind, &relation.Resolution, &relation.Path, &relation.Line); err != nil {
			rows.Close()
			return input, err
		}
		input.Relations = append(input.Relations, relation)
	}
	err = rows.Err()
	rows.Close()
	return input, err
}

func fingerprint(input Input) string {
	h := sha256.New()
	fmt.Fprint(h, GeneratorVersion, "\x00", input.CommitOID, "\x00")
	for _, a := range input.Artifacts {
		fmt.Fprint(h, a.Path, "\x00", a.Status, "\x00", a.ContentHash, "\x00", strings.Join(a.Reasons, ","), "\n")
	}
	for _, e := range input.Evidence {
		fmt.Fprint(h, e.ID, "\x00", e.Hash, "\n")
	}
	for _, s := range input.Symbols {
		fmt.Fprint(h, s.ID, "\x00", s.QualifiedName, "\n")
	}
	for _, r := range input.Relations {
		fmt.Fprint(h, r.ID, "\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}

func Build(ctx context.Context, pool *pgxpool.Pool, repositoryID, snapshotID string) (Overview, error) {
	lock, err := pool.Acquire(ctx)
	if err != nil {
		return Overview{}, err
	}
	defer lock.Release()
	var acquired bool
	if err := lock.QueryRow(ctx, `SELECT pg_try_advisory_lock(151,hashtext($1))`, snapshotID).Scan(&acquired); err != nil {
		return Overview{}, err
	}
	if !acquired {
		return Get(ctx, pool, repositoryID, snapshotID)
	}
	defer lock.Exec(context.Background(), `SELECT pg_advisory_unlock(151,hashtext($1))`, snapshotID)
	input, err := load(ctx, pool, repositoryID, snapshotID)
	if err != nil {
		return Overview{}, err
	}
	fp := fingerprint(input)
	var latestVersion int
	var latestFingerprint string
	err = pool.QueryRow(ctx, `SELECT version,fingerprint FROM overview_versions WHERE snapshot_id=$1 ORDER BY version DESC LIMIT 1`, snapshotID).Scan(&latestVersion, &latestFingerprint)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Overview{}, err
	}
	if latestFingerprint == fp {
		return Get(ctx, pool, repositoryID, snapshotID)
	}
	return publish(ctx, pool, input, Generate(input, time.Now()), fp, latestVersion+1)
}

func publish(ctx context.Context, pool *pgxpool.Pool, input Input, overview Overview, fp string, version int) (Overview, error) {
	repositoryID, snapshotID := input.RepositoryID, input.SnapshotID
	overview.Version = version
	if err := Validate(input, overview); err != nil {
		return Overview{}, err
	}
	encoded, err := json.Marshal(overview)
	if err != nil {
		return Overview{}, err
	}
	if _, err := privacy.Clear("overview", encoded, nil); err != nil {
		return Overview{}, errors.New("overview failed privacy policy")
	}
	transaction, err := pool.Begin(ctx)
	if err != nil {
		return Overview{}, err
	}
	defer transaction.Rollback(context.Background())
	if _, err = transaction.Exec(ctx, `DELETE FROM overview_artifacts WHERE snapshot_id=$1`, snapshotID); err != nil {
		return Overview{}, err
	}
	copyRows := make([][]any, 0, len(input.Artifacts))
	for _, a := range input.Artifacts {
		copyRows = append(copyRows, []any{snapshotID, a.Path, category(a.Path)})
	}
	if len(copyRows) > 0 {
		if _, err = transaction.CopyFrom(ctx, pgx.Identifier{"overview_artifacts"}, []string{"snapshot_id", "path", "category"}, pgx.CopyFromRows(copyRows)); err != nil {
			return Overview{}, err
		}
	}
	if _, err = transaction.Exec(ctx, `INSERT INTO overview_versions(snapshot_id,version,fingerprint,document) VALUES ($1,$2,$3,$4)`, snapshotID, overview.Version, fp, encoded); err != nil {
		return Overview{}, err
	}
	var remote string
	if err = transaction.QueryRow(ctx, `SELECT source_locator FROM repositories WHERE id=$1`, repositoryID).Scan(&remote); err != nil {
		return Overview{}, err
	}
	if err = storeConcepts(ctx, transaction, overview, input, remote); err != nil {
		return Overview{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Overview{}, err
	}
	return Get(ctx, pool, repositoryID, snapshotID)
}

func Get(ctx context.Context, pool *pgxpool.Pool, repositoryID, snapshotID string) (Overview, error) {
	var overview Overview
	var encoded []byte
	err := pool.QueryRow(ctx, `SELECT o.document FROM overview_versions o JOIN snapshots s ON s.id=o.snapshot_id
		JOIN repositories r ON r.id=s.repository_id WHERE o.snapshot_id=$1 AND r.id=$2 AND s.state='ready' AND r.source_kind='github'
		ORDER BY o.version DESC LIMIT 1`, snapshotID, repositoryID).Scan(&encoded)
	if err != nil {
		return overview, err
	}
	if err := json.Unmarshal(encoded, &overview); err != nil {
		return overview, err
	}
	rows, err := pool.Query(ctx, `SELECT id,claim_id,note,created_at FROM overview_notes WHERE snapshot_id=$1 ORDER BY created_at,id`, snapshotID)
	if err != nil {
		return overview, err
	}
	claimIDs := make(map[string]bool, len(overview.Claims))
	for _, claim := range overview.Claims {
		claimIDs[claim.ID] = true
	}
	overview.Notes = []Note{}
	for rows.Next() {
		var note Note
		if err := rows.Scan(&note.ID, &note.ClaimID, &note.Text, &note.CreatedAt); err != nil {
			rows.Close()
			return overview, err
		}
		note.Orphaned = !claimIDs[note.ClaimID]
		overview.Notes = append(overview.Notes, note)
	}
	err = rows.Err()
	rows.Close()
	return overview, err
}

func Validate(input Input, overview Overview) error {
	if overview.RepositoryID != input.RepositoryID || overview.SnapshotID != input.SnapshotID || overview.CoverageSnapshotID != input.SnapshotID || overview.Inventory.Total != len(input.Artifacts) {
		return errors.New("overview scope or inventory mismatch")
	}
	statusTotal := 0
	for _, n := range overview.Inventory.Statuses {
		statusTotal += n
	}
	categoryTotal := 0
	for _, n := range overview.Inventory.Categories {
		categoryTotal += n
	}
	if statusTotal != len(input.Artifacts) || categoryTotal != len(input.Artifacts) {
		return errors.New("overview coverage mismatch")
	}
	evidence := make(map[string]Evidence, len(input.Evidence))
	for _, item := range input.Evidence {
		evidence[item.ID] = item
	}
	claims := make(map[string]bool, len(overview.Claims))
	for _, claim := range overview.Claims {
		if claim.ID == "" || claims[claim.ID] || len(claim.EvidenceIDs) == 0 {
			return errors.New("invalid overview claim")
		}
		if claim.Kind == "fact" && claim.SupportStatus != "supported" ||
			claim.Kind == "inference" && (claim.SupportStatus != "partial" || claim.Assumption == "") ||
			claim.Kind != "fact" && claim.Kind != "inference" {
			return errors.New("invalid overview claim")
		}
		claims[claim.ID] = true
		if _, err := privacy.Clear("claim", []byte(claim.Text), nil); err != nil {
			return errors.New("overview claim failed privacy policy")
		}
		evidencePaths := map[string]bool{}
		for _, id := range claim.EvidenceIDs {
			item, ok := evidence[id]
			if !ok {
				return errors.New("overview citation missing from snapshot")
			}
			evidencePaths[item.Path] = true
		}

	}
	for _, section := range overview.Sections {
		for _, id := range section.ClaimIDs {
			if !claims[id] {
				return errors.New("section claim missing")
			}
		}
	}
	return nil
}
