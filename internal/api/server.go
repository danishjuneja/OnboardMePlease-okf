package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/jobs"
	"onboardmeplease/internal/repository"
	"onboardmeplease/internal/webui"
)

type session struct {
	csrf      string
	expiresAt time.Time
}

type Server struct {
	DB       *pgxpool.Pool
	Jobs     *river.Client[pgx.Tx]
	Config   config.Config
	sessions map[string]session
	mu       sync.Mutex
}

func New(pool *pgxpool.Pool, client *river.Client[pgx.Tx], settings config.Config) *Server {
	return &Server{DB: pool, Jobs: client, Config: settings, sessions: make(map[string]session)}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /v1/session", server.session)
	mux.HandleFunc("POST /v1/repositories", server.withSession(server.addRepository))
	mux.HandleFunc("GET /v1/repositories", server.withSession(server.listRepositories))
	mux.HandleFunc("GET /v1/jobs/{jobId}", server.withSession(server.getJob))
	mux.HandleFunc("GET /v1/jobs/{jobId}/events", server.withSession(server.jobEvents))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}", server.withSession(server.getSnapshot))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/coverage", server.withSession(server.getCoverage))
	mux.HandleFunc("POST /v1/repositories/{repositoryId}/snapshots/{snapshotId}/index", server.withSession(server.indexSnapshot))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/search", server.withSession(server.searchEvidence))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/evidence/{evidenceId}", server.withSession(server.getEvidence))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/capabilities", server.withSession(server.getCapabilities))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/symbols", server.withSession(server.getSymbols))
	mux.HandleFunc("GET /v1/repositories/{repositoryId}/snapshots/{snapshotId}/graph", server.withSession(server.getGraph))
	mux.HandleFunc("POST /v1/repositories/{repositoryId}/snapshots/{snapshotId}/scip", server.withSession(server.importSCIP))
	mux.HandleFunc("POST /v1/repositories/{repositoryId}/snapshots/{snapshotId}/embeddings", server.withSession(server.queueEmbeddings))
	mux.HandleFunc("/", webui.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != server.Config.PublicHost {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "cross-site request denied", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+server.Config.PublicHost {
			http.Error(w, "origin denied", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func token() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", errors.New("cannot create local session")
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (server *Server) session(w http.ResponseWriter, r *http.Request) {
	id, err := token()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "session_unavailable", "Local session unavailable")
		return
	}
	csrf, err := token()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "session_unavailable", "Local session unavailable")
		return
	}
	server.mu.Lock()
	for key, existing := range server.sessions {
		if time.Now().After(existing.expiresAt) {
			delete(server.sessions, key)
		}
	}
	server.sessions[id] = session{csrf: csrf, expiresAt: time.Now().Add(12 * time.Hour)}
	server.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "omp_session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: server.Config.SessionSecure, MaxAge: 12 * 60 * 60})
	available := make([]string, 0)
	if server.Config.LocalEmbeddingURL != "" {
		available = append(available, "local")
	}
	if server.Config.ModelMode == "cloud_opt_in" && server.Config.OpenAIKeyFile != "" {
		available = append(available, "cloud")
	}
	writeJSON(w, http.StatusOK, map[string]any{"csrf_token": csrf, "model_mode": server.Config.ModelMode,
		"embedding_providers": available, "phase": 2})
}

func (server *Server) withSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("omp_session")
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "session_required", "Open the local application to start a session")
			return
		}
		server.mu.Lock()
		current, present := server.sessions[cookie.Value]
		server.mu.Unlock()
		if !present || time.Now().After(current.expiresAt) {
			writeProblem(w, http.StatusUnauthorized, "session_expired", "Local session expired")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-CSRF-Token") != current.csrf {
			writeProblem(w, http.StatusForbidden, "csrf_denied", "Request token missing or invalid")
			return
		}
		next(w, r)
	}
}

