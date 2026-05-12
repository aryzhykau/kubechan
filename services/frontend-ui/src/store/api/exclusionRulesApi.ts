import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { ExclusionRule } from '../../api/index'

export const exclusionRulesApi = createApi({
  reducerPath: 'exclusionRulesApi',
  baseQuery,
  tagTypes: ['ExclusionRule'],
  endpoints: (build) => ({
    listExclusionRules: build.query<ExclusionRule[], void>({
      query: () => '/exclusion-rules',
      providesTags: (result) =>
        result
          ? [
              ...result.map(({ name }) => ({ type: 'ExclusionRule' as const, id: name })),
              { type: 'ExclusionRule', id: 'LIST' },
            ]
          : [{ type: 'ExclusionRule', id: 'LIST' }],
    }),
    createExclusionRule: build.mutation<ExclusionRule, { name: string; spec: ExclusionRule['spec'] }>({
      query: (body) => ({ url: '/exclusion-rules', method: 'POST', body }),
      invalidatesTags: [{ type: 'ExclusionRule', id: 'LIST' }],
    }),
    setExclusionRuleEnabled: build.mutation<ExclusionRule, { name: string; enabled: boolean }>({
      query: ({ name, enabled }) => ({
        url: `/exclusion-rules/${encodeURIComponent(name)}`,
        method: 'PATCH',
        body: { enabled },
      }),
      invalidatesTags: (_, __, { name }) => [{ type: 'ExclusionRule', id: name }, { type: 'ExclusionRule', id: 'LIST' }],
    }),
    deleteExclusionRule: build.mutation<void, string>({
      query: (name) => ({ url: `/exclusion-rules/${encodeURIComponent(name)}`, method: 'DELETE' }),
      invalidatesTags: (_, __, name) => [{ type: 'ExclusionRule', id: name }, { type: 'ExclusionRule', id: 'LIST' }],
    }),
  }),
})

export const {
  useListExclusionRulesQuery,
  useCreateExclusionRuleMutation,
  useSetExclusionRuleEnabledMutation,
  useDeleteExclusionRuleMutation,
} = exclusionRulesApi
