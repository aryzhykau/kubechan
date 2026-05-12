import { createApi } from '@reduxjs/toolkit/query/react'
import { baseQuery } from './baseQuery'

interface KubeChanStateResponse {
  moodLevel: number
  pokeCount: number
}

export const kubechanApi = createApi({
  reducerPath: 'kubechanApi',
  baseQuery,
  tagTypes: ['KubeChanState'],
  endpoints: (build) => ({
    getKubeChanState: build.query<KubeChanStateResponse, void>({
      query: () => '/kubechan/state',
      providesTags: ['KubeChanState'],
    }),
    poke: build.mutation<KubeChanStateResponse, void>({
      query: () => ({ url: '/kubechan/poke', method: 'POST' }),
      invalidatesTags: ['KubeChanState'],
    }),
  }),
})

export const { useGetKubeChanStateQuery, usePokeMutation } = kubechanApi
