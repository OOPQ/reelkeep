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
  const qs = rowQueryString(params)
  return useQuery<{ ok: boolean; rows: Row[]; total: number }>({
    queryKey: ['library-rows', qs],
    queryFn: async () => {
      const res = await fetch(`/api/rows?${qs}`, { cache: 'no-store' })
      if (!res.ok) throw new Error('读取媒体库失败')
      return res.json()
    },
    enabled,
    staleTime: 2_000,
    placeholderData: prev => prev,
  })
}

// rowQueryString 把筛选条件拼成查询串（/api/rows 与 /api/rows/tokens 共用）。
export function rowQueryString(params: Omit<LibraryRowQuery, 'offset' | 'limit'> & { offset?: number; limit?: number }) {
  const qs = new URLSearchParams({
    q: params.q,
    status: params.status,
    source: params.source,
    flag: params.flag,
    res: params.res,
    minDur: params.minDur || '0',
    sort: params.sort,
    dir: params.dir,
    tags: params.tags || '',
  })
  if (params.offset != null) qs.set('offset', String(params.offset))
  if (params.limit != null) qs.set('limit', String(params.limit))
  return qs.toString()
}

// useRowTokens 取当前筛选下的全部 token（有序），给预览上下翻页用。
export function useRowTokens(params: Omit<LibraryRowQuery, 'offset' | 'limit'>, enabled: boolean) {
  const qs = rowQueryString(params)
  return useQuery<{ ok: boolean; tokens: string[]; total: number }>({
    queryKey: ['library-row-tokens', qs],
    queryFn: async () => {
      const res = await fetch(`/api/rows/tokens?${qs}`, { cache: 'no-store' })
      if (!res.ok) throw new Error('读取列表失败')
      return res.json()
    },
    enabled,
    // 新条目入库后 token 顺序要尽快跟上，否则浮窗的上一项/下一项会停在旧范围。
    staleTime: 5_000,
  })
}
