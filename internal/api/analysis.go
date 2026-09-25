package api

import (
	"context"
	"encoding/json"
	"net/http"
	"onboardmeplease/internal/jobs"
	"onboardmeplease/internal/knowledge"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
	"strconv"
	"time"
)

func (s *Server) queueAnalysis(w http.ResponseWriter, r *http.Request) {
	if !s.snapshotExists(r) {
		writeProblem(w, 404, "snapshot_not_found", "Ready snapshot not found")
		return
	}
	var input struct {
		CloudOptIn bool `json:"cloud_opt_in"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil {
		writeProblem(w, 400, "invalid_request", "Invalid analysis request")
		return
	}
	a, err := providers.ConfiguredGeneration(s.Config)
	if err != nil {
		writeProblem(w, 422, "generation_unavailable", err.Error())
		return
	}
	if input.CloudOptIn {
		if s.Config.ModelMode != "cloud_opt_in" {
			writeProblem(w, 403, "cloud_disabled", "Cloud models are disabled in this installation")
			return
		}
		if _, err = s.DB.Exec(r.Context(), `UPDATE repositories SET privacy_mode='cloud_opt_in' WHERE id=$1`, r.PathValue("repositoryId")); err != nil {
			writeProblem(w, 503, "policy_unavailable", "Cannot update repository policy")
			return
		}
	}
	policy, err := knowledge.Policy(r.Context(), s.DB, s.Config, r.PathValue("repositoryId"), r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, 503, "policy_unavailable", "Repository policy unavailable")
		return
	}
	policy.ProviderKind = a.Kind
	policy.ProviderURL = a.URL
	policy.ScanSucceeded = true
	policy.Sanitized = true
	if privacy.Authorize(policy) != nil {
		writeProblem(w, 403, "generation_denied", "This repository requires explicit cloud opt-in before source can be sent to the model")
		return
	}
	inserted, err := s.Jobs.Insert(r.Context(), jobs.SynthesisArgs{RepositoryID: r.PathValue("repositoryId"), SnapshotID: r.PathValue("snapshotId")}, nil)
	if err != nil {
		writeProblem(w, 503, "analysis_queue_failed", "Cannot queue analysis")
		return
	}
	writeJSON(w, 202, map[string]any{"repository_id": r.PathValue("repositoryId"), "snapshot_id": r.PathValue("snapshotId"), "job_id": strconv.FormatInt(inserted.Job.ID, 10), "state": "queued"})
}

func (s *Server) askQuestion(w http.ResponseWriter, r *http.Request) {
	if !s.snapshotExists(r) {
		writeProblem(w, 404, "snapshot_not_found", "Ready snapshot not found")
		return
	}
	var input struct {
		Question string `json:"question"`
		ParentID string `json:"parent_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil {
		writeProblem(w, 400, "invalid_question", "Invalid question request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Minute)
	defer cancel()
	answer, err := knowledge.Ask(ctx, s.DB, s.Config, r.PathValue("repositoryId"), r.PathValue("snapshotId"), input.Question, input.ParentID)
	if err != nil {
		writeProblem(w, 422, "answer_unavailable", safeAnalysisError(err))
		return
	}
	writeJSON(w, 200, answer)
}

// Database and provider response bodies can contain source or connection data.
func safeAnalysisError(err error) string {
	if err == privacy.ErrEgressDenied {
		return "This repository has not opted in to the configured cloud model."
	}
	switch err.Error() {
	case "generation API quota exhausted (HTTP 429)", "generation rate limit exceeded (HTTP 429); wait before resuming", "generation quota or rate limit exceeded (HTTP 429)", "generation credential rejected (HTTP 401)", "generation request or model configuration rejected (HTTP 400)":
		return err.Error()
	case "question must be 1-2000 bytes", "question failed privacy scan", "follow-up turn is not in this snapshot", "generation provider unavailable", "generation incomplete or refused", "generation provider rejected request; check model, credentials and quota", "GENERATION_MODEL is not configured", "GENERATION_PROVIDER must be local or cloud", "cloud generation is not enabled":
		return err.Error()
	default:
		return "Analysis could not produce a validated answer. Check provider configuration and repository model policy; retry with a more specific question."
	}
}

func (s *Server) analysisStatus(w http.ResponseWriter, r *http.Request) {
	if !s.snapshotExists(r) {
		writeProblem(w, 404, "snapshot_not_found", "Ready snapshot not found")
		return
	}
	var id int64
	var state string
	err := s.DB.QueryRow(r.Context(), `SELECT id,state FROM river_job WHERE kind='synthesize_snapshot' AND args->>'repository_id'=$1 AND args->>'snapshot_id'=$2 ORDER BY id DESC LIMIT 1`, r.PathValue("repositoryId"), r.PathValue("snapshotId")).Scan(&id, &state)
	if err != nil {
		writeJSON(w, 200, map[string]any{"state": "not_started"})
		return
	}
	writeJSON(w, 200, map[string]any{"repository_id": r.PathValue("repositoryId"), "snapshot_id": r.PathValue("snapshotId"), "job_id": strconv.FormatInt(id, 10), "state": state})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	if !s.snapshotExists(r) {
		writeProblem(w, 404, "snapshot_not_found", "Ready snapshot not found")
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT answer FROM (SELECT answer,created_at FROM chat_turns WHERE snapshot_id=$1 ORDER BY created_at DESC LIMIT 30) recent ORDER BY created_at`, r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, 503, "chat_unavailable", "Conversation unavailable")
		return
	}
	defer rows.Close()
	answers := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			writeProblem(w, 503, "chat_unavailable", "Conversation unavailable")
			return
		}
		answers = append(answers, json.RawMessage(raw))
	}
	if rows.Err() != nil {
		writeProblem(w, 503, "chat_unavailable", "Conversation unavailable")
		return
	}
	writeJSON(w, 200, map[string]any{"answers": answers})
}
