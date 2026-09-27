package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"onboardmeplease/internal/config"
	"onboardmeplease/internal/grounding"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
	"onboardmeplease/internal/repository"
)

type Answer struct {
	ID         string             `json:"id"`
	SnapshotID string             `json:"snapshot_id"`
	Question   string             `json:"question"`
	ParentID   string             `json:"parent_id"`
	Claims     []grounding.Claim  `json:"claims"`
	Sources    []grounding.Source `json:"sources"`
	Concepts   []ConceptHit       `json:"concepts"`
	Gaps       []string           `json:"gaps"`
	Diagram    []DiagramLink      `json:"diagram"`
	Retrieval  string             `json:"retrieval"`
	CreatedAt  time.Time          `json:"created_at"`
}
type DiagramLink struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	EvidenceID string `json:"evidence_id"`
}

func Ask(ctx context.Context, pool *pgxpool.Pool, c config.Config, repo, snapshot, question, parent string) (Answer, error) {
	a, err := providers.ConfiguredGeneration(c)
	if err != nil {
		return Answer{}, err
	}
	policy, err := Policy(ctx, pool, c, repo, snapshot)
	if err != nil {
		return Answer{}, err
	}
	return AskWith(ctx, pool, c, a, policy, repo, snapshot, question, parent, RetrievalOptions{Knowledge: true, Graph: true, Vectors: true})
}

func AskWith(ctx context.Context, pool *pgxpool.Pool, c config.Config, model providers.Generator, policy privacy.ModelRequest, repo, snapshot, question, parent string, options RetrievalOptions) (Answer, error) {
	answer := Answer{SnapshotID: snapshot, Question: strings.TrimSpace(question), ParentID: parent, Claims: []grounding.Claim{}, Sources: []grounding.Source{}, Concepts: []ConceptHit{}, Gaps: []string{}, Diagram: []DiagramLink{}, CreatedAt: time.Now().UTC()}
	if len(answer.Question) == 0 || len(answer.Question) > 2000 {
		return answer, errors.New("question must be 1-2000 bytes")
	}
	if _, err := privacy.Clear("question", []byte(answer.Question), nil); err != nil {
		return answer, errors.New("question failed privacy scan")
	}
	// Ensure current index invalidates derived knowledge before retrieval.
	if _, err := Build(ctx, pool, repo, snapshot); err != nil {
		return answer, err
	}
	priorQuestions := []string{}
	priorIDs := []string{}
	cursor := parent
	for i := 0; cursor != "" && i < 4; i++ {
		var raw []byte
		var q string
		if err := pool.QueryRow(ctx, `SELECT question,answer FROM chat_turns WHERE snapshot_id=$1 AND id=$2`, snapshot, cursor).Scan(&q, &raw); err != nil {
			return answer, errors.New("follow-up turn is not in this snapshot")
		}
		var prior Answer
		if json.Unmarshal(raw, &prior) != nil {
			return answer, errors.New("follow-up turn unavailable")
		}
		priorQuestions = append(priorQuestions, q)
		for _, claim := range prior.Claims {
			priorIDs = append(priorIDs, claim.EvidenceIDs...)
		}
		cursor = prior.ParentID
	}
	// Search the question directly; follow-ups also reuse prior source IDs.
	// Avoid a planning model call before we know whether source exists.
	queries := []string{answer.Question}
	if len(priorQuestions) > 0 {
		queries = append(queries, priorQuestions[0])
	}
	retrieved, err := Retrieve(ctx, pool, c, policy, snapshot, queries, uniqueStrings(priorIDs), options)
	if err != nil {
		return answer, err
	}
	answer.Retrieval = retrievalSummary(retrieved)
	answer.Gaps = append(answer.Gaps, retrieved.Gaps...)
	answer.Concepts = retrieved.Concepts
	if len(retrieved.Sources) == 0 {
		answer.Gaps = append(answer.Gaps, "No matching source evidence was found. Try a handler, event, endpoint or component name.")
	} else {
		var draft grounding.Result
		err = grounding.Run(ctx, model, policy, "P07", "synthesis-result", map[string]any{"question": answer.Question, "prior_user_questions_newest_first": priorQuestions, "source_evidence": retrieved.Sources, "derived_okf_navigation": retrieved.Concepts, "static_relationships": retrieved.Edges, "retrieval_gaps": retrieved.Gaps}, &draft)
		if err != nil {
			return answer, err
		}
		reviewed, err := grounding.Assess(ctx, model, policy, draft, retrieved.Sources)
		if err != nil {
			return answer, err
		}
		answer.Claims = reviewed.Claims
		answer.Gaps = append(answer.Gaps, reviewed.Gaps...)
		cited := map[string]bool{}
		for _, claim := range answer.Claims {
			for _, id := range claim.EvidenceIDs {
				cited[id] = true
			}
		}
		for _, s := range retrieved.Sources {
			if cited[s.ID] {
				answer.Sources = append(answer.Sources, s)
			}
		}
		// Diagrams show parser-resolved static links, and only when the source at
		// the relationship location is also cited by an accepted answer claim.
		for _, edge := range retrieved.Edges {
			for _, s := range answer.Sources {
				if s.Path == edge.Path && edge.Line >= s.Start && edge.Line <= s.End {
					var from, to string
					err := pool.QueryRow(ctx, `SELECT a.qualified_name,b.qualified_name FROM symbols a JOIN symbols b ON b.snapshot_id=a.snapshot_id WHERE a.snapshot_id=$1 AND a.id=$2 AND b.id=$3`, snapshot, edge.FromID, edge.ToID).Scan(&from, &to)
					if err == nil && len(answer.Diagram) < 12 {
						answer.Diagram = append(answer.Diagram, DiagramLink{From: from, To: to, Kind: edge.Kind, EvidenceID: s.ID})
					}
					break
				}
			}
		}
	}
	if len(answer.Claims) == 0 {
		answer.Gaps = append(answer.Gaps, "The available evidence did not support an answer. No unsupported answer was published.")
	}
	answer.Gaps = uniqueStrings(answer.Gaps)
	// A concurrent reindex may replace ranges while the model is answering.
	checkedIDs := []string{}
	for _, source := range answer.Sources {
		checkedIDs = append(checkedIDs, source.ID)
	}
	if len(checkedIDs) > 0 {
		fresh, e := ReadSources(ctx, pool, snapshot, checkedIDs)
		if e != nil {
			return answer, e
		}
		byID := map[string]grounding.Source{}
		for _, source := range fresh {
			byID[source.ID] = source
		}
		for _, source := range answer.Sources {
			if byID[source.ID].Content != source.Content || byID[source.ID].Path != source.Path {
				return answer, errors.New("source index changed while answering; retry")
			}
		}
	}
	answer.ID, err = repository.NewID()
	if err != nil {
		return answer, err
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return answer, err
	}
	if _, err = privacy.Clear("answer", encoded, nil); err != nil {
		return answer, errors.New("answer failed privacy scan")
	}
	_, err = pool.Exec(ctx, `INSERT INTO chat_turns(id,snapshot_id,parent_id,question,answer) VALUES($1,$2,NULLIF($3,''),$4,$5)`, answer.ID, snapshot, parent, answer.Question, encoded)
	return answer, err
}
