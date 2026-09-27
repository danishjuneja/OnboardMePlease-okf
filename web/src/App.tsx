import { FormEvent, useEffect, useState } from 'react'
import KnowledgeChat from './KnowledgeChat'

type Session = { generation_available: boolean; generation_provider: string; generation_model: string; csrf_token: string; model_mode: 'strict_local' | 'cloud_opt_in'; embedding_providers: string[]; phase: number }
type Queued = { repository_id: string; snapshot_id: string; job_id: string; state: string }
type Job = { failure?: string; job_id: number; kind: string; state: string }
type Coverage = { snapshot_id: string; inventory_count: number; status_counts: Record<string, number> }
type Repository = { repository_id: string; url: string; snapshot_id: string; state: string; commit_oid: string | null }
type SearchResult = { evidence_id: string; path: string; start_line: number; end_line: number; language: string; snippet: string }
type Evidence = { evidence_id: string; source_ranges: { artifact_id: string; start_line: number; end_line: number }[]; snippet: string }
type OverviewClaim = { assumption?: string; claim_id: string; text: string; kind: string; evidence_ids: string[]; support_status: string }
type OverviewSection = { kind: string; title: string; claim_ids: string[] }
type Overview = { snapshot_id: string; commit_oid: string; state: 'ready' | 'partial' | 'stale' | 'evidence_only'; synthesis?: { reviewed_chunks: number; total_chunks: number; reviewed_units: number; total_units: number; model: string }; concepts?: OverviewSection[]; version: number; generated_at: string; sections: OverviewSection[]; claims: OverviewClaim[];
  limitations: { code: string; message: string }[]; inventory: { total: number; statuses: Record<string, number>; categories: Record<string, number>; unclassified: number }; notes: { claim_id: string; note: string }[] }

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
  const [results, setResults] = useState<SearchResult[]>([])
  const [searched, setSearched] = useState(false)
  const [evidence, setEvidence] = useState<Evidence | null>(null)
  const [overview, setOverview] = useState<Overview | null>(null)
  const [error, setError] = useState('')

  async function refreshRepositories() {
    const response = await fetch('/v1/repositories', { credentials: 'same-origin' })
    const data = await json<{ repositories: Repository[] }>(response)
    setRepositories(data.repositories)
  }

  async function selectRepository(repository: Repository) {
    setActive(repository); setCoverage(null); setOverview(null); setResults([]); setSearched(false); setEvidence(null); setQueued(null); setError('')
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
      .then((value) => { setSession(value); return refreshRepositories() })
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
      setQueued(null)
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
    events.onerror = () => { events.close(); setQueued(null); setError('Progress stream disconnected; reopen the snapshot from Recent repositories') }
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



  function prefix() { return `/v1/repositories/${active?.repository_id}/snapshots/${active?.snapshot_id}` }

  async function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!active) return
    setError(''); setEvidence(null); setSearched(false)
    try {
      const params = new URLSearchParams({ q: query.trim(), mode: 'lexical' })
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








  return <main className="shell">
    <header><span className="brand">OnboardMePlease</span></header>
    <section className="panel"><h1>Explore a repository</h1>
      <form onSubmit={submit}>
        <label>Public GitHub URL<input required type="url" value={url} onChange={e => setUrl(e.target.value)} placeholder="https://github.com/owner/repo" /></label>
        <details><summary>Capture options</summary>
          <label>Branch or tag<input value={ref} onChange={e => setRef(e.target.value)} /></label>
          {session?.model_mode === 'cloud_opt_in' && <label>Model policy<select value={privacy} onChange={e => setPrivacy(e.target.value as typeof privacy)}><option value="strict_local">Local only</option><option value="cloud_opt_in">Allow cloud models</option></select></label>}
        </details>
        <button className="primary" disabled={!session || !url.trim() || !!queued}>Capture</button>
        <p className="hint">Capture indexes source without model calls. Analysis is a separate action.</p>
      </form>
    </section>
    {error && <p className="error" role="alert">{error}</p>}
    {queued && <p role="status">{job?.state ?? queued.state}â€¦</p>}
    <section className="panel"><h2>Repositories</h2>
      {repositories.length === 0 && <p>No snapshots yet.</p>}
      <ul className="repository-list">{repositories.map(item => <li key={item.snapshot_id}><button className="link-button" onClick={() => selectRepository(item)}>{item.url}</button><span className="hint">{item.state} Â· {item.commit_oid?.slice(0, 12)}</span></li>)}</ul>
    </section>
    {active?.state === 'ready' && <>
      <section className="panel"><h2>Overview</h2>
        {coverage && <p className="hint">{coverage.inventory_count} files in this snapshot.</p>}
        {overview && <>
          <p className="hint">{overview.state}{overview.synthesis ? ` Â· ${overview.synthesis.reviewed_chunks}/${overview.synthesis.total_chunks} source chunks reviewed` : ' Â· Source indexed; model analysis has not run.'}</p>
          {overview.sections.map(section => <div className="overview-section" key={section.kind}><h3>{section.title}</h3>{section.claim_ids.map(id => {
            const claim = overview.claims.find(c => c.claim_id === id)
            return claim && <div key={id}><p>{claim.text}</p>{claim.assumption && <p className="hint">{claim.assumption}</p>}<div className="citation-row">{claim.evidence_ids.map(eid => <button className="citation" key={eid} onClick={() => openEvidence(eid)}>Source {eid.slice(0, 8)}</button>)}</div></div>
          })}</div>)}
          {overview.limitations.length > 0 && <details><summary>Coverage and limitations</summary><ul>{overview.limitations.map((item, i) => <li key={i}>{item.message}</li>)}</ul></details>}
        </>}
      </section>
      {session && <KnowledgeChat running={!!queued} key={prefix()} base={prefix()} session={session} onQueued={setQueued} onEvidence={openEvidence} />}
      <section className="panel"><details><summary>Search source</summary>
        <form className="inline-form" onSubmit={search}><input aria-label="Search source" value={query} onChange={e => setQuery(e.target.value)} placeholder="Symbol, phrase, or path" /><button className="primary" disabled={!query.trim()}>Search</button></form>
        {searched && <p className="hint">{results.length} matching chunks.</p>}
        {results.map(item => <button className="search-hit" key={item.evidence_id} onClick={() => openEvidence(item.evidence_id)}><strong>{item.path}:{item.start_line}â€“{item.end_line}</strong><span>{item.snippet.slice(0, 240)}</span></button>)}
      </details>
      {evidence && <div className="source-view" id="source-view"><h3>{evidence.source_ranges[0].artifact_id}</h3><pre><code>{evidence.snippet.split('\n').map((line, i) => `${evidence.source_ranges[0].start_line + i}  ${line}`).join('\n')}</code></pre></div>}
      </section>
    </>}
  </main>
}
