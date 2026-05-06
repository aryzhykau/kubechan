import { useState, useCallback, useEffect, useRef } from 'react'
import { IncidentList } from './IncidentList'
import { KubeChanSidebar, type KubeChanState } from './KubeChanSidebar'
import { DiagnosticsPage } from './DiagnosticsPage'
import { DiagnosticRunDetail } from './DiagnosticRunDetail'
import { ManualIncidentModal } from './ManualIncidentModal'
import { LoginPage } from './LoginPage'
import { UsersPage } from './UsersPage'
import { LLMSettingsPage } from './LLMSettingsPage'
import { pickChatterLine, type ChatterEvent } from './chatter'
import { useWebSocket, type WSEvent } from './useWebSocket'
import { api, getToken, clearToken, type CurrentUser } from './api'
import type { AnalysisResult } from './api'
import './app.css'

type View =
  | { type: 'incidents' }
  | { type: 'diagnostics' }
  | { type: 'run-detail'; runId: string }
  | { type: 'users' }
  | { type: 'llm-settings' }

function App() {
  const [currentUser, setCurrentUser] = useState<CurrentUser | null | undefined>(undefined)
  const [view, setView] = useState<View>({ type: 'incidents' })
  const [kubechan, setKubechan] = useState<KubeChanState>({ pose: 'idle' })
  const kubechanRef = useRef(kubechan)
  useEffect(() => { kubechanRef.current = kubechan }, [kubechan])

  const [showManualModal, setShowManualModal] = useState(false)

  const [moodLevel, setMoodLevel] = useState(0)
  const moodLevelRef = useRef(0)
  useEffect(() => { moodLevelRef.current = moodLevel }, [moodLevel])

  const chatterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pokeCountRef = useRef(0)
  const pokeResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastInteractionRef = useRef(Date.now())
  const silenceStageRef = useRef<0 | 1 | 2>(0)

  // Auth gate: check existing token on mount
  useEffect(() => {
    const token = getToken()
    if (!token) {
      setCurrentUser(null)
      return
    }
    api.me().then(u => setCurrentUser(u)).catch(() => {
      clearToken()
      setCurrentUser(null)
    })
  }, [])

  const triggerChatter = useCallback((event: ChatterEvent) => {
    const pose = kubechanRef.current.pose
    if (event === 'idle' || event === 'silence-hint' || event === 'silence-paranoid') {
      if (pose !== 'idle') return
    } else {
      if (pose === 'thinking' || pose === 'speaking') return
      lastInteractionRef.current = Date.now()
      silenceStageRef.current = 0
    }
    const line = pickChatterLine(event, moodLevelRef.current)
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
    pokeCountRef.current += 1
    const count = pokeCountRef.current
    const event: ChatterEvent =
      count >= 5 ? 'poke-rage' :
      count >= 3 ? 'poke-annoyed' :
      'poke'
    triggerChatter(event)
    if (pokeResetTimerRef.current !== null) clearTimeout(pokeResetTimerRef.current)
    pokeResetTimerRef.current = setTimeout(() => {
      pokeCountRef.current = 0
    }, 8000)
    api.poke().catch(() => {})
  }, [triggerChatter])

  useEffect(() => {
    const id = setInterval(() => {
      if (kubechanRef.current.pose === 'idle') triggerChatter('idle')
    }, 60_000)
    return () => clearInterval(id)
  }, [triggerChatter])

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

  const handleWS = useCallback((event: WSEvent) => {
    if (event.type === 'Incident.Created') {
      triggerChatter('new-incident')
    } else if (event.type === 'KubeChanState.Updated') {
      const e = event as { type: string; moodLevel?: number }
      if (typeof e.moodLevel === 'number') setMoodLevel(e.moodLevel)
    }
  }, [triggerChatter])
  useWebSocket(handleWS)

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

  const handleManualCreated = useCallback((incidentId: string, diagnosticRunId: string) => {
    setShowManualModal(false)
    setKubechan({ pose: 'thinking', incidentName: incidentId })
    setView({ type: 'incidents' })
    const poll = setInterval(async () => {
      try {
        const result = await api.getDiagnosticRunAnalysisResult(diagnosticRunId)
        if (result && result.status === 'completed') {
          clearInterval(poll)
          handleAnalysisComplete(result, incidentId)
        }
      } catch {
        // still pending — keep polling
      }
    }, 3000)
    setTimeout(() => clearInterval(poll), 5 * 60_000)
  }, [handleAnalysisComplete])

  const reactionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const showReaction = useCallback((line: string) => {
    setKubechan(prev => ({ ...prev, reactionLine: line }))
    if (reactionTimerRef.current !== null) clearTimeout(reactionTimerRef.current)
    reactionTimerRef.current = setTimeout(() => {
      reactionTimerRef.current = null
      setKubechan(prev => prev.reactionLine === line ? { ...prev, reactionLine: undefined } : prev)
    }, 4500)
  }, [])

  const handleIncidentResolved = useCallback(() => {
    const line = pickChatterLine('incident-resolved', moodLevelRef.current)
    if (chatterTimerRef.current !== null) clearTimeout(chatterTimerRef.current)
    lastInteractionRef.current = Date.now()
    silenceStageRef.current = 0
    setKubechan({ pose: 'chatter', chatterLine: line })
    chatterTimerRef.current = setTimeout(() => {
      chatterTimerRef.current = null
      setKubechan(prev => prev.pose === 'chatter' ? { pose: 'idle' } : prev)
    }, 9000)
  }, [])

  const handleRate = useCallback(async (resultId: string, rating: 'up' | 'down', confidence: number) => {
    try {
      await api.rateAnalysisResult(resultId, rating)
    } catch {
      // fire-and-forget; rating failure doesn't break UX
    }
    const event: ChatterEvent = rating === 'up'
      ? (confidence >= 0.75 ? 'rating-up-flustered' : 'rating-up')
      : (confidence >= 0.75 ? 'rating-down-high-conf' : 'rating-down-low-conf')
    const line = pickChatterLine(event, moodLevelRef.current)
    setKubechan(prev => {
      if (prev.pose !== 'speaking' || !prev.result || prev.result.id !== resultId) return prev
      return { ...prev, result: { ...prev.result, userRating: rating } }
    })
    showReaction(line)
  }, [moodLevelRef, showReaction])

  // ── Conditional rendering (after all hooks) ──────────────────────────────

  if (currentUser === undefined) {
    return <div className="app-loading">Loading…</div>
  }

  if (currentUser === null) {
    return <LoginPage onLogin={() => {
      api.me().then(u => setCurrentUser(u)).catch(() => {
        clearToken()
        setCurrentUser(null)
      })
    }} />
  }

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
          {currentUser.role === 'admin' && (
            <button
              className={`app-nav-btn${view.type === 'users' ? ' active' : ''}`}
              onClick={() => setView({ type: 'users' })}
            >
              Users
            </button>
          )}
          <button
            className={`app-nav-btn${view.type === 'llm-settings' ? ' active' : ''}`}
            onClick={() => setView({ type: 'llm-settings' })}
          >
            LLM Settings
          </button>
        </nav>
        <span className="app-subtitle">Kubernetes Anime Problem Insluter</span>
        <div className="app-user-info">
          <span className="app-username">{currentUser.username}</span>
          <button className="app-logout-btn" onClick={() => { clearToken(); setCurrentUser(null) }}>
            Sign out
          </button>
        </div>
      </header>
      <div className="app-body">
        <main className="app-main">
          {view.type === 'incidents' && (
            <IncidentList
              onAnalysisStart={handleAnalysisStart}
              onAnalysisComplete={handleAnalysisComplete}
              onAction={triggerChatter}
              onResolved={handleIncidentResolved}
              onReportManual={() => setShowManualModal(true)}
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
          {view.type === 'users' && currentUser.role === 'admin' && (
            <UsersPage />
          )}
          {view.type === 'llm-settings' && (
            <LLMSettingsPage />
          )}
        </main>
        <KubeChanSidebar state={kubechan} onPoke={handlePoke} moodLevel={moodLevel} onRate={handleRate} />
      </div>
      {showManualModal && (
        <ManualIncidentModal
          onClose={() => setShowManualModal(false)}
          onCreated={handleManualCreated}
        />
      )}
    </div>
  )
}

export default App
