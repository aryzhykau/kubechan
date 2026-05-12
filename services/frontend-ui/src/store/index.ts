import { configureStore, combineReducers } from '@reduxjs/toolkit'
import authReducer from './slices/authSlice'
import kubechanReducer from './slices/kubechanSlice'
import uiReducer from './slices/uiSlice'
import { authApi } from './api/authApi'
import { incidentsApi } from './api/incidentsApi'
import { diagnosticsApi } from './api/diagnosticsApi'
import { analysisApi } from './api/analysisApi'
import { exclusionRulesApi } from './api/exclusionRulesApi'
import { adminApi } from './api/adminApi'
import { kubechanApi } from './api/kubechanApi'
import { resourcesApi } from './api/resourcesApi'
import { wsMiddleware } from './middleware/wsMiddleware'

const rootReducer = combineReducers({
  auth: authReducer,
  kubechan: kubechanReducer,
  ui: uiReducer,
  [authApi.reducerPath]: authApi.reducer,
  [incidentsApi.reducerPath]: incidentsApi.reducer,
  [diagnosticsApi.reducerPath]: diagnosticsApi.reducer,
  [analysisApi.reducerPath]: analysisApi.reducer,
  [exclusionRulesApi.reducerPath]: exclusionRulesApi.reducer,
  [adminApi.reducerPath]: adminApi.reducer,
  [kubechanApi.reducerPath]: kubechanApi.reducer,
  [resourcesApi.reducerPath]: resourcesApi.reducer,
})

export type RootState = ReturnType<typeof rootReducer>

export const store = configureStore({
  reducer: rootReducer,
  middleware: (getDefaultMiddleware) =>
    getDefaultMiddleware()
      .concat(
        authApi.middleware,
        incidentsApi.middleware,
        diagnosticsApi.middleware,
        analysisApi.middleware,
        exclusionRulesApi.middleware,
        adminApi.middleware,
        kubechanApi.middleware,
        resourcesApi.middleware,
        wsMiddleware,
      ),
})

export type AppDispatch = typeof store.dispatch
