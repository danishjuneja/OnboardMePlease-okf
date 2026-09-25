import { FormEvent, useEffect, useState } from 'react'
import KnowledgeChat from './KnowledgeChat'

type Session = { generation_available: boolean; generation_provider: string; generation_model: string; csrf_token: string; model_mode: 'strict_local' | 'cloud_opt_in'; embedding_providers: string[]; phase: number }
type Queued = { repository_id: string; snapshot_id: string; job_id: string; state: string }
type Job = { failure?: string; job_id: number; kind: string; state: string }
type Coverage = { snapshot_id: string; inventory_count: number; status_counts: Record<string, number> }
type Repository = { repository_id: string; url: string; snapshot_id: string; state: string; commit_oid: string | null }
type SearchResult = { evidence_id: string; path: string; start_line: number; end_line: number; language: string; snippet: string }
type Evidence = { evidence_id: string; source_ranges: { artifact_id: string; start_line: number; end_line: number }[]; snippet: string }
type Symbol = { symbol_id: string; path: string; name: string; qualified_name: string; kind: string; start_line: number }
type Graph = { nodes: Symbol[]; edges: { from_id: string; to_id: string; kind: string; resolution: string; path: string; line: number }[]; truncated: boolean }
type SCIPReport = { imported_documents: number; skipped_documents: number; symbols: number; relations: number }
type OverviewClaim = { assumption?: string; claim_id: string; text: string; kind: string; evidence_ids: string[]; support_status: string }
type OverviewSection = { kind: string; title: string; claim_ids: string[] }
type OverviewNote = { id: number; claim_id: string; note: string; orphaned: boolean }
type Overview = { snapshot_id: string; commit_oid: string; state: 'ready' | 'partial' | 'stale' | 'evidence_only'; synthesis?: { reviewed_chunks: number; total_chunks: number; reviewed_units: number; total_units: number; model: string }; concepts?: OverviewSection[]; version: number; generated_at: string; sections: OverviewSection[]; claims: OverviewClaim[];
  limitations: { code: string; message: string }[]; inventory: { total: number; statuses: Record<string, number>; categories: Record<string, number>; unclassified: number }; notes: OverviewNote[] }
type InventoryItem = { path: string; status: string; reason_codes: string[]; category: string }

async function json<T>(response: Response): Promise<T> {
  const value = await response.json()
  if (!response.ok) throw new Error(value.message ?? `Request failed (${response.status})`)
  return value as T
}

