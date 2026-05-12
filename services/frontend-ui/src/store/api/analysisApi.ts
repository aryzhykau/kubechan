import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { AnalysisResult } from '../../api/index'

export const analysisApi = createApi({
  reducerPath: 'analysisApi',
  baseQuery,
  tagTypes: ['AnalysisResult'],
  endpoints: (build) => ({
    getDiagnosticRunAnalysisResult: build.query<AnalysisResult, string>({
      query: (runId) => `/diagnosticruns/${encodeURIComponent(runId)}/analysisresult`,
      providesTags: (_, __, runId) => [{ type: 'AnalysisResult', id: runId }],
    }),
    getAnalysisResult: build.query<AnalysisResult, string>({
      query: (id) => `/analysisresults/${encodeURIComponent(id)}`,
      providesTags: (_, __, id) => [{ type: 'AnalysisResult', id }],
    }),
    rateAnalysisResult: build.mutation<{ id: string; userRating: string }, { id: string; rating: 'up' | 'down' }>({
      query: ({ id, rating }) => ({
        url: `/analysisresults/${encodeURIComponent(id)}/rate`,
        method: 'POST',
        body: { rating },
      }),
      async onQueryStarted({ id, rating }, { dispatch, queryFulfilled }) {
        // Optimistic update
        const patch = dispatch(
          analysisApi.util.updateQueryData('getAnalysisResult', id, (draft) => {
            draft.userRating = rating
          }),
        )
        try {
          await queryFulfilled
        } catch {
          patch.undo()
        }
      },
      invalidatesTags: (_, __, { id }) => [{ type: 'AnalysisResult', id }],
    }),
  }),
})

export const {
  useGetDiagnosticRunAnalysisResultQuery,
  useGetAnalysisResultQuery,
  useRateAnalysisResultMutation,
} = analysisApi
