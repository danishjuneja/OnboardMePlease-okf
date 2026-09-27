> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# ADR 0001: layered repository analysis

Status: accepted for the first implementation. Date: 2026-09-24.

Implementation sequence clarified on 2026-09-25 in the [architecture rework](architecture-rework.md). The layered architecture remains: source evidence and graph produce initial overview/OKF knowledge, and both knowledge and source feed question retrieval. The deterministic overview preview is not the planned synthesis stage; extra frameworks and UI dependencies are deferred while that gap is addressed.

## Context

Vector similarity alone can find relevant snippets but often misses the links among a frontend, handler, worker, data store, and configuration. Generic GraphRAG can impose substantial extraction and summarization cost while a code repository offers stronger structural evidence. OKF is a portable representation of curated knowledge, not a retrieval engine or code analyzer.

## Decision

Use Go for API, ingestion orchestration, jobs, graph traversal, validation, and policy enforcement. Use Tree-sitter syntax analysis, optional SCIP semantic indexes, and framework adapters behind a language-neutral evidence model. Index exact identifiers, lexical text, and vectors in PostgreSQL with pgvector. Store typed relations in PostgreSQL and traverse them within explicit budgets. Generate technical OKF concepts from validated claims. Build a React/TypeScript browser UI served by the Go binary.

Run locally first with public GitHub URLs as the sole repository input. Container packaging supports Linux x86_64/arm64 after testing; Windows/macOS use Linux containers or separately tested native packages. A multi-user server profile requires authentication and repository authorization before release.

Strict local is the default privacy mode. Cloud generation and embeddings are opt-in per repository. A single egress layer enforces mode and sanitization; prompts never authorize network operations.

## Consequences

The common graph and retrieval contract works across languages, while depth of parsing and semantic resolution varies by adapter. Unsupported readable files remain searchable. Overview and chat must show capability gaps, missing configuration, bounded traversal, and unresolved links. We do not claim to reproduce runtime behavior without runtime evidence.

PostgreSQL holds graph, metadata, jobs, and vectors in one operational dependency. If scale measurements later justify a dedicated search or graph service, migrate behind the established interfaces and repeat the evaluation baseline.

All model prompts live in a versioned registry with typed inputs/outputs. OKF is produced by a deterministic renderer; models propose content and cite provided evidence IDs. Human verification is recorded only from a real review action.


Implementation note (2026-09-25): runtime synthesis uses bounded source units and an independent model support pass. Accepted claims produce both overview and stored OKF Markdown, with evidence dependencies reopened during chat retrieval. Go parsing and verified SCIP remain the implemented graph adapters; Tree-sitter/framework expansion in the original decision is deferred until a measured failure justifies it. OpenAI Responses with a backend key file is the first cloud generator; a compatible loopback chat-completions adapter is available for local operation.
