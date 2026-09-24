CREATE TABLE IF NOT EXISTS evidence_embeddings (
    evidence_id text NOT NULL REFERENCES evidence_chunks(id) ON DELETE CASCADE,
    provider text NOT NULL CHECK (provider IN ('local','cloud')),
    model text NOT NULL,
    dimensions integer NOT NULL CHECK (dimensions > 0 AND dimensions <= 2000),
    content_hash text NOT NULL,
    embedding vector NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (evidence_id, provider, model, dimensions)
);
CREATE INDEX IF NOT EXISTS evidence_embeddings_space_idx ON evidence_embeddings(provider,model,dimensions);
