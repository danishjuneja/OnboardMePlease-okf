# Managed result contract (version 1)

Use exact `generation`, `unit_id`, `fingerprint`, paths and evidence IDs from the CLI. Unknown fields are rejected. Run `omp knowledge apply <file> --dry-run --json` for the executable structural check.

```json
{
  "schema_version": 1,
  "generation": "from the response envelope",
  "unit_id": "from knowledge pending",
  "fingerprint": "from that unit",
  "title": "Paid-order cancellation",
  "reviewed_paths": ["server/cancel.go", "server/store.go"],
  "assessment": "agent_assessed",
  "facets": {
    "entry_points": "supported",
    "conditions": "supported",
    "state_changes": "supported",
    "transactions": "unresolved",
    "downstream": "unresolved",
    "failures": "supported"
  },
  "claims": [{
    "id": "paid-state-guard",
    "text": "The cancellation handler rejects orders whose state is not paid.",
    "kind": "source_fact",
    "evidence_ids": ["exact content-bound evidence ID"],
    "relation_ids": [],
    "conditions": ["The registered cancellation handler is invoked."],
    "assumptions": [],
    "dependency_scopes": ["server"],
    "conflicts_with": []
  }],
  "unresolved": ["Broker delivery and transaction atomicity were not established."]
}
```

Claim kinds are `source_fact`, `inference`, and `negative`. Inference requires explicit assumptions. Negative findings require a repository-relative directory/file scope (`.` means the complete eligible inventory). Scopes include additions and deletions, not just existing cited files. A relation ID must exist and have a target; unresolved dispatch is a gap, not accepted connection evidence.

Facets accept `supported`, `unresolved`, or `not_applicable`. They are agent assessments. A partial review or unresolved facet leaves the unit pending. Reviewed paths may extend beyond the starting unit but must include an anchor within it. Conditions and assumptions travel with retrieved claims. Claim IDs are namespaced by document on storage; use stored IDs in `conflicts_with` to identify other findings. Documents group claims but do not turn several claims into independent corroboration.

Reapplying the same title, unit and reviewed path set replaces that document's claims idempotently. A new title makes a separate interpretation, which may conflict with existing findings. `.onboard/knowledge/*.json` and Markdown are managed exports. Keep human review notes separately; importing arbitrary Markdown is not supported. Managed JSON exports wrap the result with its original claim dependencies; they are distinct from submission JSON. On a rebuilt cache, each exported claim is revalidated against that original provenance. Stale claims remain recorded but cannot enter current retrieval.
