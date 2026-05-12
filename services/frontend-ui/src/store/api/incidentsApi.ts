import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { Incident, AnalyzeResponse, ManualIncidentResponse } from '../../api/index'

export const incidentsApi = createApi({
  reducerPath: 'incidentsApi',
  baseQuery,
  tagTypes: ['Incident'],
  endpoints: (build) => ({
    listIncidents: build.query<Incident[], string | void>({
      query: (ns = 'kubechan') => `/incidents?namespace=${ns}`,
      providesTags: (result) =>
        result
          ? [...result.map(({ metadata }) => ({ type: 'Incident' as const, id: metadata.name })), { type: 'Incident', id: 'LIST' }]
          : [{ type: 'Incident', id: 'LIST' }],
    }),
    getIncident: build.query<Incident, string>({
      query: (id) => `/incidents/${encodeURIComponent(id)}`,
      providesTags: (_, __, id) => [{ type: 'Incident', id }],
    }),
    analyze: build.mutation<AnalyzeResponse, string>({
      query: (id) => ({ url: `/incidents/${encodeURIComponent(id)}/analyze`, method: 'POST' }),
    }),
    resolveIncident: build.mutation<Incident, string>({
      query: (id) => ({ url: `/incidents/${encodeURIComponent(id)}/resolve`, method: 'POST' }),
      invalidatesTags: (_, __, id) => [{ type: 'Incident', id }, { type: 'Incident', id: 'LIST' }],
    }),
    markFalsePositive: build.mutation<Incident, string>({
      query: (id) => ({ url: `/incidents/${encodeURIComponent(id)}/false-positive`, method: 'POST' }),
      invalidatesTags: (_, __, id) => [{ type: 'Incident', id }, { type: 'Incident', id: 'LIST' }],
    }),
    createManualIncident: build.mutation<ManualIncidentResponse, {
      namespace: string
      resourceKind: string
      resourceName: string
      userMessage: string
      relatedResources: Array<{ kind: string; name: string; namespace: string; apiGroup?: string; evidenceSlices?: string[] }>
    }>({
      query: (body) => ({ url: '/incidents/manual', method: 'POST', body }),
      invalidatesTags: [{ type: 'Incident', id: 'LIST' }],
    }),
    augmentIncident: build.mutation<AnalyzeResponse, {
      incidentId: string
      relatedResources: Array<{ kind: string; name: string; namespace: string; apiGroup?: string; evidenceSlices?: string[] }>
    }>({
      query: ({ incidentId, relatedResources }) => ({
        url: `/incidents/${encodeURIComponent(incidentId)}/augment`,
        method: 'POST',
        body: { relatedResources },
      }),
    }),
  }),
})

export const {
  useListIncidentsQuery,
  useGetIncidentQuery,
  useAnalyzeMutation,
  useResolveIncidentMutation,
  useMarkFalsePositiveMutation,
  useCreateManualIncidentMutation,
  useAugmentIncidentMutation,
} = incidentsApi
