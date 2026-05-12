import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { User } from '../../api/index'

interface AdminSettings {
  'detector.debounce_window_secs'?: number
  'detector.pending_threshold_secs'?: number
  'detector.unavailable_threshold_secs'?: number
  [key: string]: unknown
}

interface LLMSettings {
  provider: string
  configured: boolean
  credFields: Record<string, unknown>
}

interface ModelEntry { id: string; label: string }

export const adminApi = createApi({
  reducerPath: 'adminApi',
  baseQuery,
  tagTypes: ['User', 'AdminSettings', 'LLMSettings'],
  endpoints: (build) => ({
    listUsers: build.query<User[], void>({
      query: () => '/users',
      providesTags: (result) =>
        result
          ? [...result.map(({ id }) => ({ type: 'User' as const, id })), { type: 'User', id: 'LIST' }]
          : [{ type: 'User', id: 'LIST' }],
    }),
    createUser: build.mutation<User, { username: string; password: string; role: 'admin' | 'viewer' }>({
      query: (body) => ({ url: '/users', method: 'POST', body }),
      invalidatesTags: [{ type: 'User', id: 'LIST' }],
    }),
    deleteUser: build.mutation<void, string>({
      query: (id) => ({ url: `/users/${encodeURIComponent(id)}`, method: 'DELETE' }),
      invalidatesTags: (_, __, id) => [{ type: 'User', id }, { type: 'User', id: 'LIST' }],
    }),

    getAdminSettings: build.query<AdminSettings, void>({
      query: () => '/settings',
      providesTags: ['AdminSettings'],
    }),
    updateAdminSettings: build.mutation<{ status: string }, Partial<AdminSettings>>({
      query: (patch) => ({ url: '/settings', method: 'PUT', body: patch }),
      invalidatesTags: ['AdminSettings'],
    }),

    getLLMSettings: build.query<LLMSettings, void>({
      query: () => '/me/llm-settings',
      providesTags: ['LLMSettings'],
    }),
    saveLLMSettings: build.mutation<{ status: string }, { provider: string; credentials: Record<string, string> }>({
      query: (body) => ({ url: '/me/llm-settings', method: 'PUT', body }),
      invalidatesTags: ['LLMSettings'],
    }),
    getLLMModels: build.query<{ providers: Record<string, ModelEntry[]> }, void>({
      query: () => '/llm-models',
    }),
  }),
})

export const {
  useListUsersQuery,
  useCreateUserMutation,
  useDeleteUserMutation,
  useGetAdminSettingsQuery,
  useUpdateAdminSettingsMutation,
  useGetLLMSettingsQuery,
  useSaveLLMSettingsMutation,
  useGetLLMModelsQuery,
} = adminApi
