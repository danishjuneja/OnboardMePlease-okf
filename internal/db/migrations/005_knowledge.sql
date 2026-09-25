CREATE TABLE IF NOT EXISTS overview_versions (
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    fingerprint text NOT NULL,
    document jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (snapshot_id, version)
);
CREATE INDEX IF NOT EXISTS overview_versions_latest_idx ON overview_versions(snapshot_id, version DESC);

CREATE TABLE IF NOT EXISTS overview_artifacts (
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    path text NOT NULL,
    category text NOT NULL,
    PRIMARY KEY (snapshot_id, path),
    FOREIGN KEY (snapshot_id, path) REFERENCES artifacts(snapshot_id, path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS overview_notes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    claim_id text NOT NULL,
    note text NOT NULL CHECK (length(note) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS overview_notes_snapshot_idx ON overview_notes(snapshot_id, created_at);