func (server *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := server.DB.Ping(ctx); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) addRepository(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input repository.Input
	if err := decoder.Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_input", "Invalid repository request")
		return
	}
	if server.Config.ModelMode == "strict_local" && input.PrivacyMode != "strict_local" {
		writeProblem(w, http.StatusForbidden, "privacy_mode_denied", "Cloud mode is not enabled in this installation")
		return
	}
	validated, err := repository.ValidateInput(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_repository", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	repositoryID, err := repository.NewID()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "id_unavailable", "Cannot create repository ID")
		return
	}
	snapshotID, err := repository.NewID()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "id_unavailable", "Cannot create snapshot ID")
		return
	}
	locator := validated.URL
	transaction, err := server.DB.Begin(ctx)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "Database unavailable")
		return
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Exec(ctx, `INSERT INTO repositories
		(id, source_kind, source_locator, requested_ref, privacy_mode)
		VALUES ($1,$2,$3,$4,$5)`, repositoryID, validated.Kind, locator,
		validated.Ref, validated.PrivacyMode); err != nil {
		writeProblem(w, http.StatusInternalServerError, "repository_create_failed", "Cannot create repository record")
		return
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO snapshots (id, repository_id, state)
        VALUES ($1,$2,'queued')`, snapshotID, repositoryID); err != nil {
		writeProblem(w, http.StatusInternalServerError, "snapshot_create_failed", "Cannot create snapshot record")
		return
	}
	inserted, err := server.Jobs.InsertTx(ctx, transaction, jobs.IngestArgs{RepositoryID: repositoryID, SnapshotID: snapshotID}, nil)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "queue_failed", "Cannot queue snapshot analysis")
		return
	}
	if err := transaction.Commit(ctx); err != nil {
		writeProblem(w, http.StatusInternalServerError, "repository_create_failed", "Cannot commit repository request")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"repository_id": repositoryID, "snapshot_id": snapshotID, "job_id": strconv.FormatInt(inserted.Job.ID, 10), "state": "queued"})
}

type jobState struct {
	ID    int64  `json:"job_id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
}

func (server *Server) readJob(ctx context.Context, rawID string) (jobState, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return jobState{}, errors.New("invalid job ID")
	}
	var job jobState
	if err := server.DB.QueryRow(ctx, `SELECT id, kind, state::text FROM river_job WHERE id=$1`, id).Scan(&job.ID, &job.Kind, &job.State); err != nil {
		return jobState{}, errors.New("job unavailable")
	}
	return job, nil
}

func (server *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := server.readJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "job_not_found", "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (server *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "stream_unavailable", "Event stream unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		job, err := server.readJob(r.Context(), r.PathValue("jobId"))
		if err != nil {
			return
		}
		payload, _ := json.Marshal(job)
		if _, err := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
		if job.State == "completed" || job.State == "cancelled" || job.State == "discarded" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (server *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	var id, repositoryID, state string
	var sourceKind, oid, manifestHash, failureCode *string
	var capturedAt *time.Time
	err := server.DB.QueryRow(r.Context(), `SELECT id, repository_id, state, source_kind, commit_oid,
        manifest_hash, failure_code, captured_at FROM snapshots WHERE id=$1 AND repository_id=$2`,
		r.PathValue("snapshotId"), r.PathValue("repositoryId")).Scan(&id, &repositoryID, &state, &sourceKind, &oid, &manifestHash, &failureCode, &capturedAt)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Snapshot not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "repository_id": repositoryID, "state": state,
		"source_kind": sourceKind, "commit_oid": oid, "manifest_hash": manifestHash,
		"failure_code": failureCode, "captured_at": capturedAt})
}

func (server *Server) getCoverage(w http.ResponseWriter, r *http.Request) {
	snapshotID, repositoryID := r.PathValue("snapshotId"), r.PathValue("repositoryId")
	var exists bool
	if err := server.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM snapshots WHERE id=$1 AND repository_id=$2)`, snapshotID, repositoryID).Scan(&exists); err != nil || !exists {
		writeProblem(w, http.StatusNotFound, "snapshot_not_found", "Snapshot not found")
		return
	}
	rows, err := server.DB.Query(r.Context(), `SELECT status, count(*) FROM artifacts WHERE snapshot_id=$1 GROUP BY status`, snapshotID)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "coverage_unavailable", "Coverage unavailable")
		return
	}
	defer rows.Close()
	counts := map[string]int64{"analyzed": 0, "excluded": 0, "unsupported": 0, "failed": 0, "pending": 0}
	var total int64
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "coverage_unavailable", "Coverage unavailable")
			return
		}
		counts[status] = count
		total += count
	}
	if rows.Err() != nil {
		writeProblem(w, http.StatusServiceUnavailable, "coverage_unavailable", "Coverage unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": snapshotID, "inventory_count": total, "status_counts": counts})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
