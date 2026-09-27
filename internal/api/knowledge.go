package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"onboardmeplease/internal/knowledge"
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
