package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Investigation is a bounded set of leads, not an answer or a proof of a path.
type Investigation struct {
	Sources       []InvestigationSource `json:"sources"`
	Notes         []InvestigationNote   `json:"notes"`
	Relationships []Edge                `json:"relationships"`
}
type InvestigationSource struct {
	ID               string `json:"id"`
	Path             string `json:"path"`
	Symbol           string `json:"symbol,omitempty"`
	Start            int    `json:"start_line"`
	End              int    `json:"end_line"`
	Excerpt          string `json:"excerpt"`
	ExcerptTruncated bool   `json:"excerpt_truncated"`
}
type InvestigationNote struct {
	ID               string   `json:"id"`
	Text             string   `json:"text"`
	TextTruncated    bool     `json:"text_truncated"`
	Kind             string   `json:"kind"`
	Confidence       string   `json:"confidence,omitempty"`
	Evidence         []string `json:"evidence_ids"`
	Conditions       []string `json:"conditions,omitempty"`
	Assumptions      []string `json:"assumptions,omitempty"`
	DetailsTruncated bool     `json:"details_truncated"`
}

func compactDetails(items []string) ([]string, bool) {
	truncated := len(items) > 4
	if truncated {
		items = items[:4]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		part, cut := excerpt(item, 300)
		truncated = truncated || cut
		out = append(out, part)
	}
	return out, truncated
}

func excerpt(text string, max int) (string, bool) {
	if len(text) <= max {
		return text, false
	}
	for max > 0 && !utf8.RuneStart(text[max]) {
		max--
	}
	return text[:max], true
}

