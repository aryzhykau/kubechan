import { useState, useCallback, useEffect, useRef } from 'react'
import { IncidentList } from './IncidentList'
import { KubeChanSidebar, type KubeChanState } from './KubeChanSidebar'
import { DiagnosticsPage } from './DiagnosticsPage'
import { DiagnosticRunDetail } from './DiagnosticRunDetail'
import { pickChatterLine, type ChatterEvent } from './chatter'
import { useWebSocket, type WSEvent } from './useWebSocket'
import { api } from './api'
import type { AnalysisResult } from './api'
import './app.css'

type View =
  | { type: 'incidents' }
  | { type: 'diagnostics' }
  | { type: 'run-detail'; runId: string }

function App() {
  const [view, setView] = useState<View>({ type: 'incidents' })
  const [kubechan, setKubechan] = useState<KubeChanState>({ pose: 'idle' })
  const kubechanRef = useRef(kubechan)
  useEffect(() => { kubechanRef.current = kubechan }, [kubechan])

  const [moodLevel, setMoodLevel] = useState(0)
  const moodLevelRef = useRef(0)
  useEffect(() => { moodLevelRef.current = moodLevel }, [moodLevel])

  // Each chatter message gets its own timer; we cancel the previous one so stale
  // timers never wipe a newer message.
  const chatterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Poke escalation: count rapid pokes, reset after 8s of no poking.
  const pokeCountRef = useRef(0)
  const pokeResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Silence awareness: track last user interaction time.
  const lastInteractionRef = useRef(Date.now())
  const silenceStageRef = useRef<0 | 1 | 2>(0) // 0=normal, 1=hint sent, 2=paranoid sent

  const triggerChatter = useCallback((event: ChatterEvent) => {
    const pose = kubechanRef.current.pose
    // Idle lines only fire when genuinely idle — they must never interrupt reactions.
    if (event === 'idle' || event === 'silence-hint' || event === 'silence-paranoid') {
      if (pose !== 'idle') return
    } else {
      // Reaction lines: skip only if KubeChan is actively thinking/speaking.
      if (pose === 'thinking' || pose === 'speaking') return
      // Any real interaction resets silence tracking.
      lastInteractionRef.current = Date.now()
      silenceStageRef.current = 0
    }
    const line = pickChatterLine(event, moodLevelRef.current)
    // Cancel any in-flight dismiss timer before starting a new one.
    if (chatterTimerRef.current !== null) {
      clearTimeout(chatterTimerRef.current)
    }
    setKubechan({ pose: 'chatter', chatterLine: line })
    chatterTimerRef.current = setTimeout(() => {
      chatterTimerRef.current = null
      setKubechan(prev => prev.pose === 'chatter' ? { pose: 'idle' } : prev)
    }, 9000)
  }, [])

  const handlePoke = useCallback(() => {
    // Escalate based on rapid consecutive pokes — local counter for snappy UX.
    pokeCountRef.current += 1
    const count = pokeCountRef.current
    const event: ChatterEvent =
      count >= 5 ? 'poke-rage' :
      count >= 3 ? 'poke-annoyed' :
      'poke'
    triggerChatter(event)
    // Reset the poke counter after 8s of no poking.
    if (pokeResetTimerRef.current !== null) clearTimeout(pokeResetTimerRef.current)
    pokeResetTimerRef.current = setTimeout(() => {
      pokeCountRef.current = 0
    }, 8000)
    // Persist poke to backend (fire-and-forget).
    api.poke().catch(() => {})
  }, [triggerChatter])

  // idle chatter — fires every 60s when KubeChan is idle
  useEffect(() => {
    const id = setInterval(() => {
      if (kubechanRef.current.pose === 'idle') triggerChatter('idle')
    }, 60_000)
    return () => clearInterval(id)
  }, [triggerChatter])

  // Silence awareness — check every 30s; escalate at 5min then 10min of inactivity.
  useEffect(() => {
    const id = setInterval(() => {
      if (kubechanRef.current.pose !== 'idle') return
      const idleMs = Date.now() - lastInteractionRef.current
      if (idleMs >= 10 * 60_000 && silenceStageRef.current < 2) {
        silenceStageRef.current = 2
        triggerChatter('silence-paranoid')
      } else if (idleMs >= 5 * 60_000 && silenceStageRef.current < 1) {
        silenceStageRef.current = 1
        triggerChatter('silence-hint')
      }
    }, 30_000)
    return () => clearInterval(id)
  }, [triggerChatter])

  // WS: react to new/resolved incidents at the app level
  const handleWS = useCallback((event: WSEvent) => {
    if (event.type === 'Incident.Created') {
      triggerChatter('new-incident')
    } else if (event.type === 'Incident.Resolved') {
      triggerChatter('incident-resolved')
    } else if (event.type === 'KubeChanState.Updated') {
      const e = event as { type: string; moodLevel?: number }
      if (typeof e.moodLevel === 'number') setMoodLevel(e.moodLevel)
    }
    // KubeChanState.Updated — mood changed in cluster; no local action needed
    // (the CRD is the source of truth; local poke counter drives UX reactions)
  }, [triggerChatter])
  useWebSocket(handleWS)

  // Load KubeChanState from backend on mount.
  useEffect(() => {
    api.getKubeChanState().then(s => setMoodLevel(s.moodLevel)).catch(() => {})
  }, [])

  const handleAnalysisStart = useCallback((incidentName: string) => {
    setKubechan({ pose: 'thinking', incidentName })
  }, [])

  const handleAnalysisComplete = useCallback((result: AnalysisResult, incidentName: string) => {
    setKubechan({ pose: 'speaking', incidentName, result })
  }, [])

  const handleRunResultLoaded = useCallback((result: AnalysisResult | null, runId: string) => {
    if (result) {
      setKubechan({ pose: 'speaking', incidentName: result.incidentId || runId, result })
    } else {
      setKubechan({ pose: 'idle' })
    }
  }, [])

  const reactionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const showReaction = useCallback((line: string) => {
    setKubechan(prev => ({ ...prev, reactionLine: line }))
    if (reactionTimerRef.current !== null) clearTimeout(reactionTimerRef.current)
    reactionTimerRef.current = setTimeout(() => {
      reactionTimerRef.current = null
      setKubechan(prev => prev.reactionLine === line ? { ...prev, reactionLine: undefined } : prev)
    }, 4500)
  }, [])

  const handleRate = useCallback(async (resultId: string, rating: 'up' | 'down', confidence: number) => {
    try {
      await api.rateAnalysisResult(resultId, rating)
    } catch {
      // fire-and-forget; rating failure doesn't break UX
    }
    // Pick reaction line based on rating + confidence + mood.
    const event: ChatterEvent = rating === 'up'
      ? (confidence >= 0.75 ? 'rating-up-flustered' : 'rating-up')
      : (confidence >= 0.75 ? 'rating-down-high-conf' : 'rating-down-low-conf')
    const line = pickChatterLine(event, moodLevelRef.current)

    // Update result rating + inject reaction line — keep pose as 'speaking'.
    setKubechan(prev => {
      if (prev.pose !== 'speaking' || !prev.result || prev.result.id !== resultId) return prev
      return { ...prev, result: { ...prev.result, userRating: rating } }
    })
    showReaction(line)
  }, [moodLevelRef, showReaction])

  return (
    <div className="app">
      <header className="app-header">
        <span className="app-logo">⎈</span>
        <h1>KubeChan</h1>
        <nav className="app-nav">
          <button
            className={`app-nav-btn${view.type === 'incidents' ? ' active' : ''}`}
            onClick={() => { setView({ type: 'incidents' }); triggerChatter('nav-incidents') }}
          >
            Incidents
          </button>
          <button
            className={`app-nav-btn${view.type === 'diagnostics' || view.type === 'run-detail' ? ' active' : ''}`}
            onClick={() => {
              setView({ type: 'diagnostics' })
              if (kubechanRef.current.pose === 'speaking') {
                const line = pickChatterLine('dismissed-analysis', moodLevelRef.current)
                setKubechan({ pose: 'chatter', chatterLine: line })
                if (chatterTimerRef.current !== null) clearTimeout(chatterTimerRef.current)
                chatterTimerRef.current = setTimeout(() => {
                  chatterTimerRef.current = null
                  setKubechan(prev => prev.pose === 'chatter' ? { pose: 'idle' } : prev)
                }, 9000)
              } else {
                triggerChatter('nav-diagnostics')
              }
            }}
          >
            Diagnostics
          </button>
        </nav>
        <span className="app-subtitle">Kubernetes Anime Problem Insluter</span>
      </header>
      <div className="app-body">
        <main className="app-main">
          {view.type === 'incidents' && (
            <IncidentList
              onAnalysisStart={handleAnalysisStart}
              onAnalysisComplete={handleAnalysisComplete}
              onAction={triggerChatter}
            />
          )}
          {view.type === 'diagnostics' && (
            <DiagnosticsPage
              onSelectRun={runId => setView({ type: 'run-detail', runId })}
              onAction={triggerChatter}
            />
          )}
          {view.type === 'run-detail' && (
            <DiagnosticRunDetail
              runId={view.runId}
              onBack={() => { setView({ type: 'diagnostics' }); setKubechan({ pose: 'idle' }) }}
              onResultLoaded={handleRunResultLoaded}
              onAction={triggerChatter}
            />
          )}
        </main>
        <KubeChanSidebar state={kubechan} onPoke={handlePoke} moodLevel={moodLevel} onRate={handleRate} />
      </div>
    </div>
  )
}

export default App
