package jobs

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/privacy"
	"onboardmeplease/internal/providers"
)

type EmbedArgs struct {
	RepositoryID string `json:"repository_id"`
	SnapshotID   string `json:"snapshot_id"`
	Provider     string `json:"provider"`
}

func (EmbedArgs) Kind() string { return "embed_snapshot" }

type EmbedWorker struct {
	river.WorkerDefaults[EmbedArgs]
	DB       *pgxpool.Pool
	Settings config.Config
}

func (worker *EmbedWorker) Timeout(_ *river.Job[EmbedArgs]) time.Duration {
	return worker.Settings.CaptureTimeout
}

func VectorLiteral(vector []float32) string {
	values := make([]string, len(vector))
	for i, value := range vector {
		values[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(values, ",") + "]"
}

func (worker *EmbedWorker) Work(ctx context.Context, job *river.Job[EmbedArgs]) error {
	var privacyMode, sourceKind string
	err := worker.DB.QueryRow(ctx, `SELECT privacy_mode,source_kind FROM repositories WHERE id=$1`, job.Args.RepositoryID).Scan(&privacyMode, &sourceKind)
	if err != nil || sourceKind != "github" {
		return river.JobCancel(errors.New("repository unavailable for embeddings"))
	}
	adapter, err := providers.ConfiguredEmbedding(worker.Settings, job.Args.Provider)
	if err != nil {
		return river.JobCancel(err)
	}
	request := privacy.ModelRequest{Mode: worker.Settings.ModelMode, RepoOptedIn: privacyMode == "cloud_opt_in"}
	request.ProviderKind = adapter.Kind
	request.ProviderURL = adapter.URL
	request.ScanSucceeded = true
	request.Sanitized = true
	if err := privacy.Authorize(request); err != nil {
		return river.JobCancel(err)
	}
	var ready bool
	err = worker.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM snapshots WHERE id=$1 AND repository_id=$2 AND state='ready')`, job.Args.SnapshotID, job.Args.RepositoryID).Scan(&ready)
	if err != nil || !ready {
		return river.JobCancel(errors.New("ready snapshot unavailable"))
	}
	var count int
	if err := worker.DB.QueryRow(ctx, `SELECT count(*) FROM evidence_chunks WHERE snapshot_id=$1`, job.Args.SnapshotID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return river.JobCancel(errors.New("snapshot has no indexed evidence"))
	}
	if count > 5000 {
		return river.JobCancel(errors.New("embedding request exceeds 5000 chunk limit"))
	}
	rows, err := worker.DB.Query(ctx, `SELECT id,path,content,content_hash FROM evidence_chunks WHERE snapshot_id=$1 ORDER BY path,start_line`, job.Args.SnapshotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, path, content, contentHash string
		if err := rows.Scan(&id, &path, &content, &contentHash); err != nil {
			return err
		}
		if _, err := privacy.Clear(path, []byte(content), nil); err != nil {
			return river.JobCancel(errors.New("evidence failed privacy scan"))
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		var existing bool
		err := worker.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM evidence_embeddings WHERE evidence_id=$1 AND provider=$2 AND model=$3 AND dimensions=$4 AND content_hash=$5)`,
			id, adapter.Kind, adapter.Model, adapter.Dimensions, contentHash).Scan(&existing)
		if err != nil {
			return err
		}
		if existing {
			continue
		}
		vector, err := adapter.Embed(ctx, request, content)
		if err != nil {
			return err
		}
		_, err = worker.DB.Exec(ctx, `INSERT INTO evidence_embeddings
			(evidence_id,provider,model,dimensions,content_hash,embedding)
			VALUES ($1,$2,$3,$4,$5,$6::vector) ON CONFLICT (evidence_id,provider,model,dimensions)
			DO UPDATE SET content_hash=excluded.content_hash,embedding=excluded.embedding,created_at=now()`,
			id, adapter.Kind, adapter.Model, adapter.Dimensions, contentHash, VectorLiteral(vector))
		if err != nil {
			return err
		}
	}
	return rows.Err()
}
