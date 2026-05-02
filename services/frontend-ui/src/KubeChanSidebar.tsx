import type { AnalysisResult } from './api'

export type KubeChanPose = 'idle' | 'thinking' | 'speaking'

export interface KubeChanState {
  pose: KubeChanPose
  incidentName?: string
  result?: AnalysisResult
}

function confidenceColor(c: number): string {
  if (c >= 0.8) return 'high'
  if (c >= 0.5) return 'medium'
  return 'low'
}

export function KubeChanSidebar({ state }: { state: KubeChanState }) {
  const { pose, result, incidentName } = state
  const imgSrc = pose === 'thinking' ? '/kubechan-thinking.png' : '/kubechan-idle.png'

  const confidence = result ? (result.confidence ?? result.payload?.confidence ?? 0) : 0
  const openingRant    = result?.payload?.openingRant   || ''
  const rootCause      = result ? (result.likelyRootCause || result.payload?.likelyRootCause || '—') : ''
  const evidenceChain  = result?.payload?.evidenceChain  || ''
  const recommendation = result?.payload?.recommendation || ''
  const closingInsult  = result?.payload?.closingInsult  || ''
  const pct = Math.round(confidence * 100)
  const level = confidenceColor(confidence)

  return (
    <aside className="kubechan-sidebar">
      {/* scrollable top zone */}
      <div className="kubechan-scroll-zone">
        {pose !== 'idle' && (
          <div className="sidebar-bubble-wrap">
            <div className="speech-bubble">
              {pose === 'thinking' ? (
                <span className="speech-thinking">
                  <span className="spinner" />
                  H-hmph… give me a second, I'm checking your dumb cluster…
                </span>
              ) : (
                <>
                  {incidentName && (
                    <p className="speech-incident-label">{incidentName}</p>
                  )}
                  {openingRant && (
                    <p className="speech-opening-rant">{openingRant}</p>
                  )}
                  <p className="speech-root-label">Root cause</p>
                  <p className="speech-root">{rootCause}</p>
                  {evidenceChain && (
                    <>
                      <p className="speech-section-label">Evidence</p>
                      <p className="speech-evidence">{evidenceChain}</p>
                    </>
                  )}
                  {recommendation && (
                    <>
                      <p className="speech-section-label">Fix it</p>
                      <p className="speech-rec">{recommendation}</p>
                    </>
                  )}
                  {closingInsult && (
                    <p className="speech-closing-insult">{closingInsult}</p>
                  )}
                  {result && (
                    <div className="speech-meta">
                      <span className={`confidence-badge confidence-${level}`}>{pct}% confidence</span>
                      <span className="analysis-model">{result.model}</span>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>
        )}
      </div>
      {/* fixed character zone — always visible */}
      <div className="kubechan-char-zone">
        {pose === 'idle' && (
          <p className="kubechan-idle-hint">click an incident<br />to ask me for help</p>
        )}
        <img src={imgSrc} alt="KubeChan" className="kubechan-sidebar-char" />
      </div>
    </aside>
  )
}
