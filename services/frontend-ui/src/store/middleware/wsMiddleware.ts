import type { Middleware } from '@reduxjs/toolkit'
import ReconnectingWebSocket from 'reconnecting-websocket'
import { getToken } from '../../api/index'
import { setMoodLevel, setChatter } from '../slices/kubechanSlice'
import { incidentsApi } from '../api/incidentsApi'
import { analysisApi } from '../api/analysisApi'
import { pickChatterLine, pickChatterExpression } from '../../persona/chatter'
import { notifyAnalysisDone } from '../../analysis/tracker'
import { clearUser } from '../slices/authSlice'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const wsMiddleware: Middleware<object, any> = (store) => {
  let ws: ReconnectingWebSocket | null = null

  const connect = () => {
    const token = getToken()
    if (!token) return

    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    // Backend ServeWSWithAuth reads the token from the ?token= query param
    const url = `${protocol}://${window.location.host}/ws?token=${encodeURIComponent(token)}`

    ws = new ReconnectingWebSocket(url, [], {
      maxRetries: Infinity,
      minReconnectionDelay: 2000,
      maxReconnectionDelay: 30000,
      reconnectionDelayGrowFactor: 1.5,
    })

    // Detect HTTP 401 on the WS handshake: the upgrade is rejected before the
    // connection is established, so we verify via /api/v1/me on each failure.
    ws.onerror = () => {
      const token = getToken()
      if (!token) return
      const headers: Record<string, string> = { Authorization: `Bearer ${token}` }
      fetch('/api/v1/me', { headers }).then(res => {
        if (res.status === 401) store.dispatch(clearUser())
      }).catch(() => { /* network error — leave WS reconnect to handle it */ })
    }

    ws.onmessage = (e: MessageEvent) => {
      try {
        const event = JSON.parse(e.data as string) as { type: string; [key: string]: unknown }
        const state = store.getState()
        const moodLevel = state.kubechan.moodLevel

        if (event.type === 'Incident.Created') {
          store.dispatch(incidentsApi.util.invalidateTags([{ type: 'Incident', id: 'LIST' }]))
          store.dispatch(setChatter({ line: pickChatterLine('new-incident', moodLevel), image: pickChatterExpression('new-incident') }))
        } else if (event.type === 'KubeChanState.Updated') {
          if (typeof event.moodLevel === 'number') {
            store.dispatch(setMoodLevel(event.moodLevel))
          }
        } else if (event.type === 'Analysis.Completed') {
          const runId = event.diagnosticRunId as string | undefined
          if (runId) notifyAnalysisDone(runId)
          store.dispatch(analysisApi.util.invalidateTags([{ type: 'AnalysisResult', id: 'LIST' }]))
          store.dispatch(incidentsApi.util.invalidateTags([{ type: 'Incident', id: 'LIST' }]))
        } else if (event.type?.startsWith('Incident.') || event.type?.startsWith('ProblemCase.')) {
          store.dispatch(incidentsApi.util.invalidateTags([{ type: 'Incident', id: 'LIST' }]))
        }
      } catch {
        // ignore unparseable frames
      }
    }
  }

  const disconnect = () => {
    ws?.close()
    ws = null
  }

  return (next) => (action) => {
    const result = next(action)

    // Connect when user logs in, disconnect when logged out
    if ((action as { type: string }).type === 'auth/setUser') {
      disconnect()
      connect()
    }
    if ((action as { type: string }).type === 'auth/clearUser') {
      disconnect()
    }

    return result
  }
}
