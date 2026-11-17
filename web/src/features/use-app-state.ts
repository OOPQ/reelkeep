import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { fetchState, post } from '@/lib/api'
import type { AppState } from '@/types'

export function useAppState(refetchMs = 15_000) {
  return useQuery<AppState>({
    queryKey: ['state'],
    queryFn: async ({ signal, client }) => {
      const prev = client.getQueryData<AppState>(['state'])
      const next = await fetchState(signal, prev?.rowsRev)
      if (!next.rows && prev?.rows) next.rows = prev.rows
      return next
    },
    staleTime: 5_000,
    refetchInterval: query => {
      const state = query.state.data as AppState | undefined
      return state?.job?.running || state?.download?.running ? 1_500 : refetchMs
    },
    refetchOnWindowFocus: false,
  })
}

export function useToggleFav() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ token, on }: { token: string; on: boolean }) =>
      post<{ ok: boolean }>('/api/fav', { tokens: [token], on }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['state'] }),
  })
}
