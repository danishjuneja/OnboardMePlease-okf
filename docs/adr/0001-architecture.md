# ADR 0001: layered repository analysis

Status: accepted for the first implementation. Date: 2026-09-24.

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