// Investigate accepts a question and ranks lexical leads; it does not infer intent.
func (s *Store) Investigate(question string, limit int) (Envelope, error) {
	out := Investigation{Sources: []InvestigationSource{}, Notes: []InvestigationNote{}, Relationships: []Edge{}}
	r := s.Envelope("no_match", out)
	terms := queryTerms(question)
	if limit < 1 || limit > 10 || len(terms) > 32 {
		return r, fmt.Errorf("invalid_query: limit or question too large")
	}
	fresh, err := s.Audit()
	if err != nil {
		return r, err
	}
	if !fresh {
		r.State = "stale"
		r.Reasons = append(r.Reasons, "index_inputs_changed: run update")
		return r, nil
	}
	if len(terms) == 0 {
		r.Reasons = append(r.Reasons, "direct_source_investigation_required")
		return r, nil
	}
	fts := make([]string, len(terms))
	for i, term := range terms {
		fts[i] = "\"" + term + "\""
	}
	rows, err := s.Q.Query("SELECT id,kind FROM search WHERE search MATCH ? ORDER BY bm25(search),id LIMIT 200", strings.Join(fts, " OR "))
	if err != nil {
		return r, err
	}
	type candidate struct{ id, kind string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.kind); err != nil {
			rows.Close()
			return r, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	broadTruncated := len(candidates) == 200
	// BM25's top OR matches can be dominated by common words in a question.
	// Always include exact identifier matches before applying the result budget.
	words := strings.FieldsFunc(question, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	seen := map[string]bool{}
	for _, c := range candidates {
		seen[c.id] = true
	}
	if len(words) > 32 {
		words = words[:32]
	}
	for _, word := range words {
		if len([]rune(word)) < 3 {
			continue
		}
		matches, e := s.Q.Query("SELECT id FROM chunks WHERE json_extract(payload,'$.symbol')=? ORDER BY path,id LIMIT 20", word)
		if e != nil {
			return r, e
		}
		for matches.Next() {
			var id string
			if e = matches.Scan(&id); e != nil {
				break
			}
			if !seen[id] {
				candidates = append(candidates, candidate{id, "source"})
				seen[id] = true
			}
		}
		if e == nil {
			e = matches.Err()
		}
		matches.Close()
		if e != nil {
			return r, e
		}
		if unicode.IsUpper([]rune(word)[0]) {
			identifierTerms := strings.Fields(tokenize(word))
			if len(identifierTerms) > 0 {
				parts := make([]string, len(identifierTerms))
				for i, term := range identifierTerms {
					parts[i] = "\"" + term + "\""
				}
				extra, er := s.Q.Query("SELECT id FROM search WHERE search MATCH ? AND kind='source' ORDER BY bm25(search),id LIMIT 100", strings.Join(parts, " AND "))
				if er != nil {
					return r, er
				}
				for extra.Next() {
					var id string
					if er = extra.Scan(&id); er != nil {
						break
					}
					if !seen[id] {
						candidates = append(candidates, candidate{id, "source"})
						seen[id] = true
					}
				}
				if er == nil {
					er = extra.Err()
				}
				extra.Close()
				if er != nil {
					return r, er
				}
			}
		}
	}
	type ranked struct {
		chunk Chunk
		score int
	}
	var rankedSources []ranked
	questionTokens := " " + tokenize(question) + " "
	for _, c := range candidates {
		if c.kind == "claim" {
			claim, e := s.claim(c.id)
			if e != nil {
				return r, e
			}
			if !claim.Stale && s.claimCurrent(claim) && len(out.Notes) < limit {
				text, truncated := excerpt(claim.Claim.Text, 1200)
				conditions, conditionsTruncated := compactDetails(claim.Claim.Conditions)
				assumptions, assumptionsTruncated := compactDetails(claim.Claim.Assumptions)
				out.Notes = append(out.Notes, InvestigationNote{claim.Claim.ID, text, truncated, claim.Claim.Kind, claim.Claim.Confidence, claim.Claim.Evidence, conditions, assumptions, conditionsTruncated || assumptionsTruncated})
				if truncated || conditionsTruncated || assumptionsTruncated {
					r.Truncated = true
				}
			}
			continue
		}
		chunk, e := s.Chunk(c.id)
		if e != nil {
			return r, e
		}
		if !s.W.Current(chunk) {
			r.State = "partial"
			r.Reasons = append(r.Reasons, "stale_candidates_removed")
			continue
		}
		tokens := " " + tokenize(chunk.Path+" "+chunk.Symbol+" "+chunk.Text) + " "
		score := 0
		for _, term := range terms {
			if strings.Contains(tokens, " "+term+" ") {
				score++
			}
			if strings.Contains(" "+tokenize(chunk.Path)+" ", " "+term+" ") {
				score += 4
			}
		}
		for _, word := range words {
			if unicode.IsUpper([]rune(word)[0]) && (strings.Contains(chunk.Text, "type "+word+" ") || strings.Contains(chunk.Text, "class "+word) || strings.Contains(chunk.Text, "interface "+word)) {
				score += 60
			}
		}
		if chunk.Symbol != "" && strings.Contains(questionTokens, " "+tokenize(chunk.Symbol)+" ") {
			score += 20
			for _, word := range words {
				if word == chunk.Symbol && unicode.IsUpper([]rune(word)[0]) {
					score += 30
					break
				}
			}
		}
		rankedSources = append(rankedSources, ranked{chunk, score})
	}
	sort.SliceStable(rankedSources, func(i, j int) bool { return rankedSources[i].score > rankedSources[j].score })
	for _, item := range rankedSources {
		if len(out.Sources) >= limit {
			r.Truncated = true
			break
		}
		c := item.chunk
		text, truncated := excerpt(c.Text, 1200)
		out.Sources = append(out.Sources, InvestigationSource{c.ID, c.Path, c.Symbol, c.Start, c.End, text, truncated})
		if truncated {
			r.Truncated = true
		}
	}
	if len(out.Sources) > 0 {
		rows, err = s.Q.Query("SELECT payload FROM edges WHERE src=? OR dst=? ORDER BY id LIMIT 9", out.Sources[0].ID, out.Sources[0].ID)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			if len(out.Relationships) == 8 {
				r.Truncated = true
				r.Reasons = append(r.Reasons, "relationship_budget_reached")
				break
			}
			var raw string
			var edge Edge
			if err = rows.Scan(&raw); err != nil {
				break
			}
			if err = json.Unmarshal([]byte(raw), &edge); err != nil {
				break
			}
			out.Relationships = append(out.Relationships, edge)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return r, err
		}
	}
	r.Data = out
	if len(out.Sources)+len(out.Notes) > 0 && r.State != "partial" {
		r.State = "matched"
	}
	if len(out.Sources)+len(out.Notes) == 0 {
		r.Reasons = append(r.Reasons, "direct_source_investigation_required")
	}
	if broadTruncated {
		r.Truncated = true
		r.Reasons = append(r.Reasons, "candidate_budget_reached")
	}
	if r.Truncated {
		r.Reasons = append(r.Reasons, "result_budget_reached")
		if r.State == "matched" {
			r.State = "partial"
		}
	}
	r.Warnings = append(r.Warnings, "Lexical leads and relationships require source verification; candidate relationships are not proof.")
	return r, nil
}
