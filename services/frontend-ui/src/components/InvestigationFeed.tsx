import type { AnalysisProgress } from '../api/index'
import { useEffect, useRef, useState } from 'react'
import { analysisActivityEvents, analysisEventLabel } from '../analysis/progress'
import './InvestigationFeed.css'

export function InvestigationFeed({ progress }: { progress?: AnalysisProgress }) {
  const root = useRef<HTMLElement>(null)
  const [follow, setFollow] = useState(true)
  useEffect(() => {
    const container = root.current?.closest('.kubechan-scroll-zone')
    if (follow && container) container.scrollTop = container.scrollHeight
  }, [progress?.lastSeq, follow])
  if (!progress?.available) return null
  const events = analysisActivityEvents(progress).filter(event => !['context_delivered', 'model_request', 'indexing'].includes(event.stage))
  return <section ref={root} className="investigation-feed" aria-label="KubeChan investigation journal">
    <h3>KubeChan · investigation journal</h3>
    <p>{progress.status === 'completed' || progress.status === 'partial' ? 'Checks and findings recorded for the finalized assessment.' : progress.status === 'running' ? 'Findings are provisional until the assessment is finalized.' : 'This investigation stopped; its accepted draft findings are retained.'}</p>
    <button type="button" aria-pressed={follow} onClick={() => setFollow(value => !value)}>{follow ? 'Pause following' : 'Follow latest'}</button>
    <ol>{events.map(event => <li key={`${event.attemptId}-${event.seq}`} className={event.change ? 'investigation-feed__finding' : ''}>
      <time dateTime={event.recordedAt}>{new Date(event.recordedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}</time>
      <span>{analysisEventLabel(event)}</span>
      {event.change?.kind === 'review' && <button type="button" onClick={() => window.dispatchEvent(new CustomEvent('kubechan:select-investigation-node', { detail: event.change!.id }))}>Show resource</button>}
    </li>)}</ol>
  </section>
}
