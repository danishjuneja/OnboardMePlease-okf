CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS repositories (
    id text PRIMARY KEY,
    source_kind text NOT NULL CHECK (source_kind IN ('github', 'local')),
    source_locator text NOT NULL,
    requested_ref text NOT NULL DEFAULT '',
    snapshot_choice text NOT NULL DEFAULT '',
    privacy_mode text NOT NULL CHECK (privacy_mode IN ('strict_local', 'cloud_opt_in')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS snapshots (
    id text PRIMARY KEY,
    repository_id text NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    state text NOT NULL CHECK (state IN ('queued', 'capturing', 'partial', 'ready', 'failed')),
    source_kind text,
    commit_oid text,
    manifest_hash text,
    captured_at timestamptz,
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS snapshots_repository_created_idx ON snapshots(repository_id, created_at DESC);

CREATE TABLE IF NOT EXISTS artifacts (
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    path text NOT NULL,
    content_hash text,
    byte_size bigint NOT NULL DEFAULT 0,
    status text NOT NULL CHECK (status IN ('analyzed', 'excluded', 'unsupported', 'failed', 'pending')),
    reason_codes text[] NOT NULL DEFAULT '{}',
    PRIMARY KEY (snapshot_id, path)
);
CREATE INDEX IF NOT EXISTS artifacts_snapshot_status_idx ON artifacts(snapshot_id, status);
