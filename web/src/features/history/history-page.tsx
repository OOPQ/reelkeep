import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { ChevronDown, Clock3, History, Play, Search, Trash2 } from 'lucide-react'
import { useAppState } from '@/features/use-app-state'
import { confirmAction } from '@/store/confirm'
import { post } from '@/lib/api'
import { toast } from 'sonner'
import { useUiStore } from '@/store/ui'
import { coverUrl, cn, rowTitle } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { ApiError } from '@/components/api-error'
import type { Row } from '@/types'

type HistoryGroup = {
  date: string
  rows: Row[]
}

function coverOrFallback(row: Row) {
  return coverUrl(row.token)
}

function HistoryRow({ row, onPlay }: { row: Row; onPlay: () => void }) {
  const [broken, setBroken] = React.useState(false)
  const coverRev = useUiStore(s => s.coverRev)
  return (
    <Card className="group flex items-center gap-3 p-2.5 transition-colors hover:border-[color:var(--border-strong)]">
      <button type="button" onClick={onPlay} className="relative h-[84px] w-[64px] shrink-0 overflow-hidden rounded-lg" aria-label="继续播放">
        {broken ? (
          <div className="grid h-full w-full place-items-center bg-[color:var(--surface-2)] text-[color:var(--subtle)]">
            <History className="size-4" />
          </div>
        ) : (
          <img
            src={`${coverOrFallback(row)}&c=${coverRev}`}
            alt=""
            loading="lazy"
            onError={() => setBroken(true)}
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
          />
        )}
        <span className="absolute inset-0 grid place-items-center bg-black/35 text-white opacity-0 transition-opacity group-hover:opacity-100">
          <Play className="size-5 fill-current" />
        </span>
      </button>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[12px] font-medium" title={rowTitle(row)}>{rowTitle(row)}</div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[9.5px] text-[color:var(--subtle)]">
          <span>{row.source || row.site || '未知来源'}</span>
          {row.duration ? <span>· {row.duration}</span> : null}
          {row.res ? <span>· {row.res}</span> : null}
          <span className="text-[color:var(--primary-2)]">· 播放 {row.play_count || 0} 次</span>
        </div>
        <div className="mt-1 text-[9px] text-[color:var(--subtle)]">
          {row.last_play || '—'} · {row.file_name ? '本地文件' : '未下载'}
        </div>
      </div>
      <Button size="sm" variant="ghost" onClick={onPlay} className="shrink-0">
        <Play className="size-3.5" /> 继续
      </Button>
    </Card>
  )
}

function HistorySection({ group, defaultOpen, onPlay }: { group: HistoryGroup; defaultOpen: boolean; onPlay: (row: Row) => void }) {
  const [open, setOpen] = React.useState(defaultOpen)
  return (
    <details open={open} onToggle={e => setOpen(e.currentTarget.open)} className="overflow-hidden rounded-[var(--radius-card)] border border-[color:var(--border)] bg-[color:var(--surface)]">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 marker:hidden">
        <ChevronDown className={cn('size-3.5 text-[color:var(--subtle)] transition-transform', open ? '' : '-rotate-90')} />
        <span className="text-[11px] font-medium">{group.date}</span>
        <span className="text-[9.5px] text-[color:var(--subtle)]">{group.rows.length} 条</span>
        <span className="ml-auto text-[9px] text-[color:var(--subtle)]">展开 / 收起</span>
      </summary>
      <div className="space-y-1.5 border-t border-[color:var(--border)] p-2">
        {group.rows.map(row => (
          <HistoryRow key={row.token} row={row} onPlay={() => onPlay(row)} />
        ))}
      </div>
    </details>
  )
}

export function HistoryPage() {
  const { data, refetch, error, isFetching } = useAppState(15_000)
  const navigate = useNavigate()
  const openPreview = useUiStore(s => s.openPreview)
  const [query, setQuery] = React.useState('')

  const played = React.useMemo(() => {
    const q = query.trim().toLowerCase()
    return (data?.rows || [])
      .filter(r => r.status !== 'recycled' && r.last_play)
      .filter(r => !q || `${r.title} ${r.token} ${r.source}`.toLowerCase().includes(q))
      .sort((a, b) => (b.last_play || '').localeCompare(a.last_play || ''))
  }, [data?.rows, query])

  const groups = React.useMemo<HistoryGroup[]>(() => {
    const map = new Map<string, Row[]>()
    for (const row of played) {
      const date = (row.last_play || '').slice(0, 10) || '未知日期'
      if (!map.has(date)) map.set(date, [])
      map.get(date)!.push(row)
    }
    return [...map.entries()].map(([date, rows]) => ({ date, rows }))
  }, [played])

  const totalPlays = played.reduce((sum, r) => sum + (Number(r.play_count) || 0), 0)

  const clearHistory = async () => {
    if (!played.length) return
    if (!(await confirmAction({ title: '清空浏览历史', description: '只清空播放时间和次数，不删除台账、文件或收藏。', confirmText: '清空历史', danger: true }))) return
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/history/clear', {})
      if (!r.ok) throw new Error(r.error || '清空失败')
      for (const row of played) {
        try { localStorage.removeItem(`pektino.resume.${row.token}`) } catch { }
      }
      toast.success('浏览历史已清空')
      await refetch()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  if (error) {
    return (
      <div className="p-4">
        <ApiError title="浏览历史加载失败" error={error} onRetry={() => void refetch()} busy={isFetching} />
      </div>
    )
  }

  return (
    <div className="flex w-full flex-col gap-3 p-4">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <div className="flex items-center gap-1.5 text-[10px] tracking-[.18em] text-[color:var(--subtle)]">
            <History className="size-3" /> HISTORY
          </div>
          <div className="mt-0.5 text-[16px] font-semibold">浏览历史</div>
          <div className="mt-1 text-[10px] text-[color:var(--muted)]">按最后播放时间倒序；只统计真正打开过的条目。</div>
        </div>
        <div className="flex items-center gap-3 text-[10px] text-[color:var(--subtle)]">
          <span>{played.length} 条记录</span>
          <span>累计 {totalPlays} 次</span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-[color:var(--subtle)]" />
          <Input value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索标题 / token / 来源" className="pl-8" />
        </div>
        <Button size="sm" variant="outline" onClick={() => navigate('/')}>
          <Clock3 className="size-3.5" /> 去媒体库
        </Button>
        <Button size="sm" variant="ghost" disabled={!played.length} onClick={clearHistory}>
          <Trash2 className="size-3.5" /> 清空历史
        </Button>
      </div>

      {groups.length ? (
        <div className="space-y-2">
          {groups.map((group, index) => (
            <HistorySection
              key={group.date}
              group={group}
              defaultOpen={index === 0}
              onPlay={row => openPreview(row.token, true)}
            />
          ))}
        </div>
      ) : (
        <div className="grid min-h-[280px] place-items-center rounded-[var(--radius-card)] border border-[color:var(--border)] bg-[color:var(--surface)]">
          <div className="text-center">
            <Clock3 className="mx-auto size-7 text-[color:var(--subtle)]" />
            <div className="mt-2 text-[12px] font-medium">{query ? '没有匹配的历史' : '还没有浏览记录'}</div>
            <div className="mt-1 text-[10px] text-[color:var(--subtle)]">在媒体库打开并播放视频后，这里会自动出现。</div>
          </div>
        </div>
      )}
    </div>
  )
}
