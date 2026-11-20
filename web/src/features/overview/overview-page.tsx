import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ArrowRight,
  CalendarDays,
  Clock3,
  Download,
  FolderCheck,
  Heart,
  Library,
  PlayCircle,
  Sparkles,
  TrendingUp,
} from 'lucide-react'
import { useAppState } from '@/features/use-app-state'
import { useUiStore } from '@/store/ui'
import { coverUrl, cn, formatCount } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { StatCard } from '@/components/stat-card'
import type { Row } from '@/types'

function dateKey(d: Date) {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function percent(n: number, max: number) {
  if (!max || n <= 0) return 0
  return Math.max(4, Math.round((n / max) * 100))
}

function Cover({ row, className }: { row: Row; className?: string }) {
  const [broken, setBroken] = React.useState(false)
  const coverRev = useUiStore(s => s.coverRev)
  if (broken || !row.token) {
    return (
      <div className={cn('flex items-center justify-center bg-[color:var(--surface-2)] text-[color:var(--subtle)]', className)}>
        <Library className="size-5" />
      </div>
    )
  }
  return (
    <img
      src={`${coverUrl(row.token)}&c=${coverRev}`}
      alt=""
      loading="lazy"
      onError={() => setBroken(true)}
      className={cn('object-cover', className)}
    />
  )
}

export function OverviewPage() {
  const { data } = useAppState(15_000)
  const navigate = useNavigate()
  const openPreview = useUiStore(s => s.openPreview)

  const rows = React.useMemo(() => (data?.rows || []).filter(r => r.status !== 'recycled'), [data?.rows])
  const stats = data?.stats
  const sources = data?.sources || []
  const sourceName = React.useMemo(() => new Map(sources.map(s => [s.key, s.name])), [sources])

  const sourceStats = React.useMemo(() => {
    const map = new Map<string, number>()
    for (const r of rows) {
      const key = r.source || '未知'
      map.set(key, (map.get(key) || 0) + 1)
    }
    return [...map.entries()].sort((a, b) => b[1] - a[1])
  }, [rows])

  const resStats = React.useMemo(() => {
    const map = new Map<string, number>()
    for (const r of rows) {
      const key = (r.res || '未知').trim() || '未知'
      map.set(key, (map.get(key) || 0) + 1)
    }
    return [...map.entries()].sort((a, b) => b[1] - a[1]).slice(0, 6)
  }, [rows])

  const activity = React.useMemo(() => {
    const out: Array<{ key: string; label: string; count: number }> = []
    const counts = new Map<string, number>()
    for (const r of rows) {
      if (r.first_seen) counts.set(r.first_seen.slice(0, 10), (counts.get(r.first_seen.slice(0, 10)) || 0) + 1)
    }
    for (let i = 13; i >= 0; i--) {
      const d = new Date()
      d.setDate(d.getDate() - i)
      const key = dateKey(d)
      out.push({ key, label: `${d.getMonth() + 1}/${d.getDate()}`, count: counts.get(key) || 0 })
    }
    return out
  }, [rows])
  const activityMax = Math.max(1, ...activity.map(d => d.count))

  const recent = React.useMemo(
    () => [...rows].sort((a, b) => (b.first_seen || '').localeCompare(a.first_seen || '')).slice(0, 6),
    [rows],
  )
  const mostPlayed = React.useMemo(
    () => [...rows]
      .filter(r => Number(r.play_count) > 0)
      .sort((a, b) => Number(b.play_count) - Number(a.play_count))
      .slice(0, 5),
    [rows],
  )
  const favorites = React.useMemo(
    () => rows.filter(r => r.fav === '1').sort((a, b) => (b.first_seen || '').localeCompare(a.first_seen || '')).slice(0, 4),
    [rows],
  )
  const continueRows = React.useMemo(
    () => rows.filter(r => r.last_play).sort((a, b) => (b.last_play || '').localeCompare(a.last_play || '')).slice(0, 4),
    [rows],
  )

  const download = data?.download
  const active = download?.items?.filter(i => i.state === 'downloading' || i.state === 'queued' || i.state === 'paused').length || 0
  const downloadPercent = download?.total ? Math.round(((download.done || 0) / download.total) * 100) : 0
  const sourceMax = sourceStats[0]?.[1] || 1
  const resMax = resStats[0]?.[1] || 1

  return (
    <div className="space-y-3 p-4">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <div className="flex items-center gap-1.5 text-[10px] tracking-[.18em] text-[color:var(--subtle)]">
            <Sparkles className="size-3" /> OVERVIEW
          </div>
          <div className="mt-0.5 text-[16px] font-semibold">概览</div>
          <div className="mt-1 text-[10px] text-[color:var(--muted)]">从当前台账、播放记录和下载队列里汇总的常用数据。</div>
        </div>
        <Button size="sm" variant="outline" onClick={() => navigate('/queue')}>
          <Download className="size-3.5" /> 下载队列
        </Button>
      </div>

      <div className="grid grid-cols-[repeat(auto-fit,minmax(128px,1fr))] gap-2">
        <StatCard icon={Library} label="总条目" value={formatCount(stats?.total || 0)} sub={`今日 +${stats?.todayAdded || 0}`} onClick={() => navigate('/')} />
        <StatCard icon={FolderCheck} label="已下载" value={formatCount(stats?.done || 0)} sub={`${Number(stats?.diskGB || 0).toFixed(1)} GB`} tone="primary" onClick={() => navigate('/')} />
        <StatCard icon={Download} label="待下载" value={formatCount(stats?.new || 0)} sub={`缺失 ${stats?.missing || 0}`} onClick={() => navigate('/')} />
        <StatCard icon={Heart} label="收藏" value={formatCount(stats?.favorites || 0)} tone="danger" onClick={() => navigate('/')} />
        <StatCard icon={Clock3} label="观看时长" value={`${Number(stats?.hours || 0).toFixed(1)}h`} sub={`已看 ${stats?.watched || 0}`} />
        <StatCard icon={CalendarDays} label="待看" value={formatCount(stats?.toWatch || 0)} sub={`未看 ${stats?.new || 0}`} onClick={() => navigate('/')} />
      </div>

      <div className="grid grid-cols-1 gap-3 min-[1100px]:grid-cols-3">
        <Card className="p-4 min-[1100px]:col-span-2">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="text-[11.5px] font-medium">最近入库</div>
              <div className="text-[9.5px] text-[color:var(--subtle)]">按 first_seen 倒序</div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => navigate('/')}>全部 <ArrowRight className="size-3.5" /></Button>
          </div>
          {recent.length ? (
            <div className="mt-3 grid grid-cols-3 gap-2 max-[700px]:grid-cols-2">
              {recent.map(row => (
                <button
                  key={row.token}
                  type="button"
                  onClick={() => openPreview(row.token)}
                  className="group overflow-hidden rounded-lg border border-[color:var(--border)] bg-[color:var(--surface-2)] text-left transition-colors hover:border-[color:var(--border-strong)]"
                >
                  <Cover row={row} className="aspect-video w-full" />
                  <div className="p-2">
                    <div className="line-clamp-2 text-[10px] leading-4">{row.title || row.token}</div>
                    <div className="mt-1 flex items-center justify-between gap-1 text-[8.5px] text-[color:var(--subtle)]">
                      <span className="truncate">{sourceName.get(row.source) || row.site || row.source}</span>
                      <span className="shrink-0">{row.duration || ''}</span>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          ) : (
            <div className="py-10 text-center text-[10px] text-[color:var(--subtle)]">还没有台账数据，先去抓取或导入。</div>
          )}
        </Card>

        <Card className="p-4">
          <div className="flex items-center justify-between">
            <div className="text-[11.5px] font-medium">下载队列</div>
            <Download className="size-3.5 text-[color:var(--primary-2)]" />
          </div>
          <div className="mt-3 text-[22px] font-semibold">{active}</div>
          <div className="text-[9.5px] text-[color:var(--subtle)]">
            {download?.running ? (download.paused ? '已暂停' : '下载中') : '队列空闲'}
            {download?.failed ? ` · ${download.failed} 失败` : ''}
          </div>
          <Progress value={downloadPercent} className="mt-3" />
          <div className="mt-1 flex justify-between text-[9px] text-[color:var(--subtle)]">
            <span>{download?.done || 0}/{download?.total || 0} 完成</span>
            <span>{downloadPercent}%</span>
          </div>
          <Button size="sm" variant="outline" className="mt-3 w-full" onClick={() => navigate('/queue')}>
            打开队列 <ArrowRight className="size-3.5" />
          </Button>
        </Card>

        <Card className="p-4">
          <div className="flex items-center gap-1.5 text-[11.5px] font-medium"><TrendingUp className="size-3.5 text-[color:var(--primary-2)]" /> 近 14 天入库</div>
          <div className="mt-3 flex h-24 items-end gap-1">
            {activity.map(d => (
              <div key={d.key} className="group flex h-full flex-1 flex-col justify-end" title={`${d.key} · ${d.count} 条`}>
                <div
                  className={cn('w-full rounded-t-[3px] bg-[color:var(--primary)] opacity-70 group-hover:opacity-100', d.count === 0 && 'opacity-15')}
                  style={{ height: `${percent(d.count, activityMax)}%` }}
                />
              </div>
            ))}
          </div>
          <div className="mt-1 flex justify-between text-[8px] text-[color:var(--subtle)]"><span>{activity[0]?.label}</span><span>{activity[activity.length - 1]?.label}</span></div>
        </Card>

        <Card className="p-4">
          <div className="text-[11.5px] font-medium">常看来源</div>
          <div className="mt-3 space-y-2">
            {sourceStats.slice(0, 6).map(([key, count]) => (
              <div key={key}>
                <div className="flex justify-between gap-2 text-[9.5px]">
                  <span className="truncate text-[color:var(--muted)]">{sourceName.get(key) || key}</span>
                  <span className="shrink-0 text-[color:var(--subtle)]">{count}</span>
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-[color:var(--surface-2)]">
                  <div className="h-full rounded-full bg-[color:var(--primary)]" style={{ width: `${percent(count, sourceMax)}%` }} />
                </div>
              </div>
            ))}
            {!sourceStats.length ? <div className="py-6 text-center text-[9.5px] text-[color:var(--subtle)]">暂无来源数据</div> : null}
          </div>
        </Card>

        <Card className="p-4">
          <div className="text-[11.5px] font-medium">分辨率分布</div>
          <div className="mt-3 space-y-2">
            {resStats.map(([key, count]) => (
              <div key={key}>
                <div className="flex justify-between gap-2 text-[9.5px]">
                  <span className="truncate text-[color:var(--muted)]">{key}</span>
                  <span className="shrink-0 text-[color:var(--subtle)]">{count}</span>
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-[color:var(--surface-2)]">
                  <div className="h-full rounded-full bg-[color:var(--success)]" style={{ width: `${percent(count, resMax)}%` }} />
                </div>
              </div>
            ))}
            {!resStats.length ? <div className="py-6 text-center text-[9.5px] text-[color:var(--subtle)]">暂无分辨率数据</div> : null}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center gap-1.5 text-[11.5px] font-medium"><PlayCircle className="size-3.5 text-[color:var(--primary-2)]" /> 最常播放</div>
          <div className="mt-3 space-y-2">
            {mostPlayed.map((row, i) => (
              <button key={row.token} type="button" onClick={() => openPreview(row.token)} className="flex w-full items-center gap-2 text-left">
                <span className="w-4 shrink-0 text-[9px] text-[color:var(--subtle)]">{i + 1}</span>
                <Cover row={row} className="h-8 w-12 shrink-0 rounded" />
                <span className="min-w-0 flex-1 truncate text-[10px]">{row.title || row.token}</span>
                <span className="shrink-0 text-[9px] text-[color:var(--primary-2)]">{row.play_count} 次</span>
              </button>
            ))}
            {!mostPlayed.length ? <div className="py-6 text-center text-[9.5px] text-[color:var(--subtle)]">还没有播放记录</div> : null}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center gap-1.5 text-[11.5px] font-medium"><Heart className="size-3.5 text-[color:var(--danger)]" /> 收藏 / 待看</div>
          <div className="mt-3 space-y-2">
            {(favorites.length ? favorites : continueRows).slice(0, 4).map(row => (
              <button key={row.token} type="button" onClick={() => openPreview(row.token)} className="flex w-full items-center gap-2 text-left">
                <Cover row={row} className="h-8 w-12 shrink-0 rounded" />
                <span className="min-w-0 flex-1 truncate text-[10px]">{row.title || row.token}</span>
                <span className="shrink-0 text-[8.5px] text-[color:var(--subtle)]">{row.fav === '1' ? '收藏' : '待看'}</span>
              </button>
            ))}
            {!favorites.length && !continueRows.length ? <div className="py-6 text-center text-[9.5px] text-[color:var(--subtle)]">收藏或待看后，这里会显示常用入口</div> : null}
          </div>
        </Card>
      </div>
    </div>
  )
}
