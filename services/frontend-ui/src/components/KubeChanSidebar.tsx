import { useRef, useEffect, useState } from 'react'
import { useMatch, useNavigate } from 'react-router-dom'
import { Button, CircularProgress } from '@mui/material'
import ArrowForwardIcon from '@mui/icons-material/ArrowForward'
import { useListIncidentsQuery } from '../store/api/incidentsApi'
import { useListDiagnosticRunsQuery } from '../store/api/diagnosticsApi'
import type { AnalysisResult } from '../api/index'
import { useAnalysisProgress } from '../analysis/useAnalysisProgress'
import { analysisProgressLabel } from '../analysis/progress'
import { InvestigationFeed } from './InvestigationFeed'
import { assessmentPath } from '../analysis/investigation'

const THINKING_FRAMES: { image: string; phrase: string }[] = [
  { image: '/kubechan-looking.png',    phrase: "H-hmph… give me a second, I'm scanning your dumb cluster…" },
  { image: '/kubechan-looking-2.png',  phrase: "Something's not adding up here… let me look more carefully." },
  { image: '/kubechan-looking-3.png',  phrase: "Wait — that doesn't look right. Hold on." },
  { image: '/kubechan-pray-calm.png',  phrase: "I'm not doing this because I care! I'm just… running diagnostics…" },
  { image: '/kubechan-rolleye.png',    phrase: "This better not be something obvious you could've googled yourself…" },
  { image: '/kubechan-sigh-angry.png', phrase: "Ugh, another disaster to clean up. Give me a moment." },
  { image: '/kubechan-sigh-calm.png',  phrase: "Fine. I'm looking into it. You're welcome in advance." },
  { image: '/kubechan-tired-1.png',    phrase: "Y-you're lucky I'm even helping… scanning now…" },
]

export type KubeChanPose = 'idle' | 'thinking' | 'speaking' | 'chatter'

export interface KubeChanState {
  pose: KubeChanPose
  incidentName?: string
  result?: AnalysisResult
  chatterLine?: string
  chatterImage?: string
  reactionLine?: string
}

