import { fetchBaseQuery } from '@reduxjs/toolkit/query/react'
import type { BaseQueryFn, FetchArgs, FetchBaseQueryError } from '@reduxjs/toolkit/query'
import { getToken } from '../../api/index'
import { clearUser } from '../slices/authSlice'

const rawBase = fetchBaseQuery({
  baseUrl: '/api/v1',
  prepareHeaders: (headers) => {
    const token = getToken()
    if (token) headers.set('Authorization', `Bearer ${token}`)
    return headers
  },
})

export const baseQuery: BaseQueryFn<string | FetchArgs, unknown, FetchBaseQueryError> = async (
  args,
  api,
  extra,
) => {
  const result = await rawBase(args, api, extra)
  if (result.error?.status === 401) {
    // Clear auth state — AuthGate will redirect to /login on the next render.
    api.dispatch(clearUser())
  }
  return result
}
