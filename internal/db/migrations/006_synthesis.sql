CREATE TABLE analysis_units (
 snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
 cache_key text NOT NULL,
 evidence_ids text[] NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(snapshot_id,cache_key)
);
CREATE TABLE knowledge_concepts (
 snapshot_id text NOT NULL,
 version integer NOT NULL,
 id text NOT NULL,
 title text NOT NULL,
 markdown text NOT NULL,
 evidence_ids text[] NOT NULL,
 content_hash text NOT NULL,
 search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple',title || ' ' || markdown)) STORED,
 PRIMARY KEY(snapshot_id,version,id),
 FOREIGN KEY(snapshot_id,version) REFERENCES overview_versions(snapshot_id,version) ON DELETE CASCADE
);
CREATE INDEX knowledge_concepts_search ON knowledge_concepts USING gin(search_vector);
CREATE TABLE knowledge_embeddings (
 snapshot_id text NOT NULL,
 version integer NOT NULL,
 concept_id text NOT NULL,
 provider text NOT NULL,
 model text NOT NULL,
 dimensions integer NOT NULL,
 content_hash text NOT NULL,
 embedding vector NOT NULL,
 PRIMARY KEY(snapshot_id,version,concept_id,provider,model,dimensions),
 FOREIGN KEY(snapshot_id,version,concept_id) REFERENCES knowledge_concepts(snapshot_id,version,id) ON DELETE CASCADE
);
CREATE TABLE chat_turns (
 id text PRIMARY KEY,
 snapshot_id text NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
 parent_id text REFERENCES chat_turns(id),
 question text NOT NULL,
 answer jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_turns_snapshot ON chat_turns(snapshot_id,created_at);
