import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { AnalysisProgress, DiagnosticRunSummary, Evidence } from '../../api/index'

export const diagnosticsApi = createApi({
  reducerPath: 'diagnosticsApi',
  baseQuery,
  tagTypes: ['DiagnosticRun', 'Evidence', 'AnalysisProgress'],
  endpoints: (build) => ({
    getAnalysisProgress: build.query<AnalysisProgress, string>({
      query: (runId) => `/diagnosticruns/${encodeURIComponent(runId)}/analysisprogress`,
      providesTags: (_, __, runId) => [{ type: 'AnalysisProgress', id: runId }, { type: 'AnalysisProgress', id: 'LIST' }],
    }),
    cancelAnalysis: build.mutation<{ status: string }, string>({
      query: (runId) => ({ url: `/diagnosticruns/${encodeURIComponent(runId)}/analysis/cancel`, method: 'POST' }),
      invalidatesTags: (_, __, runId) => [{ type: 'AnalysisProgress', id: runId }, { type: 'DiagnosticRun', id: 'LIST' }],
    }),
    listDiagnosticRuns: build.query<DiagnosticRunSummary[], string | void>({
      query: (incidentId) =>
        incidentId
          ? `/diagnosticruns?incidentId=${encodeURIComponent(incidentId)}`
          : '/diagnosticruns',
      providesTags: (result) =>
        result
          ? [
              ...result.map(({ diagnosticRunId }) => ({ type: 'DiagnosticRun' as const, id: diagnosticRunId })),
              { type: 'DiagnosticRun', id: 'LIST' },
            ]
          : [{ type: 'DiagnosticRun', id: 'LIST' }],
    }),
    getDiagnosticRunEvidence: build.query<Evidence, string>({
      query: (runId) => `/diagnosticruns/${encodeURIComponent(runId)}/evidence`,
      providesTags: (_, __, runId) => [{ type: 'Evidence', id: runId }],
    }),
    deleteDiagnosticRun: build.mutation<void, string>({
      query: (runId) => ({ url: `/diagnosticruns/${encodeURIComponent(runId)}`, method: 'DELETE' }),
      invalidatesTags: (_, __, runId) => [
        { type: 'DiagnosticRun', id: runId },
        { type: 'DiagnosticRun', id: 'LIST' },
      ],
    }),
    bulkDeleteDiagnosticRuns: build.mutation<{ deleted: number }, string[]>({
      query: (ids) => ({ url: '/diagnosticruns', method: 'DELETE', body: { ids } }),
      invalidatesTags: [{ type: 'DiagnosticRun', id: 'LIST' }],
    }),
  }),
})

export const {
  useGetAnalysisProgressQuery,
  useCancelAnalysisMutation,
  useListDiagnosticRunsQuery,
  useGetDiagnosticRunEvidenceQuery,
  useDeleteDiagnosticRunMutation,
  useBulkDeleteDiagnosticRunsMutation,
} = diagnosticsApi
