import { createSlice } from '@reduxjs/toolkit'
import type { PayloadAction } from '@reduxjs/toolkit'
import type { CurrentUser } from '../../api/index'
import { clearToken } from '../../api/index'
import type { RootState } from '../index'

interface AuthState {
  currentUser: CurrentUser | null | undefined // undefined = loading
}

const initialState: AuthState = { currentUser: undefined }

export const authSlice = createSlice({
  name: 'auth',
  initialState,
  reducers: {
    setUser: (state, action: PayloadAction<CurrentUser>) => {
      state.currentUser = action.payload
    },
    clearUser: (state) => {
      clearToken()
      state.currentUser = null
    },
  },
})

export const { setUser, clearUser } = authSlice.actions

export const selectCurrentUser = (state: RootState) => state.auth.currentUser

export default authSlice.reducer
