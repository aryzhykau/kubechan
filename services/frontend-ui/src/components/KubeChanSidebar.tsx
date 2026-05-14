import { useRef, useEffect, useState } from 'react'
import type { AnalysisResult } from '../api/index'

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
  const { pose, result, incidentName, reactionLine, chatterImage } = state
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
                    <span key={thinkingFrameIdx} className="speech-thinking-text">
                      {THINKING_FRAMES[thinkingFrameIdx].phrase}
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
