package knowledge

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func fixture() Input {
	return Input{RepositoryID: "repo-a", SnapshotID: "snapshot-a", CommitOID: strings.Repeat("a", 40),
		Artifacts: []Artifact{{Path: "README.md", Status: "analyzed"}, {Path: "lib/util.py", Status: "analyzed"},
			{Path: "misc/sparse.xyz", Status: "unsupported", Reasons: []string{"unsupported_media"}}},
		Evidence: []Evidence{{ID: "readme-evidence", Path: "README.md", StartLine: 1, EndLine: 3},
			{ID: "util-evidence", Path: "lib/util.py", StartLine: 1, EndLine: 4}},
		Snippets: []SourceText{{Path: "README.md", StartLine: 1, Content: "# Library\nThis repository provides reusable helpers for a small application.\n## Setup"}}}
}

func TestLibraryOverviewDoesNotInventDeployment(t *testing.T) {
	input := fixture()
	overview := Generate(input, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	if err := Validate(input, overview); err != nil {
		t.Fatal(err)
	}
	if overview.Inventory.Total != 3 || overview.Inventory.Statuses["unsupported"] != 1 || overview.Inventory.Unclassified != 1 {
		t.Fatalf("inventory incomplete: %+v", overview.Inventory)
	}
	for _, section := range overview.Sections {
		if section.Kind == "deployment" && len(section.ClaimIDs) != 0 {
			t.Fatal("invented deployment for library fixture")
		}
	}
	found := false
	for _, limitation := range overview.Limitations {
		if limitation.Code == "deployment_unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing deployment limitation")
	}
}

func TestClaimMustResolveToSnapshotEvidence(t *testing.T) {
	input := fixture()
	overview := Generate(input, time.Now())
	if len(overview.Claims) == 0 {
		t.Fatal("fixture produced no claims")
	}
	overview.Claims[0].EvidenceIDs = []string{"invented-evidence"}
	if err := Validate(input, overview); err == nil {
		t.Fatal("accepted invented source ID")
	}
}

func TestPendingInventoryMarksOverviewPartial(t *testing.T) {
	input := fixture()
	input.Artifacts = append(input.Artifacts, Artifact{Path: "src/unindexed.py", Status: "pending"})
	overview := Generate(input, time.Now())
	if overview.State != "partial" {
		t.Fatalf("expected partial overview, got %s", overview.State)
	}
	if err := Validate(input, overview); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentationDoesNotBecomePurposeWithoutSynthesis(t *testing.T) {
	input := fixture()
	overview := Generate(input, time.Now())
	if len(overview.Sections[0].ClaimIDs) != 0 {
		t.Fatal("documentation became synthesized purpose")
	}
	if overview.Synthesis != nil {
		t.Fatal("baseline reported semantic coverage")
	}
}

func TestPurposeUnresolvedForHeadingsOnly(t *testing.T) {
	input := fixture()
	input.Snippets = []SourceText{{Path: "README.md", StartLine: 1, Content: "# Install dependencies\nRun npm install.\n## Usage"}}
	overview := Generate(input, time.Now())
	if len(overview.Sections[0].ClaimIDs) != 0 {
		t.Fatalf("headings became purpose claims: %+v", overview.Claims)
	}
	found := false
	for _, limitation := range overview.Limitations {
		if limitation.Code == "purpose_unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing purpose limitation")
	}
}

func TestManifestAndSourceNamesDoNotBecomePurpose(t *testing.T) {
	input := fixture()
	input.Snippets = append(input.Snippets, SourceText{Path: "package.json", Content: `{"description":"A toolkit for tracing repository behavior."}`}, SourceText{Path: "HuntRewards.sol", Content: "contract HuntRewards { uint huntCount; }"})
	overview := Generate(input, time.Now())
	if len(overview.Sections[0].ClaimIDs) != 0 {
		t.Fatal("metadata or names became purpose")
	}
}

func TestSparseOrExcludedImplementationLeavesPurposeUnresolved(t *testing.T) {
	input := Input{RepositoryID: "repo", SnapshotID: "snapshot", CommitOID: strings.Repeat("b", 40),
		Artifacts: []Artifact{{Path: "ui/App.tsx", Status: "analyzed"}, {Path: "api/server.py", Status: "excluded"}},
		Evidence:  []Evidence{{ID: "ui", Path: "ui/App.tsx", StartLine: 1, EndLine: 10}}}
	overview := Generate(input, time.Now())
	if len(overview.Sections[0].ClaimIDs) != 0 {
		t.Fatalf("unsupported source became purpose: %+v", overview.Claims)
	}
	if err := Validate(input, overview); err != nil {
		t.Fatal(err)
	}
}

func TestFrontendRouteGuardDoesNotImplyServer(t *testing.T) {
	input := Input{RepositoryID: "repo", SnapshotID: "snapshot", CommitOID: strings.Repeat("b", 40),
		Artifacts: []Artifact{{Path: "frontend/App.tsx", Status: "analyzed"}, {Path: "frontend/RouteGuard.tsx", Status: "analyzed"}},
		Evidence: []Evidence{{ID: "app", Path: "frontend/App.tsx", StartLine: 1, EndLine: 10},
			{ID: "guard", Path: "frontend/RouteGuard.tsx", StartLine: 1, EndLine: 10}}}
	overview := Generate(input, time.Now())
	for _, claim := range overview.Claims {
		if strings.Contains(claim.Text, "server or API") || strings.Contains(claim.Text, "server source") {
			t.Fatalf("frontend routing was mistaken for server code: %+v", claim)
		}
	}
}

