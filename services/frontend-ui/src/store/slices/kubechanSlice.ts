import { createSlice } from '@reduxjs/toolkit'
import type { PayloadAction } from '@reduxjs/toolkit'
import type { AnalysisResult } from '../../api/index'
import type { RootState } from '../index'

export type KubeChanPose = 'idle' | 'thinking' | 'speaking' | 'chatter'

interface KubeChanState {
  pose: KubeChanPose
  moodLevel: number
  incidentName?: string
  result?: AnalysisResult
  chatterLine?: string
  reactionLine?: string
}

const initialState: KubeChanState = { pose: 'idle', moodLevel: 0 }

export const kubechanSlice = createSlice({
  name: 'kubechan',
  initialState,
  reducers: {
    setIdle: (state) => {
      state.pose = 'idle'
      state.incidentName = undefined
      state.result = undefined
      state.chatterLine = undefined
      state.reactionLine = undefined
    },
    setThinking: (state, action: PayloadAction<string>) => {
      state.pose = 'thinking'
      state.incidentName = action.payload
      state.result = undefined
      state.chatterLine = undefined
    },
    setSpeaking: (state, action: PayloadAction<{ result: AnalysisResult; incidentName: string }>) => {
      state.pose = 'speaking'
      state.result = action.payload.result
      state.incidentName = action.payload.incidentName
    },
    setChatter: (state, action: PayloadAction<string>) => {
      state.pose = 'chatter'
      state.chatterLine = action.payload
    },
    clearChatter: (state) => {
      if (state.pose === 'chatter') state.pose = 'idle'
      state.chatterLine = undefined
    },
    setReaction: (state, action: PayloadAction<string>) => {
      state.reactionLine = action.payload
    },
    clearReaction: (state) => {
      state.reactionLine = undefined
    },
    setMoodLevel: (state, action: PayloadAction<number>) => {
      state.moodLevel = action.payload
    },
    rateResult: (state, action: PayloadAction<{ resultId: string; rating: 'up' | 'down' }>) => {
      if (state.result && state.result.id === action.payload.resultId) {
        state.result = { ...state.result, userRating: action.payload.rating }
      }
    },
  },
})

export const {
  setIdle,
  setThinking,
  setSpeaking,
  setChatter,
  clearChatter,
  setReaction,
  clearReaction,
  setMoodLevel,
  rateResult,
} = kubechanSlice.actions

export const selectKubeChan = (state: RootState) => state.kubechan
export const selectMoodLevel = (state: RootState) => state.kubechan.moodLevel
export const selectPose = (state: RootState) => state.kubechan.pose

export default kubechanSlice.reducer