export function KubeChanSidebar({ state, onPoke, moodLevel = 0, onDismiss }: {
  state: KubeChanState
  onPoke?: () => void
  moodLevel?: number
  onDismiss?: () => void
}) {
  const navigate = useNavigate()
  const { data: incidents = [], isLoading: incidentsLoading, isError: incidentsError } = useListIncidentsQuery(undefined, { pollingInterval: 15000, refetchOnMountOrArgChange: true })
  const { data: runs = [], isLoading: runsLoading, isError: runsError } = useListDiagnosticRunsQuery(undefined, { pollingInterval: 10000, refetchOnMountOrArgChange: true })
  const activeRun = runs.find(run => run.status === 'in_progress')
  const assessmentRoute = useMatch('/incidents/:incidentId/assessments/:runId')
  const { data: agentProgress } = useAnalysisProgress(assessmentRoute?.params.runId ?? activeRun?.diagnosticRunId ?? '')
  const { pose: storedPose, result, incidentName, reactionLine, chatterImage } = state
  const pose = assessmentRoute ? (!agentProgress?.available || agentProgress.status === 'running' ? 'thinking' : 'idle') : storedPose
  const contentRef = useRef<HTMLDivElement>(null)
  const [shaking, setShaking] = useState(false)
  const [thinkingFrameIdx, setThinkingFrameIdx] = useState(0)
  const [imgVisible, setImgVisible] = useState(true)

  useEffect(() => {
    if (pose !== 'thinking') {
      setImgVisible(true)
      return
    }
    setThinkingFrameIdx(Math.floor(Math.random() * THINKING_FRAMES.length))

    let swapTimer: ReturnType<typeof setTimeout> | null = null
    const interval = setInterval(() => {
      setImgVisible(false)
      swapTimer = setTimeout(() => {
        setThinkingFrameIdx(prev => {
          const others = THINKING_FRAMES.map((_, i) => i).filter(i => i !== prev)
          return others[Math.floor(Math.random() * others.length)]
        })
        setImgVisible(true)
      }, 200)
    }, 2500)

    return () => {
      clearInterval(interval)
      if (swapTimer) clearTimeout(swapTimer)
    }
  }, [pose])

  const hasFalseAlarm = pose === 'speaking' && !!result?.payload?.suggestExclusionRule
  const [falseAlarmImg] = useState(() =>
    Math.random() < 0.5 ? '/kubechan-facepalm-angry.png' : '/kubechan-facepalm-calm.png'
  )
  const imgSrc = pose === 'thinking'
    ? THINKING_FRAMES[thinkingFrameIdx].image
    : pose === 'chatter' && chatterImage
      ? chatterImage
      : hasFalseAlarm
        ? falseAlarmImg
        : '/kubechan-idle-1.png'

  useEffect(() => {
    if (contentRef.current) contentRef.current.scrollTop = 0
  }, [result])

  function handlePoke() {
    setShaking(true)
    setTimeout(() => setShaking(false), 500)
    onPoke?.()
  }

  const openingRant   = result?.payload?.openingRant   || ''
  const closingInsult = result?.payload?.closingInsult || ''
  const openIncidents = incidents.filter(item => item.status.state === 'open')
  const terminalStates = ['completed', 'failed', 'error']
  const activeRuns = runs.filter(run => !run.analysisResultId && !terminalStates.includes(run.status.toLowerCase()))
  const recentRuns = runs.filter(run => !!run.analysisResultId || terminalStates.includes(run.status.toLowerCase())).slice(0, 3)

  const elapsed = (value: string) => {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
    if (!Number.isFinite(seconds)) return 'unknown time'
    if (seconds < 60) return `${seconds}s`
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  }
  const runStatus = (value: string) => ({ pending: 'Waiting for evidence', dispatched: 'Waiting for evidence', in_progress: 'Analyzing', queued: 'Queued', running: 'Running', collecting: 'Collecting evidence', analyzing: 'Analyzing' }[value.toLowerCase()] ?? (value || 'In progress'))

  return (
    <aside className="kubechan-sidebar">
      {/* scrollable top zone */}
      <div className="kubechan-scroll-zone">
        {!assessmentRoute && <div className="assistant-overview">
          <div className="assistant-overview-counts">
            <button type="button" onClick={() => navigate('/')}><strong>{openIncidents.length}</strong><span>Open incidents</span></button>
            <div><strong>{activeRuns.length}</strong><span>Active analyses</span></div>
          </div>
          {(incidentsLoading || runsLoading) && <div className="assistant-loading"><CircularProgress size={13} /> Refreshing overview…</div>}
          {(incidentsError || runsError) && <p className="assistant-data-error">Could not refresh all data. Open a section to try again.</p>}
          <section className="assistant-list-section">
            <h3>Active analyses</h3>
            {activeRuns.length === 0 ? <p className="assistant-empty">No analyses running.</p> : activeRuns.slice(0, 5).map(run => {
              const incident = incidents.find(item => item.metadata.name === run.incidentId)
              const resource = incident?.spec.rootResource
              const title = resource ? `${resource.kind}/${resource.name}` : run.incidentId || 'Incident'
              return <article className="assistant-run" key={run.diagnosticRunId}>
                <strong title={title}>{title}</strong>
                <span>{incident?.spec.rootResource.namespace ?? incident?.metadata.namespace ?? 'Unknown namespace'} · {runStatus(run.status)}</span>
                <span>Started {elapsed(run.requestedAt)} ago</span>
                {agentProgress?.runId === run.diagnosticRunId && agentProgress.available && <span role="status" aria-live="polite">{analysisProgressLabel(agentProgress)}</span>}
                <Button size="small" endIcon={<ArrowForwardIcon />} onClick={() => navigate(assessmentPath(run.incidentId, run.diagnosticRunId))}>Open analysis</Button>
              </article>
            })}
            {activeRuns.length > 5 && <button className="assistant-more" onClick={() => navigate('/diagnostics')}>View all runs ({activeRuns.length - 5} more)</button>}
          </section>
          <section className="assistant-list-section">
            <h3>Recent results</h3>
            {recentRuns.length === 0 ? <p className="assistant-empty">No completed analyses yet.</p> : recentRuns.map(run => {
              const incident = incidents.find(item => item.metadata.name === run.incidentId)
              const isFailed = ['failed', 'error'].includes(run.status.toLowerCase())
              const hasResult = !!run.analysisResultId && !!run.incidentId
              const title = incident ? `${incident.spec.rootResource.kind}/${incident.spec.rootResource.name}` : run.incidentId || 'Incident'
              return <article className="assistant-run assistant-run--recent" key={run.diagnosticRunId}>
                <strong title={title}>{title}</strong>
                <span>{run.agentStatus && run.agentStatus !== 'completed' ? analysisProgressLabel({ available: true, runId: run.diagnosticRunId, stage: run.agentStatus, events: [] }) : isFailed ? 'Analysis failed' : !hasResult ? 'Result unavailable' : run.needsMoreInfo ? 'More evidence needed' : 'Analysis ready'}</span>
                <Button size="small" endIcon={<ArrowForwardIcon />} onClick={() => hasResult ? navigate(`/incidents/${encodeURIComponent(run.incidentId)}/assessments/${encodeURIComponent(run.diagnosticRunId)}`) : navigate(`/diagnostics/${encodeURIComponent(run.diagnosticRunId)}`)}>{hasResult ? 'Open assessment' : 'Open run'}</Button>
              </article>
            })}
          </section>
          {openIncidents.length > 0 && <button className="assistant-more" onClick={() => navigate('/')}>View all open incidents ({openIncidents.length})</button>}
        </div>}
        {assessmentRoute && <InvestigationFeed progress={agentProgress} />}
        {assessmentRoute && <div className="sidebar-bubble-wrap"><div className="speech-bubble"><p className="speech-neutral" role="status">{agentProgress?.available ? analysisProgressLabel(agentProgress) : 'Waiting for the collected evidence to begin the investigation.'}</p></div></div>}
        {!assessmentRoute && pose !== 'idle' && (
          <div className="sidebar-bubble-wrap">
            <div className="speech-bubble">
              {/* scrollable content */}
              <div className="speech-bubble-content" ref={contentRef}>
                {pose === 'thinking' && (
                  <span className="speech-thinking">
                    <span className="spinner" />
                    <span key={thinkingFrameIdx} className="speech-thinking-text">
                      {agentProgress?.available ? analysisProgressLabel(agentProgress) : assessmentRoute ? 'Waiting for the collected evidence to begin the investigation.' : THINKING_FRAMES[thinkingFrameIdx].phrase}
                    </span>
                  </span>
                )}
                {pose === 'chatter' && (
                  <p className="speech-chatter">{state.chatterLine}</p>
                )}
                {pose === 'speaking' && (
                  <>
                    {incidentName && (
                      <p className="speech-incident-label">{incidentName}</p>
                    )}
                    {openingRant
                      ? <p className="speech-opening-rant">{openingRant}</p>
                      : <p className="speech-neutral">Analysis complete. Check the incident card.</p>
                    }
                    {closingInsult && (
                      <p className="speech-closing-insult">{closingInsult}</p>
                    )}
                  </>
                )}
              </div>
              {pose === 'speaking' && (
                <button className="bubble-dismiss" onClick={onDismiss} aria-label="Dismiss">×</button>
              )}
            </div>
          </div>
        )}
      </div>
      {/* fixed character zone — always visible */}
      <div className="kubechan-char-zone">
        {pose === 'idle' && !assessmentRoute && (
          <p className="kubechan-idle-hint">click an incident<br />to ask me for help</p>
        )}
        {reactionLine && (
          <div className="kubechan-reaction-callout" key={reactionLine}>
            <span>{reactionLine}</span>
          </div>
        )}
        <img
          src={imgSrc}
          alt="KubeChan"
          className={[
            'kubechan-sidebar-char',
            shaking ? 'kubechan-shake' : '',
          ].filter(Boolean).join(' ')}
          onClick={handlePoke}
          style={{ cursor: 'pointer', opacity: imgVisible ? 1 : 0, transition: 'opacity 0.2s ease' }}
        />
        <div className={`kubechan-mood mood-${moodLevel}`}>
          {moodLevel === 0 && <span>· calm</span>}
          {moodLevel === 1 && <span>· irritated</span>}
          {moodLevel >= 2 && <span>· RAGE</span>}
        </div>
      </div>
    </aside>
  )
}
