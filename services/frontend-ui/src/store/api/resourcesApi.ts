import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'
import type { KindItem, ResourceItem } from '../../api/index'

export const resourcesApi = createApi({
  reducerPath: 'resourcesApi',
  baseQuery,
  endpoints: (build) => ({
    listNamespaces: build.query<string[], void>({
      query: () => '/namespaces',
    }),
    listKinds: build.query<KindItem[], { ns: string; q?: string }>({
      query: ({ ns, q }) =>
        `/namespaces/${encodeURIComponent(ns)}/kinds${q ? `?q=${encodeURIComponent(q)}` : ''}`,
    }),
    listResources: build.query<ResourceItem[], { ns: string; kind: string; apiGroup?: string }>({
      query: ({ ns, kind, apiGroup }) =>
        `/namespaces/${encodeURIComponent(ns)}/resources?kind=${encodeURIComponent(kind)}${apiGroup ? `&apiGroup=${encodeURIComponent(apiGroup)}` : ''}`,
    }),
  }),
})

export const { useListNamespacesQuery, useListKindsQuery, useListResourcesQuery } = resourcesApi
