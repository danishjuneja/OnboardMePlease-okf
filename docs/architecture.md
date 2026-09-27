# Architecture

The service explains captured source without deploying or executing the repository. Its useful output is a technical overview and answers with inspectable source citations.

## Data flow

1. Capture a public, credential-free GitHub URL at an immutable commit. Scan and inventory files; do not run repository scripts.
2. Index readable source into citable chunks. Go syntax analysis adds declarations and direct static relationships. Other languages retain text search; optional SCIP import accepts only matching embedded source.
3. On explicit analysis, review bounded source units and independently assess proposed claims. Cache successful units by source, model and prompt/schema fingerprints.
4. Publish the overview and capability-oriented OKF concepts from accepted claims. Generated knowledge navigates to original evidence; it is not independent proof.
5. Questions retrieve source and knowledge using direct lexical searches and optional vectors. Prior user questions and cited source IDs supply follow-up context. Bounded relationship expansion and adjacent source reads share the retrieval budget.
6. Generate and assess the answer, then render citations from application-owned evidence IDs. Previous answers do not count as evidence.

## Runtime and code ownership

One Go application serves the embedded React UI, HTTP API and River workers. PostgreSQL/pgvector stores snapshots, inventories, evidence, static relationships, job state, cached analysis, knowledge and chat. Snapshot files live in the application data directory. River uses the same database; no separate worker, vector service or graph database is deployed.

| Backend package | Responsibility |
|---|---|
| `api` | HTTP routes, sessions, origin checks and request handling |
| `config` | Runtime environment and secret-file configuration |
| `repository` | Input validation, Git capture and inventory |
| `privacy` | Secret filtering and provider authorization |
| `index` | Source chunks, Go parsing and optional SCIP |
| `graph` | Bounded static relationship traversal |
| `knowledge` | Overview, synthesis, retrieval, answers and OKF rendering |
| `grounding` | Structured model results and source support assessment |
| `providers` | Generation and embedding adapters |
| `jobs` | Durable capture, synthesis and embedding work |
| `db` | Database migrations |
| `webui` | Embedded UI serving |

Migrations belong to the database package because the executable embeds and applies them. Prompt templates and schemas stay at the root because they are separately embedded Go packages. Frontend source and embedded output have different roles; moving them together would obscure the build boundary.

## Budgets and uncertainty

An analysis run reviews up to 8 uncached units, each at most 12 chunks / 24,000 source bytes. At most 600 accepted claims are published. Summary source has its own budget; remaining work and oversized chunks stay explicit gaps. Questions use bounded context and relationship traversal (3 hops, 60 shared expansions and bounded fan-out). Optional embedding runs process at most 250 missing source chunks and 100 concepts.

Removing automatic synthesis and question planning avoids unsolicited analysis and one generation request per question. Direct searches may miss terms a planner would have expanded. Generation and source assessment remain separate to preserve the evidence contract. Per-run limits do not impose a dollar cap.

The UI excludes correction editing, standalone graph controls, SCIP uploads, embedding controls and inventory paging. Advanced APIs remain available; stored notes and migrations are preserved. Private/local repositories, runtime tracing and shared Internet hosting are outside the current product scope.

## Trust boundaries

Source, comments, documentation, external indexes and questions are untrusted input. Secret scanning applies before searchable storage, model requests and exports, but can miss secrets or exclude useful code. Prompts cannot grant repository code tools or override application policy. Every query, cache and citation is scoped to a snapshot. Static relations and model claims do not prove runtime behavior.

Strict-local mode rejects cloud providers and has no cloud fallback. Cloud use requires installation-level configuration and repository consent. Credentials remain in backend secret files. The local single-user API enforces loopback/origin/session checks; it does not provide shared-host authentication or repository ACLs.
