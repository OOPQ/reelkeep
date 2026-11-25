import * as React from 'react'
import type { Row } from '@/types'

export type PreviewSource = 'library' | 'gacha' | 'queue' | 'history' | 'overview' | 'collection' | 'tag' | 'torrent' | 'other'

export type PreviewSession = {
  rows: Row[]
  token: string
  autoplay: boolean
  source: PreviewSource
}

type OpenOptions = { autoplay?: boolean; source?: PreviewSource }

export type PreviewApi = {
  session: PreviewSession | null
  open: (rows: Row[], token: string, options?: OpenOptions) => void
  navigate: (token: string, autoplay?: boolean) => void
  refreshRows: (rows: Row[]) => void
  removeRow: (token: string) => void
  close: () => void
}

const PreviewContext = React.createContext<PreviewApi | null>(null)

function dedupeRows(rows: Row[]) {
  const seen = new Set<string>()
  const out: Row[] = []
  for (const row of rows) {
    if (!row?.token || seen.has(row.token)) continue
    seen.add(row.token)
    out.push(row)
  }
  return out
}

export function PreviewProvider({ children }: { children: React.ReactNode }) {
  const [session, setSession] = React.useState<PreviewSession | null>(null)

  const open = React.useCallback((rows: Row[], token: string, options: OpenOptions = {}) => {
    if (!token) return
    const next = dedupeRows(rows)
    if (!next.length) return
    setSession({ rows: next, token, autoplay: Boolean(options.autoplay), source: options.source || 'other' })
  }, [])

  const navigate = React.useCallback((token: string, autoplay = false) => {
    setSession(s => s ? { ...s, token, autoplay } : s)
  }, [])

  const refreshRows = React.useCallback((rows: Row[]) => {
    const next = dedupeRows(rows)
    if (!next.length) return
    setSession(s => {
      if (!s) return s
      // 当前条目不能被晚到的全量列表挤掉，否则浮窗会找不到当前项。
      if (!next.some(r => r.token === s.token)) {
        const current = s.rows.find(r => r.token === s.token)
        if (current) next.unshift(current)
      }
      return { ...s, rows: next }
    })
  }, [])

  const removeRow = React.useCallback((token: string) => {
    setSession(s => {
      if (!s) return s
      const rows = s.rows.filter(r => r.token !== token)
      if (!rows.length) return null
      return { ...s, rows }
    })
  }, [])

  const close = React.useCallback(() => setSession(null), [])

  const value = React.useMemo<PreviewApi>(
    () => ({ session, open, navigate, refreshRows, removeRow, close }),
    [session, open, navigate, refreshRows, removeRow, close],
  )

  return <PreviewContext.Provider value={value}>{children}</PreviewContext.Provider>
}

export function usePreview() {
  const ctx = React.useContext(PreviewContext)
  if (!ctx) throw new Error('usePreview 必须在 PreviewProvider 内使用')
  return ctx
}
