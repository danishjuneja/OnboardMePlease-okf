> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# Simplification decisions

The core job is to turn a pinned public repository into a source-backed overview and answer questions across its modules. Capture, secret filtering, source chunks, retrieval, bounded relationships, model generation, claim assessment and citations all contribute directly to that job.

Removed: standalone UI graph exploration, SCIP upload, semantic-index controls, inventory paging, correction editing, reindex/export actions, decorative landing layout and their state/handlers/styles. Correction write routes and handlers are removed as well. Existing stored data and optional technical APIs are preserved.

Capture now only captures and indexes. Analysis starts explicitly and keeps its existing eight-unit budget, successful-unit cache and provider-failure cancellation. Questions search directly and use prior user questions/source IDs for follow-ups, removing one planning generation call per question. Answer generation and claim assessment remain separate: removing source assessment would undermine the product's evidence contract. Direct retrieval may miss terminology that the previous planner expanded; users can refine a query with a symbol or component name.

Deployment remains two containers: one application and PostgreSQL/pgvector. River runs inside the application using the same database. There is no separate worker service, vector service or graph database to remove. Replacing durable jobs or storage would add a migration and recovery problem without demonstrated operating savings. Cloud calls are explicit and bounded, but this is not a dollar spending cap: prices and total usage depend on model choice, repository size and user requests.

This pass uses compilation only; no tests, repository evaluation, model requests or deployment are performed. Build success does not verify semantic answer quality. Optional embedding and SCIP code remain candidates for a later deliberate API-scope reduction, rather than being called dead code while clients can still invoke them.
