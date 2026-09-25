import { FormEvent, useEffect, useRef, useState } from 'react'

type Answer = {
 id: string; question: string; claims: { text: string; evidence_ids: string[]; assumption: string }[]
 sources: { evidence_id: string; path: string; start_line: number; end_line: number }[]
 concepts: { id: string; title: string; markdown: string }[]; gaps: string[]; retrieval: string
 diagram: { from: string; to: string; kind: string; evidence_id: string }[]
}
type Props = {
 base: string; session: { csrf_token: string; generation_available: boolean; generation_provider: string; generation_model: string }
 onQueued: (job: { repository_id: string; snapshot_id: string; job_id: string; state: string }) => void
 onEvidence: (id: string) => void
}

export default function KnowledgeChat({ base, session, onQueued, onEvidence }: Props) {
 const [answers, setAnswers] = useState<Answer[]>([])
 const [question, setQuestion] = useState('')
 const [parent, setParent] = useState('')
 const [cloudConsent, setCloudConsent] = useState(false)
 const [busy, setBusy] = useState(false)
 const [error, setError] = useState('')
 const current = useRef(base)
 current.current = base
 useEffect(() => {
  const abort = new AbortController()
  setAnswers([]); setParent(''); setQuestion(''); setError(''); setBusy(false); setCloudConsent(false)
  fetch(`${base}/questions`, { signal: abort.signal }).then(async r => {
   if (!r.ok) throw new Error('Conversation unavailable')
   const data = await r.json()
   if (!abort.signal.aborted) { setAnswers(data.answers); setParent(data.answers.at(-1)?.id ?? '') }
  }).catch(e => { if (!abort.signal.aborted) setError(e.message) })
  return () => abort.abort()
 }, [base])

 async function analyze() {
  setError(''); setBusy(true)
  try {
   const response = await fetch(`${base}/analysis`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token }, body: JSON.stringify({ cloud_opt_in: cloudConsent }) })
   const value = await response.json(); if (!response.ok) throw new Error(value.message)
   if (current.current === base) onQueued(value)
  } catch (e) { if (current.current === base) setError(e instanceof Error ? e.message : 'Analysis unavailable') }
  finally { if (current.current === base) setBusy(false) }
 }

 async function ask(event: FormEvent) {
  event.preventDefault(); setError(''); setBusy(true)
  try {
   const response = await fetch(`${base}/questions`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token }, body: JSON.stringify({ question, parent_id: parent }) })
   const value = await response.json(); if (!response.ok) throw new Error(value.message)
   if (current.current === base) { setAnswers(previous => [...previous, value]); setParent(value.id); setQuestion('') }
  } catch (e) { if (current.current === base) setError(e instanceof Error ? e.message : 'Answer unavailable') }
  finally { if (current.current === base) setBusy(false) }
 }

 return <section className="panel">
  <h2>Understand this repository</h2>
  {session.generation_available ? <>
   <p className="hint">Generation: {session.generation_provider} · {session.generation_model}. Each run reviews up to 8 source batches; repeat to resume large repositories.</p>
   {session.generation_provider === 'cloud' && <label className="consent"><input type="checkbox" checked={cloudConsent} onChange={e => setCloudConsent(e.target.checked)} />Allow cloud analysis and questions for this repository. Approved source and questions will be sent to OpenAI.</label>}
   <button className="secondary" disabled={busy || (session.generation_provider === 'cloud' && !cloudConsent)} onClick={analyze}>Analyze or resume overview</button>
   <p className="hint">Source and OKF concepts are searched together. Every answer reopens source and checks its claims. Static analysis can leave runtime behavior unresolved.</p>
   <form onSubmit={ask}>
    <label>Technical question<textarea required maxLength={2000} value={question} onChange={e => setQuestion(e.target.value)} placeholder="What happens when a customer cancels after payment?" /></label>
    <div className="overview-actions"><button className="primary" disabled={busy || !question.trim()}>Ask</button>
    {parent && <button type="button" className="link-button" onClick={() => setParent('')}>Start a new question</button>}</div>
    <p className="hint">{busy ? 'Reading and checking source…' : parent ? 'Following up on the previous answer in this snapshot.' : 'New question in this snapshot.'}</p>
   </form>
  </> : <p>Evidence search is available. Configure a generation provider and model on the server to create an overview and ask technical questions.</p>}
  {error && <p className="error" role="alert">{error}</p>}
  <div aria-live="polite">{answers.map(answer => <article key={answer.id} className="answer">
   <h3>{answer.question}</h3><p className="hint">{answer.retrieval}</p>
   {answer.claims.map((claim, i) => <div key={i}><p>{claim.text}</p>
    {claim.assumption && <p className="hint">Inference limit: {claim.assumption}</p>}
    <div className="citation-row">{claim.evidence_ids.map(id => { const source = answer.sources.find(s => s.evidence_id === id); return <button key={id} className="citation" onClick={() => onEvidence(id)}>{source ? `${source.path}:${source.start_line}–${source.end_line}` : id.slice(0, 8)}</button> })}</div>
   </div>)}
   {answer.diagram.length > 0 && <details><summary>Static source relationships</summary><ul>{answer.diagram.map((edge, i) => <li key={i}><code>{edge.from}</code> → <code>{edge.to}</code> · {edge.kind} <button className="citation" onClick={() => onEvidence(edge.evidence_id)}>Source</button></li>)}</ul></details>}
   {answer.concepts.length > 0 && <details><summary>OKF knowledge used ({answer.concepts.length})</summary>{answer.concepts.map(concept => <details key={concept.id}><summary>{concept.title}</summary><pre>{concept.markdown}</pre></details>)}</details>}
   {answer.gaps.length > 0 && <details open><summary>Evidence gaps and limits</summary><ul>{answer.gaps.map((gap, i) => <li key={i}>{gap}</li>)}</ul></details>}
   <button className="link-button" onClick={() => setParent(answer.id)}>Follow up on this answer</button>
  </article>)}</div>
 </section>
}
