import { useRef, useEffect, useState } from 'react'
import { useGetAdminSettingsQuery } from '../store/api/adminApi'
import { useAppSelector } from '../store/hooks'
import { selectCurrentUser } from '../store/slices/authSlice'
import type { AnalysisResult } from '../api/index'

export type KubeChanPose = 'idle' | 'thinking' | 'speaking' | 'chatter'

export interface KubeChanState {
  pose: KubeChanPose
  incidentName?: string
  result?: AnalysisResult
  chatterLine?: string
  reactionLine?: string
}

function confidenceColor(c: number): string {
  if (c >= 0.8) return 'high'
  if (c >= 0.5) return 'medium'
  return 'low'
}

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

export function KubeChanSidebar({ state, onPoke, moodLevel = 0, onRate }: {
  state: KubeChanState
  onPoke?: () => void
  moodLevel?: number
  onRate?: (resultId: string, rating: 'up' | 'down', confidence: number) => void
}) {
  const { pose, result, incidentName, reactionLine } = state
  const contentRef = useRef<HTMLDivElement>(null)
  const currentUser = useAppSelector(selectCurrentUser)
  const { data: adminSettings } = useGetAdminSettingsQuery(undefined, { skip: !currentUser })
  const personaOn = adminSettings?.['persona.allowed'] !== false && adminSettings?.['persona.enabled'] === true
  const [shaking, setShaking] = useState(false)
  const [reacting, setReacting] = useState<'up' | 'down' | null>(null)
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

  function handleRate(rating: 'up' | 'down') {
    if (!result?.id || result.userRating) return
    setReacting(rating)
    setTimeout(() => setReacting(null), 700)
    onRate?.(result.id, rating, result.confidence ?? 0)
  }

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
        {(pose === 'thinking' || pose === 'speaking' || result || (pose === 'chatter' && personaOn)) && (
          <div className="sidebar-bubble-wrap">
            <div className="speech-bubble">
              {/* scrollable content */}
              <div className="speech-bubble-content" ref={contentRef}>
                {pose === 'thinking' && (
                  <span className="speech-thinking">
                    <span className="spinner" />
                    <span key={thinkingFrameIdx} className="speech-thinking-text">
                      {personaOn ? THINKING_FRAMES[thinkingFrameIdx].phrase : 'Analyzing your cluster…'}
                    </span>
                  </span>
                )}
                {pose === 'chatter' && personaOn && (
                  <p className="speech-chatter">{state.chatterLine}</p>
                )}
                {pose === 'speaking' && (
                  <>
                    {incidentName && (
                      <p className="speech-incident-label">{incidentName}</p>
                    )}
                    {openingRant && personaOn && (
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
                    {closingInsult && personaOn && (
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
              {/* rating panel — always pinned to bubble bottom */}
              {pose === 'speaking' && result?.id && onRate && (
                <div className="speech-rating-panel">
                  <span className="speech-rating-label">
                    {result.userRating ? 'Feedback recorded.' : 'Was this correct?'}
                  </span>
                  <div className="speech-rating-btns">
                    <button
                      className={`rating-btn rating-btn-up${result.userRating === 'up' ? ' active' : ''}${result.userRating && result.userRating !== 'up' ? ' dimmed' : ''}`}
                      onClick={() => handleRate('up')}
                      disabled={!!result.userRating}
                      title="Correct diagnosis"
                    >
                      <span className="rating-icon">👍</span>
                      <span className="rating-label">Correct</span>
                    </button>
                    <button
                      className={`rating-btn rating-btn-down${result.userRating === 'down' ? ' active' : ''}${result.userRating && result.userRating !== 'down' ? ' dimmed' : ''}`}
                      onClick={() => handleRate('down')}
                      disabled={!!result.userRating}
                      title="Wrong diagnosis"
                    >
                      <span className="rating-icon">👎</span>
                      <span className="rating-label">Wrong</span>
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
      {/* fixed character zone — only visible when persona is on */}
      {personaOn && <div className="kubechan-char-zone">
        {pose === 'idle' && (
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
            reacting === 'up' ? 'kubechan-react-up' : '',
            reacting === 'down' ? 'kubechan-react-down' : '',
          ].filter(Boolean).join(' ')}
          onClick={handlePoke}
          style={{ cursor: 'pointer', opacity: imgVisible ? 1 : 0, transition: 'opacity 0.2s ease' }}
        />
        <div className={`kubechan-mood mood-${moodLevel}`}>
          {moodLevel === 0 && <span>· calm</span>}
          {moodLevel === 1 && <span>· irritated</span>}
          {moodLevel >= 2 && <span>· RAGE</span>}
        </div>
      </div>}
    </aside>
  )
}
