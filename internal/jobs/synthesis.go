package jobs

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"onboardmeplease/internal/config"
	"onboardmeplease/internal/knowledge"
	"time"
)

type SynthesisArgs struct {
	RepositoryID string `json:"repository_id"`
	SnapshotID   string `json:"snapshot_id"`
}

func (SynthesisArgs) Kind() string { return "synthesize_snapshot" }

type SynthesisWorker struct {
	river.WorkerDefaults[SynthesisArgs]
	DB       *pgxpool.Pool
	Settings config.Config
}

func (w *SynthesisWorker) Timeout(*river.Job[SynthesisArgs]) time.Duration { return 45 * time.Minute }
func (w *SynthesisWorker) Work(ctx context.Context, job *river.Job[SynthesisArgs]) error {
	_, err := knowledge.Synthesize(ctx, w.DB, w.Settings, job.Args.RepositoryID, job.Args.SnapshotID)
	// Explicit resume uses cached successful units. Avoid unattended repeated
	// provider charges when a model or credential is misconfigured.
	if err != nil {
		return river.JobCancel(err)
	}
	return nil
}
