import { useQuery } from '@tanstack/react-query'
import type { Row } from '@/types'

export type LibraryRowQuery = {
  q: string
  status: string
  source: string
  flag: string
  res: string
  minDur: string
  sort: string
  dir: string
  offset: number
  limit: number
  tags?: string
}

export function useLibraryRows(params: LibraryRowQuery, enabled: boolean) {
  const qs = new URLSearchParams({
    q: params.q,
    status: params.status,
    source: params.source,
    flag: params.flag,
    res: params.res,
    minDur: params.minDur || '0',
    sort: params.sort,
    dir: params.dir,
    offset: String(params.offset),
    limit: String(params.limit),
    tags: params.tags || '',
  })
  return useQuery<{ ok: boolean; rows: Row[]; total: number }>({
    queryKey: ['library-rows', qs.toString()],
    queryFn: async () => {
      const res = await fetch(`/api/rows?${qs.toString()}`, { cache: 'no-store' })
      if (!res.ok) throw new Error('读取媒体库失败')
      return res.json()
    },
    enabled,
    staleTime: 2_000,
    placeholderData: prev => prev,
  })
}
