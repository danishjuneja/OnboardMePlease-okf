package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"onboardmeplease/internal/config"
	"onboardmeplease/internal/index"
	"onboardmeplease/internal/knowledge"
	"onboardmeplease/internal/repository"
)

type IngestArgs struct {
	RepositoryID string `json:"repository_id"`
	SnapshotID   string `json:"snapshot_id"`
}

func (IngestArgs) Kind() string { return "ingest_snapshot" }

type IngestWorker struct {
	Settings config.Config
	river.WorkerDefaults[IngestArgs]
	DB             *pgxpool.Pool
	DataDir        string
	CaptureTimeout time.Duration
}

func (worker *IngestWorker) Timeout(_ *river.Job[IngestArgs]) time.Duration {
	return worker.CaptureTimeout
}

func (worker *IngestWorker) Work(ctx context.Context, job *river.Job[IngestArgs]) error {
	var input repository.Input
	var sourceKind string
	if err := worker.DB.QueryRow(ctx, `SELECT source_kind, source_locator, requested_ref, privacy_mode
        FROM repositories WHERE id=$1`, job.Args.RepositoryID).Scan(
		&sourceKind, &input.URL, &input.Ref, &input.PrivacyMode,
	); err != nil {
		return errors.New("repository record unavailable")
	}
	if sourceKind != "github" {
		_, _ = worker.DB.Exec(context.Background(), `UPDATE snapshots SET state='failed', failure_code='source_kind_retired'
            WHERE id=$1 AND repository_id=$2 AND state <> 'ready'`, job.Args.SnapshotID, job.Args.RepositoryID)
		return river.JobCancel(errors.New("repository source kind is no longer supported"))
	}
	input.Kind = "github"
	validated, err := repository.ValidateInput(input)
	if err != nil {
		_, _ = worker.DB.Exec(context.Background(), `UPDATE snapshots SET state='failed', failure_code='source_revalidation_failed'
            WHERE id=$1 AND repository_id=$2`, job.Args.SnapshotID, job.Args.RepositoryID)
		return river.JobCancel(errors.New("repository source is no longer valid"))
	}
	if _, err := worker.DB.Exec(ctx, `UPDATE snapshots SET state='capturing', failure_code=NULL
        WHERE id=$1 AND repository_id=$2`, job.Args.SnapshotID, job.Args.RepositoryID); err != nil {
		return errors.New("cannot mark snapshot capturing")
	}
	manifest, err := repository.Capture(ctx, validated, worker.DataDir, job.Args.RepositoryID, job.Args.SnapshotID)
	if err != nil {
		_, _ = worker.DB.Exec(context.Background(), `UPDATE snapshots SET state='failed', failure_code='capture_failed'
            WHERE id=$1 AND repository_id=$2`, job.Args.SnapshotID, job.Args.RepositoryID)
		return fmt.Errorf("snapshot capture failed: %w", err)
	}
	transaction, err := worker.DB.Begin(ctx)
	if err != nil {
		return errors.New("cannot begin snapshot publication")
	}
	defer transaction.Rollback(context.Background())
	var existingArtifacts int
	if err := transaction.QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE snapshot_id=$1`, job.Args.SnapshotID).Scan(&existingArtifacts); err != nil {
		return errors.New("cannot inspect snapshot artifact rows")
	}
	if existingArtifacts != len(manifest.Artifacts) {
		if _, err := transaction.Exec(ctx, `DELETE FROM artifacts WHERE snapshot_id=$1`, job.Args.SnapshotID); err != nil {
			return errors.New("cannot reset snapshot artifact rows")
		}
	}
	rows := make([][]any, 0, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		rows = append(rows, []any{manifest.SnapshotID, artifact.Path, artifact.ContentHash, artifact.ByteSize, artifact.Status, artifact.ReasonCodes})
	}
	if existingArtifacts != len(manifest.Artifacts) {
		if _, err := transaction.CopyFrom(ctx, pgx.Identifier{"artifacts"}, []string{
			"snapshot_id", "path", "content_hash", "byte_size", "status", "reason_codes",
		}, pgx.CopyFromRows(rows)); err != nil {
			return errors.New("cannot publish artifact inventory")
		}
	}
	if _, err := transaction.Exec(ctx, `UPDATE snapshots
        SET state='ready', source_kind=$3, commit_oid=NULLIF($4,''), manifest_hash=$5, captured_at=$6, failure_code=NULL
        WHERE id=$1 AND repository_id=$2`, manifest.SnapshotID, manifest.RepositoryID,
		manifest.SourceKind, manifest.CommitOID, manifest.ManifestHash, manifest.CapturedAt); err != nil {
		return errors.New("cannot publish snapshot metadata")
	}
	if err := transaction.Commit(ctx); err != nil {
		return errors.New("cannot commit snapshot publication")
	}
	if err := index.Build(ctx, worker.DB, worker.DataDir, job.Args.RepositoryID, job.Args.SnapshotID); err != nil {
		return fmt.Errorf("snapshot evidence indexing failed: %w", err)
	}
	if _, err := knowledge.Build(ctx, worker.DB, job.Args.RepositoryID, job.Args.SnapshotID); err != nil {
		return fmt.Errorf("snapshot overview generation failed: %w", err)
	}
	return nil
}

func NewClient(pool *pgxpool.Pool, settings config.Config) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	if err := river.AddWorkerSafely(workers, &IngestWorker{DB: pool, DataDir: settings.DataDir, CaptureTimeout: settings.CaptureTimeout, Settings: settings}); err != nil {
		return nil, errors.New("cannot register ingestion worker")
	}
	if err := river.AddWorkerSafely(workers, &EmbedWorker{DB: pool, Settings: settings}); err != nil {
		return nil, errors.New("cannot register embedding worker")
	}
	if err := river.AddWorkerSafely(workers, &SynthesisWorker{DB: pool, Settings: settings}); err != nil {
		return nil, err
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Workers: workers,
	})
	if err != nil {
		return nil, errors.New("cannot initialize job queue")
	}
	return client, nil
}
