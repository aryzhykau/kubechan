import { createSlice } from '@reduxjs/toolkit'
import type { PayloadAction } from '@reduxjs/toolkit'
import type { ExclusionRuleProposal } from '../../api/index'
import type { RootState } from '../index'

interface UIState {
  showManualModal: boolean
  exclusionProposal: ExclusionRuleProposal | null
  deleteConfirmRunId: string | null
}

const initialState: UIState = {
  showManualModal: false,
  exclusionProposal: null,
  deleteConfirmRunId: null,
}

export const uiSlice = createSlice({
  name: 'ui',
  initialState,
  reducers: {
    openManualModal: (state) => { state.showManualModal = true },
    closeManualModal: (state) => { state.showManualModal = false },
    setExclusionProposal: (state, action: PayloadAction<ExclusionRuleProposal | null>) => {
      state.exclusionProposal = action.payload
    },
    setDeleteConfirmRunId: (state, action: PayloadAction<string | null>) => {
      state.deleteConfirmRunId = action.payload
    },
  },
})

export const { openManualModal, closeManualModal, setExclusionProposal, setDeleteConfirmRunId } = uiSlice.actions

export const selectShowManualModal = (state: RootState) => state.ui.showManualModal
export const selectExclusionProposal = (state: RootState) => state.ui.exclusionProposal
export const selectDeleteConfirmRunId = (state: RootState) => state.ui.deleteConfirmRunId
// composite selector for App.tsx convenience
export const selectUI = (state: RootState) => state.ui

export default uiSlice.reducer
