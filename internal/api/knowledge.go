package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"onboardmeplease/internal/knowledge"
	"onboardmeplease/internal/privacy"
)

func (server *Server) getOverview(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	overview, err := knowledge.Build(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "overview_unavailable", "Technical overview unavailable")
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (server *Server) getOverviewInventory(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	offset := 0
	limit := 100
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 1000000 {
			writeProblem(w, http.StatusBadRequest, "invalid_offset", "Offset must be nonnegative")
			return
		}
		offset = parsed
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeProblem(w, http.StatusBadRequest, "invalid_limit", "Limit must be 1-500")
			return
		}
		limit = parsed
	}
	// Ensure that every artifact has been assigned exactly one inventory category.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if _, err := knowledge.Get(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId")); err != nil {
		if _, err = knowledge.Build(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId")); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "overview_unavailable", "Technical overview unavailable")
			return
		}
	}
	rows, err := server.DB.Query(ctx, `SELECT a.path,a.status,a.reason_codes,o.category FROM overview_artifacts o
		JOIN artifacts a ON a.snapshot_id=o.snapshot_id AND a.path=o.path
		WHERE o.snapshot_id=$1 ORDER BY a.path OFFSET $2 LIMIT $3`, r.PathValue("snapshotId"), offset, limit)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "inventory_unavailable", "Overview inventory unavailable")
		return
	}
	defer rows.Close()
	type item struct {
		Path     string   `json:"path"`
		Status   string   `json:"status"`
		Reasons  []string `json:"reason_codes"`
		Category string   `json:"category"`
	}
	items := make([]item, 0)
	for rows.Next() {
		var value item
		if err := rows.Scan(&value.Path, &value.Status, &value.Reasons, &value.Category); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "inventory_unavailable", "Overview inventory unavailable")
			return
		}
		items = append(items, value)
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "inventory_unavailable", "Overview inventory unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": r.PathValue("snapshotId"), "offset": offset, "limit": limit, "artifacts": items})
}

func (server *Server) addOverviewNote(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		ClaimID string `json:"claim_id"`
		Note    string `json:"note"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_note", "Invalid correction note")
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 1 || len(input.Note) > 2000 {
		writeProblem(w, http.StatusBadRequest, "invalid_note", "Note must be 1-2000 bytes")
		return
	}
	if _, err := privacy.Clear("note", []byte(input.Note), nil); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "note_blocked", "Note failed the privacy policy")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	overview, err := knowledge.Build(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "overview_unavailable", "Technical overview unavailable")
		return
	}
	found := false
	for _, claim := range overview.Claims {
		if claim.ID == input.ClaimID {
			found = true
			break
		}
	}
	if !found {
		writeProblem(w, http.StatusBadRequest, "unknown_claim", "Correction must reference a current overview claim")
		return
	}
	var id int64
	var createdAt time.Time
	if err := server.DB.QueryRow(ctx, `INSERT INTO overview_notes(snapshot_id,claim_id,note) VALUES ($1,$2,$3) RETURNING id,created_at`, r.PathValue("snapshotId"), input.ClaimID, input.Note).Scan(&id, &createdAt); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "note_unavailable", "Cannot save correction note")
		return
	}
	writeJSON(w, http.StatusCreated, knowledge.Note{ID: id, ClaimID: input.ClaimID, Text: input.Note, CreatedAt: createdAt})
}

func (server *Server) deleteOverviewNote(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("noteId"), 10, 64)
	if err != nil || id < 1 {
		writeProblem(w, http.StatusBadRequest, "invalid_note", "Invalid correction ID")
		return
	}
	command, err := server.DB.Exec(r.Context(), `DELETE FROM overview_notes WHERE id=$1 AND snapshot_id=$2`, id, r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "note_unavailable", "Cannot remove correction note")
		return
	}
	if command.RowsAffected() == 0 {
		writeProblem(w, http.StatusNotFound, "note_not_found", "Correction note not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) exportOKF(w http.ResponseWriter, r *http.Request) {
	if !server.snapshotExists(r) {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Ready GitHub snapshot not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	bundle, err := knowledge.Export(ctx, server.DB, r.PathValue("repositoryId"), r.PathValue("snapshotId"))
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "okf_unavailable", "OKF export unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=onboardmeplease-okf-"+r.PathValue("snapshotId")+".zip")
	w.Header().Set("X-OKF-Spec-Revision", knowledge.OKFSpecRevision)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bundle)
}
