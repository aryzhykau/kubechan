import { useRef, useEffect, useState } from 'react'
import type { AnalysisResult } from '../api/index'

const THINKING_IMAGES = [
  '/kubechan-looking.png',
  '/kubechan-looking-2.png',
  '/kubechan-looking-3.png',
  '/kubechan-pray-calm.png',
  '/kubechan-rolleye.png',
  '/kubechan-sigh-angry.png',
  '/kubechan-sigh-calm.png',
  '/kubechan-tired-1.png',
]

const THINKING_PHRASES = [
  "H-hmph… give me a second, I'm checking your dumb cluster…",
  "Fine, fine, I'm looking into it. Don't rush me!",
  "Y-you're lucky I'm even helping… scanning now…",
  "Ugh, another mess to clean up. Just give me a moment…",
  "I'm not doing this because I care! I'm just… running diagnostics…",
  "Don't stare at me while I'm working, it's distracting!",
  "This better not be something obvious you could've googled yourself…",
]

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

export function KubeChanSidebar({ state, onPoke, moodLevel = 0, onRate }: {
  state: KubeChanState
  onPoke?: () => void
  moodLevel?: number
  onRate?: (resultId: string, rating: 'up' | 'down', confidence: number) => void
}) {
  const { pose, result, incidentName, reactionLine } = state
  const contentRef = useRef<HTMLDivElement>(null)
  const [shaking, setShaking] = useState(false)
  const [reacting, setReacting] = useState<'up' | 'down' | null>(null)
  const [thinkingImg, setThinkingImg] = useState(THINKING_IMAGES[0])
  const [thinkingPhrase, setThinkingPhrase] = useState(THINKING_PHRASES[0])
  const [imgVisible, setImgVisible] = useState(true)

  useEffect(() => {
    if (pose !== 'thinking') {
      setImgVisible(true)
      return
    }
    setThinkingImg(THINKING_IMAGES[Math.floor(Math.random() * THINKING_IMAGES.length)])
    setThinkingPhrase(THINKING_PHRASES[Math.floor(Math.random() * THINKING_PHRASES.length)])

    let swapTimer: ReturnType<typeof setTimeout> | null = null
    const imgInterval = setInterval(() => {
      setImgVisible(false)
      swapTimer = setTimeout(() => {
        setThinkingImg(prev => {
          const others = THINKING_IMAGES.filter(i => i !== prev)
          return others[Math.floor(Math.random() * others.length)]
        })
        setImgVisible(true)
      }, 200)
    }, 2500)

    const phraseInterval = setInterval(() => {
      setThinkingPhrase(prev => {
        const others = THINKING_PHRASES.filter(p => p !== prev)
        return others[Math.floor(Math.random() * others.length)]
      })
    }, 5000)

    return () => {
      clearInterval(imgInterval)
      clearInterval(phraseInterval)
      if (swapTimer) clearTimeout(swapTimer)
    }
  }, [pose])

  const hasFalseAlarm = pose === 'speaking' && !!result?.payload?.suggestExclusionRule
  const [falseAlarmImg] = useState(() =>
    Math.random() < 0.5 ? '/kubechan-facepalm-angry.png' : '/kubechan-facepalm-calm.png'
  )
  const imgSrc = pose === 'thinking'
    ? thinkingImg
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
        {pose !== 'idle' && (
          <div className="sidebar-bubble-wrap">
            <div className="speech-bubble">
              {/* scrollable content */}
              <div className="speech-bubble-content" ref={contentRef}>
                {pose === 'thinking' && (
                  <span className="speech-thinking">
                    <span className="spinner" />
                    <span key={thinkingPhrase} className="speech-thinking-text">
                      {thinkingPhrase}
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
      {/* fixed character zone — always visible */}
      <div className="kubechan-char-zone">
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
      </div>
    </aside>
  )
}