export default function App() {
  const [session, setSession] = useState<Session | null>(null)
  const [url, setUrl] = useState('')
  const [ref, setRef] = useState('')
  const [privacy, setPrivacy] = useState<'strict_local' | 'cloud_opt_in'>('strict_local')
  const [repositories, setRepositories] = useState<Repository[]>([])
  const [active, setActive] = useState<Repository | null>(null)
  const [queued, setQueued] = useState<Queued | null>(null)
  const [job, setJob] = useState<Job | null>(null)
  const [coverage, setCoverage] = useState<Coverage | null>(null)
  const [query, setQuery] = useState('')
  const [searchMode, setSearchMode] = useState<'lexical' | 'semantic'>('lexical')
  const [embeddingProvider, setEmbeddingProvider] = useState('local')
  const [results, setResults] = useState<SearchResult[]>([])
  const [searched, setSearched] = useState(false)
  const [evidence, setEvidence] = useState<Evidence | null>(null)
  const [symbolQuery, setSymbolQuery] = useState('')
  const [symbols, setSymbols] = useState<Symbol[]>([])
  const [graph, setGraph] = useState<Graph | null>(null)
  const [scipFile, setScipFile] = useState<File | null>(null)
  const [scipReport, setScipReport] = useState<SCIPReport | null>(null)
  const [overview, setOverview] = useState<Overview | null>(null)
  const [inventoryItems, setInventoryItems] = useState<InventoryItem[]>([])
  const [inventoryOffset, setInventoryOffset] = useState(0)
  const [noteClaim, setNoteClaim] = useState('')
  const [noteText, setNoteText] = useState('')
  const [error, setError] = useState('')

  async function refreshRepositories() {
    const response = await fetch('/v1/repositories', { credentials: 'same-origin' })
    const data = await json<{ repositories: Repository[] }>(response)
    setRepositories(data.repositories)
  }

  async function selectRepository(repository: Repository) {
    setActive(repository); setCoverage(null); setOverview(null); setInventoryItems([]); setInventoryOffset(0); setNoteClaim(''); setNoteText(''); setResults([]); setSearched(false); setEvidence(null); setSymbols([]); setGraph(null); setScipReport(null); setScipFile(null); setError('')
    if (repository.state === 'ready') {
      try {
        const base = `/v1/repositories/${repository.repository_id}/snapshots/${repository.snapshot_id}`
        const [coverageResponse, overviewResponse] = await Promise.all([
          fetch(`${base}/coverage`, { credentials: 'same-origin' }),
          fetch(`${base}/overview`, { credentials: 'same-origin' }),
        ])
        setCoverage(await json<Coverage>(coverageResponse))
        setOverview(await json<Overview>(overviewResponse))
        const analysisResponse = await fetch(`${base}/analysis`, { credentials: 'same-origin' })
        if (analysisResponse.ok) {
          const analysis = await analysisResponse.json() as Queued
          if (analysis.job_id && ['available', 'running', 'retryable', 'scheduled', 'pending'].includes(analysis.state)) setQueued(analysis)
          else if (analysis.job_id && ['cancelled', 'discarded'].includes(analysis.state)) {
            const failed = await json<Job>(await fetch(`/v1/jobs/${analysis.job_id}`, { credentials: 'same-origin' }))
            setError(failed.failure ?? `Analysis ${failed.state}; successful batches are cached. Use Analyze or resume overview to retry.`)
          }
        }
      } catch (cause) { setError(cause instanceof Error ? cause.message : 'Overview unavailable') }
    }
  }

  useEffect(() => {
    fetch('/v1/session', { credentials: 'same-origin' })
      .then((response) => json<Session>(response))
      .then((value) => { setSession(value); if (value.embedding_providers.length > 0) setEmbeddingProvider(value.embedding_providers[0]); return refreshRepositories() })
      .catch((cause: Error) => setError(cause.message))
  }, [])

  useEffect(() => {
    if (!queued) return
    const events = new EventSource(`/v1/jobs/${queued.job_id}/events`)
    events.addEventListener('progress', async (event) => {
      const next = JSON.parse((event as MessageEvent).data) as Job
      setJob(next)
      if (!['completed', 'cancelled', 'discarded'].includes(next.state)) return
      events.close()
      if (next.state !== 'completed') { setError(next.failure ?? `Background job ${next.state}; successful analysis batches are cached for resume`); return }
      try {
        const prefix = `/v1/repositories/${queued.repository_id}/snapshots/${queued.snapshot_id}`
        const snapshot = await json<{ state: string; commit_oid: string | null }>(await fetch(prefix, { credentials: 'same-origin' }))
        if (snapshot.state !== 'ready') throw new Error(`Snapshot ${snapshot.state}`)
        const repositoryURL = repositories.find(item => item.repository_id === queued.repository_id)?.url ?? url.trim()
        await selectRepository({ repository_id: queued.repository_id, snapshot_id: queued.snapshot_id, url: repositoryURL, state: snapshot.state, commit_oid: snapshot.commit_oid })
        await refreshRepositories()
      } catch (cause) { setError(cause instanceof Error ? cause.message : 'Snapshot unavailable') }
    })
    events.onerror = () => { events.close(); setError('Progress stream disconnected; reopen the snapshot from Recent repositories') }
    return () => events.close()
  }, [queued])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!session) return
    setError(''); setQueued(null); setJob(null); setCoverage(null)
    const payload = { kind: 'github', url: url.trim(), ...(ref.trim() ? { ref: ref.trim() } : {}), privacy_mode: privacy }
    try {
      const response = await fetch('/v1/repositories', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
        body: JSON.stringify(payload),
      })
      setQueued(await json<Queued>(response))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Repository request failed') }
  }

  async function reindex() {
    if (!active || !session) return
    setError(''); setJob(null)
    try {
      const response = await fetch(`/v1/repositories/${active.repository_id}/snapshots/${active.snapshot_id}/index`, {
        method: 'POST', credentials: 'same-origin', headers: { 'X-CSRF-Token': session.csrf_token },
      })
      setQueued(await json<Queued>(response))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Index request failed') }
  }

  async function embedSnapshot() {
    if (!active || !session) return
    setError(''); setJob(null)
    try {
      const response = await fetch(`${prefix()}/embeddings`, { method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
        body: JSON.stringify({ provider: embeddingProvider }) })
      setQueued(await json<Queued>(response))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Embedding request failed') }
  }

  function prefix() { return `/v1/repositories/${active?.repository_id}/snapshots/${active?.snapshot_id}` }

  async function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!active) return
    setError(''); setEvidence(null); setSearched(false)
    try {
      const params = new URLSearchParams({ q: query.trim(), mode: searchMode })
      if (searchMode === 'semantic') params.set('provider', embeddingProvider)
      const response = await fetch(`${prefix()}/search?${params}`, { credentials: 'same-origin' })
      const data = await json<{ results: SearchResult[] }>(response)
      setResults(data.results); setSearched(true)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Search unavailable') }
  }

  async function openEvidence(id: string) {
    setError('')
    try {
      const response = await fetch(`${prefix()}/evidence/${encodeURIComponent(id)}`, { credentials: 'same-origin' })
      setEvidence(await json<Evidence>(response))
      window.setTimeout(() => document.getElementById('source-view')?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 0)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Evidence unavailable') }
  }

  async function findSymbols(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!active) return
    setError(''); setGraph(null)
    try {
      const response = await fetch(`${prefix()}/symbols?q=${encodeURIComponent(symbolQuery.trim())}`, { credentials: 'same-origin' })
      const data = await json<{ symbols: Symbol[] }>(response)
      setSymbols(data.symbols)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Symbols unavailable') }
  }

  async function explore(symbol: Symbol) {
    setError('')
    try {
      const response = await fetch(`${prefix()}/graph?symbol_id=${encodeURIComponent(symbol.symbol_id)}`, { credentials: 'same-origin' })
      setGraph(await json<Graph>(response))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Graph unavailable') }
  }

  async function uploadSCIP() {
    if (!active || !session || !scipFile) return
    setError(''); setScipReport(null)
    try {
      const response = await fetch(`${prefix()}/scip`, { method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/octet-stream', 'X-CSRF-Token': session.csrf_token }, body: scipFile })
      setScipReport(await json<SCIPReport>(response))
      setOverview(await json<Overview>(await fetch(`${prefix()}/overview`, { credentials: 'same-origin' })))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'SCIP import failed') }
  }

  async function loadInventory(offset: number) {
    if (!active) return
    setError('')
    try {
      const response = await fetch(`${prefix()}/overview/inventory?offset=${offset}&limit=100`, { credentials: 'same-origin' })
      const data = await json<{ artifacts: InventoryItem[] }>(response)
      setInventoryItems(data.artifacts); setInventoryOffset(offset)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Inventory unavailable') }
  }

  async function saveNote() {
    if (!active || !session || !noteClaim || !noteText.trim()) return
    setError('')
    try {
      const response = await fetch(`${prefix()}/overview/notes`, { method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
        body: JSON.stringify({ claim_id: noteClaim, note: noteText.trim() }) })
      await json<OverviewNote>(response)
      setOverview(await json<Overview>(await fetch(`${prefix()}/overview`, { credentials: 'same-origin' })))
      setNoteClaim(''); setNoteText('')
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not save correction') }
  }

  async function deleteNote(id: number) {
    if (!active || !session) return
    setError('')
    try {
      const response = await fetch(`${prefix()}/overview/notes/${id}`, { method: 'DELETE', credentials: 'same-origin', headers: { 'X-CSRF-Token': session.csrf_token } })
      if (!response.ok) { await json(response); return }
      setOverview(await json<Overview>(await fetch(`${prefix()}/overview`, { credentials: 'same-origin' })))
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Could not remove correction') }
  }

  async function downloadOKF() {
    if (!active) return
    setError('')
    try {
      const response = await fetch(`${prefix()}/knowledge/export`, { credentials: 'same-origin' })
      if (!response.ok) { await json(response); return }
      const blob = await response.blob()
      const objectURL = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = objectURL; link.download = `onboardmeplease-okf-${active.snapshot_id}.zip`
      document.body.appendChild(link); link.click(); link.remove(); window.setTimeout(() => URL.revokeObjectURL(objectURL), 1000)
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'OKF export unavailable') }
  }

  return <main className="shell">
    <header><span className="brand">OnboardMePlease</span><span className="phase">Repository understanding</span></header>
    <section className="intro">
      <p className="eyebrow">Repository onboarding</p>
      <h1>Explore the code behind the flow.</h1>
      <p>Capture a public GitHub commit, explore its technical overview, and ask questions grounded in source and OKF knowledge.</p>
    </section>
    <section className="panel">
      <h2>Add a GitHub repository</h2>
      <form onSubmit={submit}>
        <label>GitHub repository URL<input required type="url" value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://github.com/owner/repo" /></label>
        <label>Branch or tag (optional)<input value={ref} onChange={(event) => setRef(event.target.value)} placeholder="Default branch" /></label>
        <label>Model data policy<select value={privacy} onChange={(event) => setPrivacy(event.target.value as 'strict_local' | 'cloud_opt_in')}>
          <option value="strict_local">Strict local</option>
          {session?.model_mode === 'cloud_opt_in' && <option value="cloud_opt_in">Cloud opt-in for this repository</option>}
        </select></label>
        <p className="hint">Capture fetches public GitHub content. A configured model analyzes source after indexing when this repository’s model policy permits it.</p>
        <button className="primary" disabled={!session || !url.trim()}>Capture and index</button>
      </form>
    </section>
    {error && <p className="error" role="alert">{error}</p>}
    {queued && <section className="panel result" aria-live="polite"><h2>Job progress</h2>
      <dl><dt>Repository</dt><dd>{queued.repository_id}</dd><dt>Snapshot</dt><dd>{queued.snapshot_id}</dd><dt>Job</dt><dd>{job?.state ?? queued.state}</dd></dl>
    </section>}
    <section className="panel"><h2>Recent repositories</h2>
      {repositories.length === 0 ? <p className="hint">No GitHub snapshots yet.</p> : <ul className="repository-list">{repositories.map((item) => <li key={item.snapshot_id}>
        <button type="button" className="link-button" onClick={() => selectRepository(item)}>{item.url.replace(/\.git$/, '')}</button>
        <span className="hint">{item.state}{item.commit_oid ? ` · ${item.commit_oid.slice(0, 12)}` : ''}</span>
      </li>)}</ul>}
    </section>
    {active?.state === 'ready' && session && <KnowledgeChat base={prefix()} session={session} onQueued={setQueued} onEvidence={openEvidence} />}
    {active && overview && <section className="panel overview"><h2>Technical overview</h2>
      <p className="hint">Captured commit {overview.commit_oid || 'unavailable'} · overview v{overview.version} · {overview.state} · {overview.synthesis ? 'model-reviewed source' : 'extracted declarations'}</p>
      <div className="overview-summary"><div><strong>{overview.inventory.total}</strong><span> inventoried artifacts</span></div>
        <div><strong>{overview.inventory.statuses.analyzed ?? 0}</strong><span> analyzed</span></div>
        <div><strong>{overview.inventory.unclassified}</strong><span> unclassified</span></div></div>
      <p className="hint">Inventory counts show indexing coverage. Semantic review coverage is reported separately below.</p>
      {overview.synthesis ? <p className="hint">Semantically reviewed {overview.synthesis.reviewed_chunks} / {overview.synthesis.total_chunks} chunks across {overview.synthesis.reviewed_units} / {overview.synthesis.total_units} batches · {overview.synthesis.model}</p> : <p className="hint">Source synthesis has not run. These are extracted declarations, not a generated repository explanation.</p>}
      {overview.sections.map((section) => <details className="overview-section" key={section.kind} open={section.kind === 'purpose' || section.kind === 'runtime'}>
        <summary><strong>{section.title}</strong><span>{section.claim_ids.length} cited claims</span></summary>
        {section.claim_ids.length === 0 ? <p className="hint">No supported claim established for this area.</p> : <ul>{section.claim_ids.map((id) => {
          const claim = overview.claims.find((item) => item.claim_id === id)
          if (!claim) return null
          return <li key={id}><p>{claim.kind === 'inference' && <strong>Inferred from source · </strong>}{claim.text}</p>{claim.assumption && <p className="hint">Inference limit: {claim.assumption}</p>}<div className="citation-row">{claim.evidence_ids.map((sourceID) =>
            <button type="button" className="citation" key={sourceID} onClick={() => openEvidence(sourceID)}>Open source {sourceID.slice(0, 8)}</button>)}
            <button type="button" className="link-button" onClick={() => { setNoteClaim(id); setNoteText('') }}>Add correction</button></div>
            {overview.notes.filter((note) => note.claim_id === id && !note.orphaned).map((note) => <p className="review-note" key={note.id}>Local correction: {note.note} <button type="button" className="link-button" onClick={() => deleteNote(note.id)}>Remove</button></p>)}
          </li>
        })}</ul>}
      </details>)}
      {noteClaim && <div className="note-editor"><label>Correction for claim {noteClaim.slice(0, 8)}<textarea maxLength={2000} value={noteText} onChange={(event) => setNoteText(event.target.value)} placeholder="Describe what should be checked or corrected" /></label>
        <button type="button" className="secondary" disabled={!noteText.trim()} onClick={saveNote}>Save local correction</button>
        <button type="button" className="link-button" onClick={() => setNoteClaim('')}>Cancel</button></div>}
      {overview.notes.some((note) => note.orphaned) && <p className="hint">{overview.notes.filter((note) => note.orphaned).length} saved corrections refer to claims changed by regeneration; they remain in the record.</p>}
      <h3>Analysis limits</h3><ul>{overview.limitations.map((item) => <li key={item.code + item.message}>{item.message}</li>)}</ul>
      <div className="overview-actions"><button type="button" className="secondary" onClick={() => loadInventory(0)}>Browse full inventory</button>
        <button type="button" className="primary" onClick={downloadOKF}>Download OKF bundle</button></div>
      {inventoryItems.length > 0 && <div className="inventory-page"><h3>Inventory {inventoryOffset + 1}–{inventoryOffset + inventoryItems.length}</h3>
        <ul>{inventoryItems.map((item) => <li key={item.path}><code>{item.path}</code> · {item.status} · {item.category}{item.reason_codes.length > 0 ? ` · ${item.reason_codes.join(', ')}` : ''}</li>)}</ul>
        <div className="pagination"><button type="button" className="secondary" disabled={inventoryOffset === 0} onClick={() => loadInventory(Math.max(0, inventoryOffset - 100))}>Previous</button>
          <button type="button" className="secondary" disabled={inventoryOffset + inventoryItems.length >= overview.inventory.total} onClick={() => loadInventory(inventoryOffset + 100)}>Next</button></div></div>}
    </section>}
    {active && <section className="panel result"><h2>Snapshot evidence</h2>
      <p className="hint">{active.url.replace(/\.git$/, '')} · {active.snapshot_id}{active.commit_oid ? ` · ${active.commit_oid}` : ''}</p>
      {coverage && <><h3>Inventory coverage</h3><p>{coverage.inventory_count} artifacts accounted for</p>
        <ul>{Object.entries(coverage.status_counts).map(([status, count]) => <li key={status}>{status}: {count}</li>)}</ul>
        <p className="hint">Analyzed means text indexed, not that runtime behavior has been proved. Go syntax is extracted where parsing succeeds; other languages have text search only.</p>
        {coverage.status_counts.pending > 0 && <button type="button" className="secondary" onClick={reindex}>Index this snapshot</button>}</>}
      {session && session.embedding_providers.length > 0 && <div className="embedding-controls">
        <label>Semantic provider<select value={embeddingProvider} onChange={(event) => setEmbeddingProvider(event.target.value)}>
          {session.embedding_providers.map((provider) => <option key={provider} value={provider}>{provider === 'cloud' ? 'OpenAI text-embedding-3-small' : 'Configured local provider'}</option>)}
        </select></label>
        <button type="button" className="secondary" onClick={embedSnapshot}>Index semantic vectors</button>
        <p className="hint">This sends approved source chunks to the selected provider. Cloud indexing requires this repository’s explicit cloud opt-in.</p>
      </div>}
      <h3>Search source</h3>
      <form className="inline-form" onSubmit={search}><input aria-label="Search source" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Symbol, phrase, or file path" />
        <select aria-label="Search mode" value={searchMode} onChange={(event) => setSearchMode(event.target.value as 'lexical' | 'semantic')}>
          <option value="lexical">Exact and lexical</option>
          {session && session.embedding_providers.length > 0 && <option value="semantic">Semantic</option>}
        </select><button className="primary" disabled={!query.trim()}>Search</button></form>
      {searched && <p className="hint">{results.length} matching chunks. Results are scoped to this snapshot.</p>}
      {results.map((item) => <button type="button" className="search-hit" key={item.evidence_id} onClick={() => openEvidence(item.evidence_id)}>
        <strong>{item.path}:{item.start_line}-{item.end_line}</strong><span>{item.language} · {item.snippet.slice(0, 240)}</span>
      </button>)}
      {evidence && <div className="source-view" id="source-view"><h3>{evidence.source_ranges[0].artifact_id}:{evidence.source_ranges[0].start_line}-{evidence.source_ranges[0].end_line}</h3>
        <pre><code>{evidence.snippet.split('\n').map((line, index) => `${evidence.source_ranges[0].start_line + index}  ${line}`).join('\n')}</code></pre>
        <p className="hint">Evidence ID: {evidence.evidence_id}</p></div>}
      <h3>Symbols and relationships</h3>
      <form className="inline-form" onSubmit={findSymbols}><input aria-label="Find symbols" value={symbolQuery} onChange={(event) => setSymbolQuery(event.target.value)} placeholder="Function or type name" /><button className="secondary">Find symbols</button></form>
      {symbols.map((symbol) => <button type="button" className="search-hit" key={symbol.symbol_id} onClick={() => explore(symbol)}>
        <strong>{symbol.name} · {symbol.kind}</strong><span>{symbol.path}:{symbol.start_line}</span>
      </button>)}
      {graph && <div className="source-view"><h3>Bounded relation graph</h3>
        <p>{graph.nodes.length} symbols, {graph.edges.length} relationships{graph.truncated ? ' · truncated at traversal limit' : ''}</p>
        <ul>{graph.edges.map((edge) => <li key={edge.from_id + edge.to_id + edge.line}>{graph.nodes.find((node) => node.symbol_id === edge.from_id)?.name ?? edge.from_id} → {graph.nodes.find((node) => node.symbol_id === edge.to_id)?.name ?? edge.to_id} · {edge.kind} · {edge.path}:{edge.line}</li>)}</ul>
        <p className="hint">Go direct calls and verified SCIP references are shown. Missing links do not prove a relationship is absent.</p></div>}
      <h3>Import a SCIP index</h3>
      <p className="hint">Optional. Documents are accepted only when their embedded text exactly matches this snapshot. The index is processed locally and no source is sent to a model.</p>
      <div className="embedding-controls"><input aria-label="SCIP index file" type="file" accept=".scip,application/octet-stream" onChange={(event) => setScipFile(event.target.files?.[0] ?? null)} />
        <button type="button" className="secondary" disabled={!scipFile} onClick={uploadSCIP}>Import SCIP</button></div>
      {scipReport && <p className="hint">Imported {scipReport.imported_documents} documents, skipped {scipReport.skipped_documents}; {scipReport.symbols} symbols and {scipReport.relations} relations.</p>}
    </section>}
    <footer>Explanations are scoped to the captured commit. Open citations to inspect the implementation.</footer>
  </main>
}
