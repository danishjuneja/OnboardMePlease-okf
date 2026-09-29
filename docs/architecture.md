# Local evidence CLI

`cmd/omp` parses the six commands. `internal/engine` owns the workspace, SQLite store, indexing, Go relationship extraction, retrieval and claim lifecycle. Files separate these responsibilities without empty packages or provider abstractions. `skills/omp-investigate` is the canonical agent workflow. The former service is recoverable at Git revision `c6697e2`.

The CLI runs Git inventory commands and reads source; it has no HTTP server, generation model, API client, database server, queue or daemon. SQLite and FTS5 are embedded using the pure-Go modernc driver. Installation/building requires dependencies to have been provisioned; the compiled CLI does not download them. Go type extraction never executes a repository build or downloads packages.

## Source lifecycle

Each worktree stores its own `.onboard/cache/index.sqlite`. Git discovers tracked and eligible untracked files; deleted files, links, binary files, files over 2 MiB or with lines over 16 KiB, selected credential paths and secret-pattern matches are excluded. Generated source is retained with a label. `.onboard` and agent/graphify artifacts never enter raw-source indexing. The small config accepts one `exclude = ["relative/directory", "*.pattern"]` line.

Updates hash source and reuse unchanged parsed chunks. Parsed checkpoints survive interruption. A second content audit precedes one atomic SQLite publication. Readers use a read transaction. Retrieval/traversal audit the current inventory, including additions, before treating the graph as current. This full audit has a real I/O cost on large repositories. If a process is killed while holding `cache/writer.lock`, inspect its PID and remove that file only after confirming the writer has stopped. SQLite handles transaction rollback; the next update resumes parsed checkpoints. Both normal update and `--verify` currently perform a full hash audit: there is no metadata-only fast path disguised as complete verification.

Evidence IDs include relative path, full file hash, source range and extractor version. Line shifts or file edits invalidate old IDs. Source reads verify hashes. Go declarations provide chunk boundaries; oversized declarations split into bounded continuations. Unsupported languages use bounded line chunks. Source kind distinguishes implementation, test, configuration and documentation.

## Relationships and limits

Go AST/type checking uses the active platform's build-file selection and local module source. Local declarations are linked by compiler object identity, never global name matching. Preinstalled standard-library export archives may be read; absent exports and external modules remain gaps. Calls, references, imports and source-order continuations are distinct. Interface calls remain dynamic dispatch. Function-valued arguments produce references, not invented runtime calls.

The first supported semantic surface is Go. Java, Scala, TypeScript and Python are text-only; there is no universal framework/event resolver. Build tags, missing dependencies and unsupported constructs limit resolution. All local Go relationships are recomputed when source generation changes; fine-grained reverse-dependent extraction is a later optimization. Claims that rely on relationships conservatively depend on the full eligible inventory because type resolution can change outside the endpoint files.

FTS tokenization splits camelCase, snake_case and qualified identifiers. Search ranks lexical matches, promotes exact symbol/path matches and requires every significant query term for ordinary eligibility. This conservative rule is not a calibrated semantic confidence score; business-language paraphrases can miss. The skill handles that through direct source investigation. No vectors or embedding runtime are included (phase 7).

Traversal has direction, relationship-kind, depth, node and byte limits, cycle protection, and generation-bound cursors. Short cursor tokens refer to ignored local files; at most 128 recent cursor states are retained. Expired cursors require a new traversal. Evidence establishes a static relationship, not deployment or successful execution. High-degree branches and budget limits must remain visible rather than implying no further path exists.

## Knowledge

The agent authors structured findings. Apply validates generation, unit fingerprint, scope, source hashes and references. Claims retain their own evidence, conditions, assumptions and optional dependency scopes. A negative claim's scope hash detects additions as well as edits. Stale claims leave retrieval; current claims in the same document remain usable. Overlapping interpretations remain separate and may explicitly reference conflicts. Structural validation cannot establish semantic truth.

SQLite commits documents and claims together. Deterministic JSON/Markdown exports are repairable from the committed records after a crash. Portable exports carry the original dependency hashes and are revalidated per claim when rebuilding the cache. They are never rebased onto changed source. They are managed results, not a manual-document ingestion channel. The skill defines answer and knowledge-building workflows and the query-scoped fallback to the user's existing agent.
