package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"onboardmeplease/internal/privacy"
)

// OKFSpecRevision pins the v0.2 specification used by this renderer.
const OKFSpecRevision = "0b87c52c6ef999286c745e19998fdfcd03d5dbee"

func markdown(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\r", " ", "\n", " ", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}

func sourceURL(remote, commit string, evidence Evidence) (string, error) {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || len(commit) != 40 {
		return "", errors.New("immutable GitHub source URL unavailable")
	}
	for _, char := range commit {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return "", errors.New("invalid commit ID")
		}
	}
	parts := strings.Split(evidence.Path, "/")
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid evidence path")
		}
		parts[i] = url.PathEscape(part)
	}
	root := strings.TrimSuffix(strings.TrimSuffix(remote, ".git"), "/")
	return fmt.Sprintf("%s/blob/%s/%s#L%d-L%d", root, commit, strings.Join(parts, "/"), evidence.StartLine, evidence.EndLine), nil
}

func RenderOKF(overview Overview, input Input, remote string) ([]byte, error) {
	if err := Validate(input, overview); err != nil {
		return nil, err
	}
	byID := make(map[string]Claim, len(overview.Claims))
	for _, claim := range overview.Claims {
		byID[claim.ID] = claim
	}
	evidence := make(map[string]Evidence, len(input.Evidence))
	for _, item := range input.Evidence {
		evidence[item.ID] = item
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	index := strings.Builder{}
	index.WriteString("---\nokf_version: \"0.2\"\n---\n\n# Repository knowledge\n\n")
	sections := overview.Sections
	if len(overview.Concepts) > 0 {
		sections = append([]Section{}, overview.Concepts...)
		for _, s := range overview.Sections {
			if s.Kind == "coverage" {
				sections = append(sections, s)
			}
		}
	}
	for _, section := range sections {
		name := "concepts/" + section.Kind + ".md"
		index.WriteString(fmt.Sprintf("- [%s](%s) - Source-backed %s and explicit gaps.\n", markdown(section.Title), name, markdown(strings.ToLower(section.Title))))
		used := make(map[string]bool)
		var ids []string
		for _, claimID := range section.ClaimIDs {
			claim, ok := byID[claimID]
			if !ok {
				return nil, errors.New("OKF section references missing claim")
			}
			for _, id := range claim.EvidenceIDs {
				if !used[id] {
					used[id] = true
					ids = append(ids, id)
				}
			}
		}
		var doc strings.Builder
		doc.WriteString("---\n")
		doc.WriteString("type: Repository Technical Overview\n")
		doc.WriteString("title: " + strconv.Quote(section.Title) + "\n")
		doc.WriteString("description: \"Static source-backed repository orientation; unresolved areas are explicit.\"\n")
		doc.WriteString("status: draft\n")
		doc.WriteString("generated: { by: process:onboardmeplease-" + GeneratorVersion + ", at: " + overview.GeneratedAt.Format("2006-01-02T15:04:05Z07:00") + " }\n")
		if len(ids) > 0 {
			doc.WriteString("sources:\n")
			for _, id := range ids {
				item, ok := evidence[id]
				if !ok {
					return nil, errors.New("OKF source missing")
				}
				link, err := sourceURL(remote, overview.CommitOID, item)
				if err != nil {
					return nil, err
				}
				doc.WriteString("  - id: " + id + "\n    resource: " + strconv.Quote(link) + "\n    title: " + strconv.Quote(fmt.Sprintf("%s:%d-%d", item.Path, item.StartLine, item.EndLine)) + "\n")
			}
		}
		doc.WriteString("---\n\n# " + markdown(section.Title) + "\n\n")
		if len(section.ClaimIDs) == 0 {
			doc.WriteString("No supported claim was established for this area.\n\n")
		}
		for _, id := range section.ClaimIDs {
			claim := byID[id]
			doc.WriteString("- " + markdown(claim.Text))
			if claim.Assumption != "" {
				doc.WriteString(" Inference limit: " + markdown(claim.Assumption))
			}
			for _, sourceID := range claim.EvidenceIDs {
				doc.WriteString("[^" + sourceID + "]")
			}
			doc.WriteString("\n")
		}
		if len(section.ClaimIDs) > 0 {
			doc.WriteString("\n")
		}
		for _, id := range ids {
			item := evidence[id]
			doc.WriteString(fmt.Sprintf("[^%s]: %s:%d-%d\n", id, markdown(item.Path), item.StartLine, item.EndLine))
		}
		if section.Kind == "coverage" {
			doc.WriteString(fmt.Sprintf("\nInventory: %d artifacts; %d unclassified. Counts by status: ", overview.Inventory.Total, overview.Inventory.Unclassified))
			for _, key := range []string{"analyzed", "excluded", "unsupported", "failed", "pending"} {
				doc.WriteString(fmt.Sprintf("%s %d; ", key, overview.Inventory.Statuses[key]))
			}
			doc.WriteString("\n\n")
		}
		for _, limitation := range overview.Limitations {
			if section.Kind == "coverage" {
				doc.WriteString("- Gap: " + markdown(limitation.Message) + "\n")
			}
		}
		content := doc.String()
		if _, err := privacy.Clear(name, []byte(content), nil); err != nil {
			return nil, errors.New("OKF document failed privacy policy")
		}
		file, err := writer.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	file, err := writer.Create("index.md")
	if err != nil {
		return nil, err
	}
	if _, err := file.Write([]byte(index.String())); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if buffer.Len() > 2<<20 {
		return nil, errors.New("OKF bundle exceeds size limit")
	}
	return buffer.Bytes(), nil
}

func Export(ctx context.Context, pool *pgxpool.Pool, repositoryID, snapshotID string) ([]byte, error) {
	overview, err := Build(ctx, pool, repositoryID, snapshotID)
	if err != nil {
		return nil, err
	}
	input, err := load(ctx, pool, repositoryID, snapshotID)
	if err != nil {
		return nil, err
	}
	var remote string
	if err := pool.QueryRow(ctx, `SELECT source_locator FROM repositories WHERE id=$1 AND source_kind='github'`, repositoryID).Scan(&remote); err != nil {
		return nil, err
	}
	return RenderOKF(overview, input, remote)
}
