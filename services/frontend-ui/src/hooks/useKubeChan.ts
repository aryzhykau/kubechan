import { useCallback, useEffect, useRef } from 'react'
import { useAppDispatch, useAppSelector } from '../store/hooks'
import {
  selectMoodLevel,
  selectPose,
  setChatter,
  clearChatter,
  setReaction,
  clearReaction,
  setThinking,
  setSpeaking,
  setIdle,
  rateResult,
} from '../store/slices/kubechanSlice'
import { selectCurrentUser } from '../store/slices/authSlice'
import { useGetKubeChanStateQuery, usePokeMutation } from '../store/api/kubechanApi'
import { useRateAnalysisResultMutation } from '../store/api/analysisApi'
import { pickChatterLine, pickChatterExpression, type ChatterEvent } from '../persona/chatter'
import type { AnalysisResult } from '../api/index'

export type { ChatterEvent }

export function useKubeChan() {
  const dispatch = useAppDispatch()
  const moodLevel = useAppSelector(selectMoodLevel)
  const pose = useAppSelector(selectPose)
  const currentUser = useAppSelector(selectCurrentUser)

  const moodLevelRef = useRef(moodLevel)
  useEffect(() => { moodLevelRef.current = moodLevel }, [moodLevel])

  const poseRef = useRef(pose)
  useEffect(() => { poseRef.current = pose }, [pose])

  const chatterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const rantTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reactionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pokeCountRef = useRef(0)
  const pokeResetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastInteractionRef = useRef(Date.now())
  const silenceStageRef = useRef<0 | 1 | 2>(0)

  const [pokeApi] = usePokeMutation()
  const [rateApi] = useRateAnalysisResultMutation()

  // Seed mood level from server on login
  useGetKubeChanStateQuery(undefined, {
    skip: !currentUser,
  })

  const showChatter = useCallback((line: string, image?: string) => {
    if (chatterTimerRef.current) clearTimeout(chatterTimerRef.current)
    dispatch(setChatter({ line, image }))
    chatterTimerRef.current = setTimeout(() => {
      chatterTimerRef.current = null
      dispatch(clearChatter())
    }, 9_000)
  }, [dispatch])

  const triggerChatter = useCallback((event: ChatterEvent) => {
    const currentPose = poseRef.current
    if (event === 'idle' || event === 'silence-hint' || event === 'silence-paranoid') {
      if (currentPose !== 'idle') return
    } else {
      if (currentPose === 'thinking' || currentPose === 'speaking') return
      lastInteractionRef.current = Date.now()
      silenceStageRef.current = 0
    }
    showChatter(pickChatterLine(event, moodLevelRef.current), pickChatterExpression(event))
  }, [showChatter])

  const handlePoke = useCallback(() => {
    pokeCountRef.current += 1
    const count = pokeCountRef.current
    const event: ChatterEvent =
      count >= 5 ? 'poke-rage' :
      count >= 3 ? 'poke-annoyed' :
      'poke'
    triggerChatter(event)
    if (pokeResetTimerRef.current) clearTimeout(pokeResetTimerRef.current)
    pokeResetTimerRef.current = setTimeout(() => { pokeCountRef.current = 0 }, 8_000)
    pokeApi()
  }, [triggerChatter, pokeApi])

  const handleAnalysisStart = useCallback((incidentName: string) => {
    if (rantTimerRef.current) {
      clearTimeout(rantTimerRef.current)
      rantTimerRef.current = null
    }
    dispatch(setThinking(incidentName))
  }, [dispatch])

  const handleAnalysisComplete = useCallback((result: AnalysisResult, incidentName: string) => {
    dispatch(setSpeaking({ result, incidentName }))
    if (rantTimerRef.current) clearTimeout(rantTimerRef.current)
    rantTimerRef.current = setTimeout(() => {
      rantTimerRef.current = null
      dispatch(setIdle())
    }, 8_000)
    if (result.payload?.suggestExclusionRule) {
      const line = pickChatterLine('false-alarm', moodLevelRef.current)
      dispatch(setReaction(line))
      if (reactionTimerRef.current) clearTimeout(reactionTimerRef.current)
      reactionTimerRef.current = setTimeout(() => {
        reactionTimerRef.current = null
        dispatch(clearReaction())
      }, 4_500)
    }
  }, [dispatch])

  const handleRunResultLoaded = useCallback((result: AnalysisResult | null, runId: string) => {
    if (result) {
      dispatch(setSpeaking({ result, incidentName: result.incidentId || runId }))
    } else {
      dispatch(setIdle())
    }
  }, [dispatch])

  const handleRate = useCallback(async (resultId: string, rating: 'up' | 'down', confidence: number) => {
    dispatch(rateResult({ resultId, rating }))
    rateApi({ id: resultId, rating })
    const event: ChatterEvent = rating === 'up'
      ? (confidence >= 0.75 ? 'rating-up-flustered' : 'rating-up')
      : (confidence >= 0.75 ? 'rating-down-high-conf' : 'rating-down-low-conf')
    const line = pickChatterLine(event, moodLevelRef.current)
    dispatch(setReaction(line))
    if (reactionTimerRef.current) clearTimeout(reactionTimerRef.current)
    reactionTimerRef.current = setTimeout(() => {
      reactionTimerRef.current = null
      dispatch(clearReaction())
    }, 4_500)
  }, [dispatch, rateApi])

  const handleIncidentResolved = useCallback(() => {
    triggerChatter('incident-resolved')
    lastInteractionRef.current = Date.now()
    silenceStageRef.current = 0
  }, [triggerChatter])

  // Idle chatter every 60s
  useEffect(() => {
    const id = setInterval(() => {
      if (poseRef.current === 'idle') triggerChatter('idle')
    }, 60_000)
    return () => clearInterval(id)
  }, [triggerChatter])

  // Silence detection every 30s
  useEffect(() => {
    const id = setInterval(() => {
      if (poseRef.current !== 'idle') return
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

  return {
    triggerChatter,
    handlePoke,
    handleAnalysisStart,
    handleAnalysisComplete,
    handleRunResultLoaded,
    handleRate,
    handleIncidentResolved,
    handleDismiss: useCallback(() => {
      if (rantTimerRef.current) {
        clearTimeout(rantTimerRef.current)
        rantTimerRef.current = null
      }
      dispatch(setIdle())
    }, [dispatch]),
  }
}
