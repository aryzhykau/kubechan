import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { CurrentUser } from '../../api/index'
import { setToken } from '../../api/index'

export const authApi = createApi({
  reducerPath: 'authApi',
  baseQuery,
  endpoints: (build) => ({
    me: build.query<CurrentUser, void>({
      query: () => '/auth/me',
    }),
    login: build.mutation<{ token: string; role: string; username: string }, { username: string; password: string }>({
      query: (creds) => ({ url: '/auth/login', method: 'POST', body: creds }),
      async onQueryStarted(_, { queryFulfilled }) {
        try {
          const { data } = await queryFulfilled
          setToken(data.token)
        } catch {
          // ignore — error handled in UI
        }
      },
    }),
  }),
})

export const { useMeQuery, useLoginMutation } = authApi
