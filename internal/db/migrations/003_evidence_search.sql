CREATE TABLE IF NOT EXISTS evidence_chunks (
    id text PRIMARY KEY,
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    path text NOT NULL,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    language text NOT NULL,
    source_kind text NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    extractor_version text NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', path || ' ' || content)) STORED,
    UNIQUE (snapshot_id, path, start_line, end_line),
    FOREIGN KEY (snapshot_id, path) REFERENCES artifacts(snapshot_id, path) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS evidence_chunks_snapshot_idx ON evidence_chunks(snapshot_id);
CREATE INDEX IF NOT EXISTS evidence_chunks_fts_idx ON evidence_chunks USING gin(search_vector);

CREATE TABLE IF NOT EXISTS artifact_capabilities (
    snapshot_id text NOT NULL,
    path text NOT NULL,
    language text NOT NULL,
    text_status text NOT NULL CHECK (text_status IN ('available','unavailable','failed')),
    syntax_status text NOT NULL CHECK (syntax_status IN ('available','unavailable','failed')),
    semantic_status text NOT NULL CHECK (semantic_status IN ('available','unavailable','failed')),
    framework_status text NOT NULL CHECK (framework_status IN ('available','unavailable','failed')),
    reason text NOT NULL DEFAULT '',
    extractor_version text NOT NULL,
    PRIMARY KEY (snapshot_id, path),
    FOREIGN KEY (snapshot_id, path) REFERENCES artifacts(snapshot_id, path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS symbols (
    id text PRIMARY KEY,
    snapshot_id text NOT NULL,
    path text NOT NULL,
    name text NOT NULL,
    qualified_name text NOT NULL,
    kind text NOT NULL,
    language text NOT NULL,
    start_line integer NOT NULL CHECK (start_line > 0),
    end_line integer NOT NULL CHECK (end_line >= start_line),
    extractor_version text NOT NULL,
    external_key text,
    FOREIGN KEY (snapshot_id, path) REFERENCES artifacts(snapshot_id, path) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS symbols_snapshot_name_idx ON symbols(snapshot_id, name);

CREATE TABLE IF NOT EXISTS relations (
    id text PRIMARY KEY,
    snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    from_id text NOT NULL,
    to_id text NOT NULL,
    kind text NOT NULL,
    resolution text NOT NULL CHECK (resolution IN ('resolved','heuristic','proposed')),
    path text NOT NULL,
    line integer NOT NULL CHECK (line > 0),
    extractor_version text NOT NULL
);
CREATE INDEX IF NOT EXISTS relations_snapshot_from_idx ON relations(snapshot_id, from_id);
CREATE INDEX IF NOT EXISTS relations_snapshot_to_idx ON relations(snapshot_id, to_id);