func TestGenericCodeWordsDoNotBecomePurpose(t *testing.T) {
	input := Input{RepositoryID: "repo", SnapshotID: "snapshot", CommitOID: strings.Repeat("b", 40),
		Artifacts: []Artifact{{Path: "one/Alpha.go", Status: "analyzed"}, {Path: "two/Beta.go", Status: "analyzed"},
			{Path: "three/Gamma.go", Status: "analyzed"}},
		Evidence: []Evidence{{ID: "a", Path: "one/Alpha.go", StartLine: 1, EndLine: 2},
			{ID: "b", Path: "two/Beta.go", StartLine: 1, EndLine: 2},
			{ID: "c", Path: "three/Gamma.go", StartLine: 1, EndLine: 2}},
		Snippets: []SourceText{{Path: "one/Alpha.go", StartLine: 1, Content: "package alpha\nvar packageName string"},
			{Path: "two/Beta.go", StartLine: 1, Content: "package beta\nvar packageName string"},
			{Path: "three/Gamma.go", StartLine: 1, Content: "package gamma\nvar packageName string"}}}
	overview := Generate(input, time.Now())
	if len(overview.Sections[0].ClaimIDs) != 0 {
		t.Fatalf("generic code vocabulary became purpose: %+v", overview.Claims)
	}
}

func TestOKFContainsSourceFootnotesAndNoHumanVerification(t *testing.T) {
	input := fixture()
	overview := Generate(input, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	overview.Claims = append(overview.Claims, Claim{ID: "documented", Text: "The README describes reusable helpers.", Kind: "fact", SupportStatus: "supported", EvidenceIDs: []string{"readme-evidence"}})
	overview.Sections[0].ClaimIDs = []string{"documented"}
	bundle, err := RenderOKF(overview, input, "https://github.com/example/library")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != len(overview.Sections)+1 {
		t.Fatalf("unexpected bundle size: %d", len(reader.File))
	}
	var purpose string
	for _, file := range reader.File {
		if file.Name != "concepts/purpose.md" {
			continue
		}
		body, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			t.Fatal(err)
		}
		purpose = string(content)
	}
	if !strings.Contains(purpose, "[^readme-evidence]") || !strings.Contains(purpose, "/blob/"+input.CommitOID+"/README.md#L1-L3") {
		t.Fatalf("missing portable source attribution: %s", purpose)
	}
	if strings.Contains(purpose, "verified:") {
		t.Fatal("invented verification event")
	}
}

func TestResolvedGoCallIsStaticClaim(t *testing.T) {
	input := fixture()
	input.Artifacts = append(input.Artifacts, Artifact{Path: "main.go", Status: "analyzed"})
	input.Evidence = append(input.Evidence, Evidence{ID: "go-evidence", Path: "main.go", StartLine: 1, EndLine: 20})
	input.Symbols = []Symbol{{ID: "main", Path: "main.go", Name: "main", QualifiedName: "./main.main", Kind: "function", StartLine: 2},
		{ID: "helper", Path: "main.go", Name: "helper", QualifiedName: "./main.helper", Kind: "function", StartLine: 10}}
	input.Relations = []Relation{{ID: "call", FromID: "main", ToID: "helper", Kind: "calls", Resolution: "resolved", Path: "main.go", Line: 5}}
	overview := Generate(input, time.Now())
	if err := Validate(input, overview); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, claim := range overview.Claims {
		if strings.Contains(claim.Text, "resolved calls link") && strings.Contains(claim.Text, "not an observed execution") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing calibrated static call claim")
	}
}

func TestExplicitStartupDeclarationsAreNotPresentedAsExecuted(t *testing.T) {
	input := fixture()
	input.Artifacts = append(input.Artifacts, Artifact{Path: "Dockerfile", Status: "analyzed"}, Artifact{Path: "package.json", Status: "analyzed"})
	input.Evidence = append(input.Evidence, Evidence{ID: "docker", Path: "Dockerfile", StartLine: 1, EndLine: 3}, Evidence{ID: "package", Path: "package.json", StartLine: 1, EndLine: 4})
	input.Snippets = []SourceText{{Path: "Dockerfile", StartLine: 1, Content: "FROM golang:1.27\nENTRYPOINT [\"./server\"]\nEXPOSE 8765"},
		{Path: "package.json", StartLine: 1, Content: "{\n  \"scripts\": {\n    \"start\": \"node app.js\"\n  }\n}"}}
	overview := Generate(input, time.Now())
	if err := Validate(input, overview); err != nil {
		t.Fatal(err)
	}
	var startup, interfaceClaim bool
	for _, claim := range overview.Claims {
		if strings.Contains(claim.Text, "npm script") && strings.Contains(claim.Text, "unresolved") {
			startup = true
		}
		if strings.Contains(claim.Text, "EXPOSE") && strings.Contains(claim.Text, "not a verified reachable service") {
			interfaceClaim = true
		}
	}
	if !startup || !interfaceClaim {
		t.Fatal("missing calibrated manifest declarations")
	}
}
