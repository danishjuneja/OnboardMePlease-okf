-- Existing local snapshots remain readable, but new repository records must
-- use GitHub URLs. NOT VALID avoids rewriting or deleting historical records.
ALTER TABLE repositories
    ADD CONSTRAINT repositories_github_only_new CHECK (source_kind = 'github') NOT VALID;
