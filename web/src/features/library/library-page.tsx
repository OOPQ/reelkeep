import * as React from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { isDownloadBusy } from './download-status'
import {
  Archive,
  CalendarClock,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Clock3,
  Columns3,
  Download,
  FolderCheck,
  FolderX,
  Heart,
  LayoutGrid,
  Layers,
  Library,
  ListChecks,
  Link2,
  MoreHorizontal,
  Plus,
  RotateCcw,
  Search,
  SlidersHorizontal,
  Sparkles,
  Table2,
  X,
} from 'lucide-react'
import { useAppState } from '@/features/use-app-state'
import { confirmAction } from '@/store/confirm'
import { useLibraryActions } from './use-library-actions'
import { useUiStore } from '@/store/ui'
import { usePreview } from '@/features/preview/preview-provider'
import { TABLE_COL_LABELS, type TableColKey } from '@/store/ui'
import { cn, formatCount } from '@/lib/utils'
import { post } from '@/lib/api'
import { Progress } from '@/components/ui/progress'
import type { DownloadItem, Row } from '@/types'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import { StatCard } from '@/components/stat-card'
import { LibraryTable } from './library-table'
import { LibraryGallery } from './library-gallery'
import { filterRows, sortRows, type LibraryFilters, type SortDir, type SortKey } from './library-filters'
import { useLibraryRows, useRowTokens } from './use-library-rows'
import { SelectionBar } from './selection-bar'
import { CollectionPicker } from './collection-picker'
import { TagPicker } from './tag-picker'
import { LinkScrapeDialog, ScrapeDialog } from './scrape-dialog'
import type { RowMenuActions } from './row-context-menu'

const STATUS_OPTIONS = [
  { value: 'all', label: '全部状态' },
  { value: 'new', label: '待下载' },
  { value: 'done', label: '已完成' },
  { value: 'failed', label: '下载失败' },
]

const FLAG_OPTIONS = [
  { value: 'none', label: '不限标记' },
  { value: 'fav', label: '★ 收藏' },
  { value: 'towatch', label: '待看' },
  { value: 'watched', label: '已看' },
  { value: 'have', label: '在磁盘' },
  { value: 'miss', label: '未下载' },
  { value: 'rated', label: '已评分' },
  { value: 'top', label: '高分 ≥4★' },
  { value: 'today', label: '今日新增' },
]

const RES_OPTIONS = [
  { value: 'all', label: '全部' },
  { value: '1080', label: '1080p+' },
  { value: '720', label: '720p+' },
  { value: '4k', label: '4K+' },
  { value: 'portrait', label: '竖屏' },
]

const SORT_OPTIONS = [
  { value: 'rank:asc', label: '排名从高到低' },
  { value: 'views:desc', label: '观看最多' },
  { value: 'likes:desc', label: '点赞最多' },
  { value: 'rating:desc', label: '评分最高' },
  { value: 'duration:desc', label: '时长最长' },
  { value: 'first_seen:desc', label: '最新入库' },
  { value: 'file_size:desc', label: '文件最大' },
]

const PER_OPTIONS = [
  { value: '25', label: '25' },
  { value: '50', label: '50' },
  { value: '100', label: '100' },
  { value: '200', label: '200' },
  { value: '0', label: '全部' },
]

const STATUS_LABELS: Record<string, string> = Object.fromEntries(STATUS_OPTIONS.map(o => [o.value, o.label]))
const RES_LABELS: Record<string, string> = Object.fromEntries(RES_OPTIONS.map(o => [o.value, o.label]))

function FilterChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <button
      type="button"
      onClick={onClear}
      className="inline-flex max-w-[220px] items-center gap-1 rounded-full border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2 py-0.5 text-[10px] text-[color:var(--muted)] transition-colors hover:border-[color:var(--border-strong)] hover:text-[color:var(--foreground)]"
      title={`清除筛选：${label}`}
    >
      <span className="truncate">{label}</span>
      <X className="size-2.5 shrink-0" />
    </button>
  )
}

type SavedView = {
  name: string; query: string; status: string; source: string; flag: string; res: string; minDur: string; sortKey: SortKey; sortDir: SortDir; per: number
}

export function LibraryPage() {
  const { data, isLoading, error, refetch, isFetching } = useAppState()
  const downloadMap = React.useMemo(() => new Map<string, DownloadItem>((data?.download?.items || []).map(item => [item.token, item])), [data?.download?.items])
  const actions = useLibraryActions()
  const [searchParams, setSearchParams] = useSearchParams()
  const batchMode = searchParams.get('batch') === '1'
  const qc = useQueryClient()
  const [batchTokens, setBatchTokens] = React.useState<string[]>(() => {
    try { return (JSON.parse(sessionStorage.getItem('pektino.batchTokens') || '[]') as string[]).filter(Boolean) } catch { return [] }
  })
  const batchSyncRef = React.useRef('')
  // 本批视图要跟着「新的一批」一起更新：任务一结束就换成本次的 token 并立刻拉一次最新状态。
  React.useEffect(() => {
    if (!batchMode) return
    const p = data?.job?.progress
    const fromJob = (p?.addedTokens || []).filter(Boolean)
    let stored: string[] = []
    try { stored = (JSON.parse(sessionStorage.getItem('pektino.batchTokens') || '[]') as string[]).filter(Boolean) } catch { }
    const next = fromJob.length ? fromJob : stored
    if (!next.length) return
    const key = `${p?.finishedAt || 0}:${next.join(',')}`
    if (key === batchSyncRef.current) return
    batchSyncRef.current = key
    setBatchTokens(next)
    try { sessionStorage.setItem('pektino.batchTokens', JSON.stringify(next)) } catch { }
    void qc.invalidateQueries({ queryKey: ['state'] })
  }, [batchMode, data?.job?.progress?.finishedAt, data?.job?.progress?.addedTokens, qc])

  const view = useUiStore(s => s.view)
  const setView = useUiStore(s => s.setView)
  const multi = useUiStore(s => s.multi)
  const setMulti = useUiStore(s => s.setMulti)
  const selected = useUiStore(s => s.selected)
  const setSelected = useUiStore(s => s.setSelected)
  const clearSelected = useUiStore(s => s.clearSelected)
  const toggleSelected = useUiStore(s => s.toggleSelected)
  const preview = usePreview()
  const previewToken = preview.session?.token ?? null

const collectionId = Number(searchParams.get('collection')) || null
const tagIds = React.useMemo(() => (searchParams.get('tags') || '')
  .split(',').map(Number).filter(n => Number.isFinite(n) && n > 0), [searchParams])
const [query, setQuery] = React.useState(searchParams.get('q') || '')
  const deferredQuery = React.useDeferredValue(query)
const [status, setStatus] = React.useState(searchParams.get('status') || 'all')
const [source, setSource] = React.useState(searchParams.get('source') || 'all')
const [flag, setFlag] = React.useState(searchParams.get('flag') || 'none')
const [res, setRes] = React.useState(searchParams.get('res') || 'all')
const [minDur, setMinDur] = React.useState(searchParams.get('minDur') || '')
const [sortKey, setSortKey] = React.useState<SortKey>((searchParams.get('sort')?.split(':')[0] as SortKey) || 'rank')
const [sortDir, setSortDir] = React.useState<SortDir>((searchParams.get('sort')?.split(':')[1] as SortDir) || 'asc')
const [per, setPer] = React.useState(Number(searchParams.get('per')) || 50)
  const [page, setPage] = React.useState(1)
  const [filterOpen, setFilterOpen] = React.useState(false)
  const [displayOpen, setDisplayOpen] = React.useState(false)
  const [moreOpen, setMoreOpen] = React.useState(false)
  const tableCols = useUiStore(s => s.tableCols)
  const setTableCol = useUiStore(s => s.setTableCol)
  const resetTableCols = useUiStore(s => s.resetTableCols)
  const [pickerTokens, setPickerTokens] = React.useState<string[] | null>(null)
  const [tagPickerTokens, setTagPickerTokens] = React.useState<string[] | null>(null)
  const [scrapeOpen, setScrapeOpen] = React.useState(false)
  const [linkOpen, setLinkOpen] = React.useState(false)
  const [savedViews, setSavedViews] = React.useState<SavedView[]>(() => {
    try { return JSON.parse(localStorage.getItem('pektino.views.v1') || '[]') } catch { return [] }
  })

  const tagsQuery = useQuery<{ items: Array<{ id: number; name: string; color: string }> }>({
    queryKey: ['tags'],
    queryFn: async () => {
      const res = await fetch('/api/tags', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取标签失败')
      return res.json()
    },
    staleTime: 5_000,
  })
  const collectionsQuery = useQuery<{ items: Array<{ id: number; name: string }> }>({
    queryKey: ['collections'],
    queryFn: async () => {
      const res = await fetch('/api/collections', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取合集失败')
      return res.json()
    },
    staleTime: 5_000,
  })
  const collectionItems = useQuery<{ tokens: string[] }>({
    queryKey: ['collection-items', collectionId],
    queryFn: async () => {
      const res = await fetch(`/api/collections/items?id=${collectionId}`, { cache: 'no-store' })
      if (!res.ok) throw new Error('读取合集失败')
      return res.json()
    },
    enabled: !!collectionId,
    staleTime: 3_000,
  })
  const collectionTokens = React.useMemo(() => new Set(collectionItems.data?.tokens || []), [collectionItems.data?.tokens])
  const collectionName = collectionsQuery.data?.items.find(c => c.id === collectionId)?.name
  const allTags = tagsQuery.data?.items || []
  const selectedTagNames = allTags.filter(t => tagIds.includes(t.id)).map(t => t.name)
  const selectedTags = allTags.filter(t => tagIds.includes(t.id))
  const serverMode = !collectionId && !batchMode && per > 0
  const baseRows = React.useMemo(() => collectionId ? (data?.rows || []).filter(r => collectionTokens.has(r.token)) : (data?.rows || []), [data?.rows, collectionId, collectionTokens])
  const filters: LibraryFilters = { q: deferredQuery, status, source, flag, res, minDur }
  const filtered = React.useMemo(() => {
    if (serverMode) return []
    return filterRows(baseRows, filters).filter(row => tagIds.every(id => (row.tags || []).some(t => t.id === id)))
  }, [baseRows, deferredQuery, status, source, flag, res, minDur, serverMode, tagIds])
  const batchSet = React.useMemo(() => batchMode ? new Set(batchTokens) : null, [batchMode, batchTokens])
  const batchRows = React.useMemo(() => serverMode ? [] : (batchSet ? filtered.filter(row => batchSet.has(row.token)) : filtered), [filtered, batchSet, serverMode])
  const sorted = React.useMemo(() => serverMode ? [] : sortRows(batchRows, sortKey, sortDir), [batchRows, sortKey, sortDir, serverMode])
  const pages = per > 0 ? Math.max(1, Math.ceil(sorted.length / per)) : 1
  const safePage = Math.min(page, pages)
  const visible = per > 0 ? sorted.slice((safePage - 1) * per, safePage * per) : sorted

  const serverQuery = useLibraryRows({
    q: deferredQuery,
    status,
    source,
    flag,
    res,
    minDur,
    sort: sortKey,
    dir: sortDir,
    offset: Math.max(0, (page - 1) * per),
    limit: per,
    tags: tagIds.join(','),
  }, serverMode)
  const displayRows = serverMode ? (serverQuery.data?.rows || []) : visible
  const displayTotal = serverMode ? (serverQuery.data?.total || 0) : sorted.length
  const displayPages = serverMode ? Math.max(1, Math.ceil(displayTotal / per)) : pages
  const displayPage = Math.min(page, displayPages)
  // 预览的上下翻页要在「整个筛选结果」里走，不能只走当前页。
  const rowTokensQuery = useRowTokens({
    q: deferredQuery,
    status,
    source,
    flag,
    res,
    minDur,
    sort: sortKey,
    dir: sortDir,
    tags: tagIds.join(','),
  }, serverMode)
  // 预览的翻页范围要覆盖「整个筛选结果」，而不是当前页：
  // - 服务端分页（媒体库）：用 token 接口返回的全量顺序；
  // - 本批新增 / 合集 / 全部：用内存里完整的 sorted，而不是 displayRows 那一页。
  const viewOrder = React.useMemo(() => {
    if (!serverMode) return sorted.map(r => r.token)
    const tokens = rowTokensQuery.data?.tokens
    return tokens && tokens.length ? tokens : displayRows.map(r => r.token)
  }, [serverMode, sorted, rowTokensQuery.data, displayRows])
  const viewRows = React.useMemo(() => {
    const map = new Map((data?.rows || []).map(row => [row.token, row]))
    return viewOrder.map(token => map.get(token)).filter((row): row is Row => Boolean(row))
  }, [data?.rows, viewOrder])
  const openPreviewList = React.useCallback((token: string, autoplay = false) => {
    const rows = [...viewRows]
    // 兜底：点进来的条目一定在翻页顺序里，避免 token 列表还没刷新到最新时预览不弹。
    if (!rows.some(row => row.token === token)) {
      const current = (data?.rows || []).find(row => row.token === token)
      if (current) rows.unshift(current)
    }
    if (rows.length) preview.open(rows, token, { autoplay, source: 'library' })
  }, [preview, viewRows, data?.rows])

  // token 列表 / 本批列表晚到时同步，但绝不能把当前打开的条目挤出顺序。
  React.useEffect(() => {
    if (preview.session?.source !== 'library' || !viewRows.length) return
    preview.refreshRows(viewRows)
  }, [preview.session?.source, viewRows, preview.refreshRows])

  React.useEffect(() => { setPage(1) }, [deferredQuery, status, source, flag, res, minDur, per, sortKey, sortDir, tagIds, collectionId, batchMode])
  React.useEffect(() => { if (page > displayPages) setPage(displayPages) }, [page, displayPages])

  React.useEffect(() => {
    const next = new URLSearchParams(window.location.search)
    const put = (key: string, value: string, fallback: string) => { if (value && value !== fallback) next.set(key, value); else next.delete(key) }
    put('q', query, '')
    put('status', status, 'all')
    put('source', source, 'all')
    put('flag', flag, 'none')
    put('res', res, 'all')
    put('minDur', minDur, '')
    put('tags', tagIds.join(','), '')
    put('collection', collectionId ? String(collectionId) : '', '')
    put('per', String(per), '50')
    put('sort', `${sortKey}:${sortDir}`, 'rank:asc')
    if (next.toString() !== window.location.search.replace(/^\?/, '')) setSearchParams(next, { replace: true })
  }, [query, status, source, flag, res, minDur, per, sortKey, sortDir, setSearchParams])

  const installedSourcesQuery = useQuery<{ items: Array<{ id: string; name: string; enabled?: boolean }> }>({
    queryKey: ['sources-all'],
    queryFn: async () => {
      const res = await fetch('/api/sources', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取源列表失败')
      return res.json()
    },
    staleTime: 5_000,
  })
  const filterSources = React.useMemo(() => {
    const all = installedSourcesQuery.data?.items
    if (all?.length) return all.map(s => ({ key: s.id, name: s.name || s.id }))
    return (data?.sources || []).map(s => ({ key: s.key, name: s.name || s.key }))
  }, [installedSourcesQuery.data?.items, data?.sources])
  const sourceOptions = React.useMemo(() => {
    const opts = [{ value: 'all', label: '全部来源' }, ...filterSources.map(s => ({ value: s.key, label: s.name }))]
    // 「其他」是兜底分类，不在源注册表里，但台账里可能有这类条目。
    if (!opts.some(o => o.value === 'other')) opts.push({ value: 'other', label: '其他' })
    return opts
  }, [filterSources])

  const filterCount = [flag !== 'none', res !== 'all', minDur !== '', tagIds.length > 0, !!collectionId].filter(Boolean).length
  const hasFilters = filterCount > 0 || query || status !== 'all' || source !== 'all' || flag !== 'none' || tagIds.length > 0 || !!collectionId

  const toggleTag = (id: number) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      const cur = (next.get('tags') || '').split(',').filter(Boolean).map(Number)
      const out = cur.includes(id) ? cur.filter(x => x !== id) : [...cur, id]
      if (out.length) next.set('tags', out.join(',')); else next.delete('tags')
      next.delete('collection')
      return next
    })
  }

  const reset = () => {
    setQuery('')
    setStatus('all')
    setSource('all')
    setFlag('none')
    setRes('all')
    setMinDur('')
    setSortKey('rank')
    setSortDir('asc')
    setPage(1)
    setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('tags'); n.delete('collection'); return n })
  }

  const saveView = () => {
    const name = window.prompt('保存当前筛选视图为：')?.trim()
    if (!name) return
    const view: SavedView = { name, query, status, source, flag, res, minDur, sortKey, sortDir, per }
    const next = [...savedViews.filter(v => v.name !== name), view]
    setSavedViews(next)
    try { localStorage.setItem('pektino.views.v1', JSON.stringify(next)) } catch { }
    toast.success('视图已保存')
  }

  const applyView = (name: string) => {
    const view = savedViews.find(v => v.name === name)
    if (!view) return
    setQuery(view.query)
    setStatus(view.status)
    setSource(view.source)
    setFlag(view.flag)
    setRes(view.res)
    setMinDur(view.minDur)
    setSortKey(view.sortKey)
    setSortDir(view.sortDir)
    setPer(view.per)
    setPage(1)
  }

  const sort = (key: SortKey) => {
    if (key === sortKey) setSortDir(d => (d === 'asc' ? 'desc' : 'asc'))
    else {
      setSortKey(key)
      setSortDir(key === 'rank' || key === 'title' ? 'asc' : 'desc')
    }
  }

  const togglePage = (checked: boolean) => {
    const pageTokens = displayRows.map(r => r.token)
    const pageSet = new Set(pageTokens)
    if (checked) setSelected([...new Set([...selected, ...pageTokens])])
    else setSelected(selected.filter(t => !pageSet.has(t)))
  }

  const onFav = (row: Row) => {
    actions.fav([row.token], row.fav !== '1')
  }

  const selectedRows = (data?.rows || []).filter(r => selected.includes(r.token))

  const menu: RowMenuActions = {
    open: openPreviewList,
    download: async row => {
      if (row.file_name || row.status === 'done') {
        toast.info('该条目已有本地文件')
        return
      }
      if (row.download_kind === 'torrent' || row.url?.toLowerCase().startsWith('magnet:')) {
        toast.error('暂不支持磁力链接')
        return
      }
      await actions.download([row.token])
    },
    fav: row => actions.fav([row.token], row.fav !== '1'),
    collection: row => setPickerTokens([row.token]),
    tag: row => setTagPickerTokens([row.token]),
    watched: row => actions.watched([row.token], !row.last_play),
    rate: (row, value) => actions.rate([row.token], value),
    recycle: async row => {
      if (await confirmAction({ title: '移入回收站', description: `删除「${row.title?.trim() || row.token}」？可在回收站恢复。`, confirmText: '移入回收站', danger: true })) {
        actions.recycle([row.token])
      }
    },
    copy: (row, kind) => actions.copy([row.token], data?.rows || [], kind),
    reveal: row => actions.reveal(row.token),
    markDone: row => actions.mark([row.token], 'done'),
  }

  const bulkDownload = async () => {
    const rows = (data?.rows || []).filter(r => selected.includes(r.token))
    const eligible = rows.filter(r => {
      if (r.file_name || r.status === 'done') return false
      const dl = downloadMap.get(r.token)
      if (dl && isDownloadBusy(r, dl)) return false
      return Boolean(r.url)
    })
    const noUrl = rows.filter(r => !r.file_name && r.status !== 'done' && !r.url).length
    if (!eligible.length) {
      toast.info(`没有可下载的条目（已有文件或已在队列中${noUrl ? `，${noUrl} 条无播放地址` : ''}）`)
      return
    }
    const skipped = rows.length - eligible.length
    await actions.download(eligible.map(r => r.token))
    if (skipped > 0) {
      toast.info(`已跳过 ${skipped} 条（已有文件 / 已在队列 / 无播放地址）`)
    }
  }
  const bulkRecycle = async () => {
    if (await confirmAction({ title: '移入回收站', description: `把已选 ${selected.length} 条移入回收站？可在回收站恢复。`, confirmText: '移入回收站', danger: true })) {
      actions.recycle(selected)
    }
  }

  // 抽卡要从「整个筛选结果里可播放的条目」抽，而不是当前这一页。
  // 服务端分页时按 200 条一页把全部结果拉回来。
  const fetchPlayablePool = React.useCallback(async (): Promise<Row[]> => {
    const out: Row[] = []
    const limit = 200
    let offset = 0
    let total = Infinity
    while (offset < total) {
      const qs = new URLSearchParams({
        q: deferredQuery,
        status,
        source,
        flag,
        res,
        minDur: String(minDur || 0),
        sort: sortKey,
        dir: sortDir,
        offset: String(offset),
        limit: String(limit),
        tags: tagIds.join(','),
      })
      const resp = await fetch(`/api/rows?${qs.toString()}`, { cache: 'no-store' })
      if (!resp.ok) throw new Error('读取媒体库失败')
      const data = await resp.json() as { rows?: Row[]; total?: number }
      const rows = data.rows || []
      total = Number(data.total) || 0
      for (const r of rows) {
        if (r.file_name) out.push(r)
      }
      if (!rows.length) break
      offset += rows.length
    }
    return out
  }, [deferredQuery, status, source, flag, res, minDur, sortKey, sortDir, tagIds])

  const gachaBusy = React.useRef(false)
  const gacha = async () => {
    if (gachaBusy.current) return
    let all: Row[]
    if (serverMode) {
      gachaBusy.current = true
      try {
        all = await fetchPlayablePool()
      } catch (e) {
        toast.error((e as Error).message)
        return
      } finally {
        gachaBusy.current = false
      }
    } else {
      const poolSource = sorted.length ? sorted : displayRows
      all = poolSource.filter(r => r.file_name)
    }
    if (!all.length) { toast.error('当前筛选结果里没有可播放的本地条目'); return }
    const pool = previewToken ? all.filter(r => r.token !== previewToken) : all
    const list = pool.length ? pool : all
    const picked = list[Math.floor(Math.random() * list.length)]
    preview.open(list, picked.token, { autoplay: true, source: 'gacha' })
  }

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || !multi) return
      if (document.querySelectorAll('[role="dialog"]').length > 0) return
      e.preventDefault()
      setMulti(false)
      clearSelected()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [multi, setMulti, clearSelected])

  React.useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || e.key.toLowerCase() !== 'r') return
      const target = e.target as HTMLElement | null
      if (target instanceof Element && target.closest('input,textarea,select,[contenteditable]')) return
      const dialogs = document.querySelectorAll('[role="dialog"]').length
      if (previewToken ? dialogs > 1 : dialogs > 0) return
      e.preventDefault()
      void gacha()
      const active = document.activeElement as HTMLElement | null
      if (active && active !== document.body) active.blur()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [previewToken, data?.rows])

  const addedTokens = React.useMemo(() => {
    const fromJob = (data?.job?.progress?.addedTokens || []).filter(Boolean)
    if (fromJob.length) return fromJob
    try {
      return (JSON.parse(sessionStorage.getItem('pektino.batchTokens') || '[]') as string[]).filter(Boolean)
    } catch {
      return []
    }
  }, [data?.job?.progress?.addedTokens])
  const enterBatch = () => {
    if (!addedTokens.length) return
    setQuery('')
    setStatus('all')
    setSource('all')
    setFlag('none')
    setRes('all')
    setMinDur('')
    try { sessionStorage.setItem('pektino.batchTokens', JSON.stringify(addedTokens)) } catch { }
    const next = new URLSearchParams(searchParams)
    next.set('batch', '1')
    setSearchParams(next)
  }
  const exitBatch = () => {
    const next = new URLSearchParams(searchParams)
    next.delete('batch')
    setSearchParams(next)
  }

  if (isLoading) return <LibrarySkeleton />

  if (error) {
    return (
      <div className="p-6">
        <div className="rounded-[var(--radius-card)] border border-[color:color-mix(in_srgb,var(--danger)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--danger)_5%,var(--surface))] p-6">
          <div className="text-[10px] tracking-[.6px] text-[color:var(--danger)]">API OFFLINE</div>
          <h2 className="mt-1 text-[16px] font-semibold">读取台账失败</h2>
          <p className="mt-2 max-w-[520px] text-[11.5px] leading-5 text-[color:var(--muted)]">
            {(error as Error).message}。确认 Reelkeep 仍在运行，然后点重试。
          </p>
          <Button className="mt-4" size="sm" variant="outline" onClick={() => refetch()} disabled={isFetching}>
            <RotateCcw className={cn('size-3.5', isFetching && 'animate-spin')} /> 重试
          </Button>
        </div>
      </div>
    )
  }

  const stats = data?.stats
  const job = data?.job as { running?: boolean; rank?: string; progress?: { phase?: string; done?: number; total?: number } } | undefined
  const sourceLabel = source === 'all' ? '全部来源' : sourceOptions.find(o => o.value === source)?.label || source
  const flagLabel = FLAG_OPTIONS.find(f => f.value === flag)?.label || '不限标记'

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-4">
      <div className="grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-2">
        <StatCard icon={Library} label="总数" value={formatCount(stats?.total || 0)} sub={`今日 +${stats?.todayAdded || 0}`} active={status === 'all' && flag === 'none'} onClick={() => { setStatus('all'); setFlag('none') }} />
        <StatCard icon={Download} label="待下载" value={formatCount(stats?.new || 0)} active={status === 'new'} onClick={() => { setStatus('new'); setFlag('none') }} />
        <StatCard icon={FolderCheck} label="已完成" value={formatCount(stats?.done || 0)} active={status === 'done'} onClick={() => { setStatus('done'); setFlag('none') }} />
        <StatCard icon={Archive} label="在磁盘" value={formatCount(stats?.downloaded || 0)} sub={`磁盘 ${Number(stats?.diskGB || 0).toFixed(1)} GB`} active={flag === 'have'} onClick={() => { setStatus('all'); setFlag('have') }} />
        <StatCard icon={FolderX} label="未下载" value={formatCount(stats?.missing || 0)} active={flag === 'miss'} onClick={() => { setStatus('all'); setFlag('miss') }} />
        <StatCard icon={Heart} label="收藏" value={formatCount(stats?.favorites || 0)} tone="danger" active={flag === 'fav'} onClick={() => { setStatus('all'); setFlag('fav') }} />
        <StatCard icon={Clock3} label="总时长(h)" value={Number(stats?.hours || 0).toFixed(1)} sub={`今日 +${Number(stats?.todayHours || 0).toFixed(1)}h`} />
      </div>

            <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[180px] flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-[color:var(--subtle)]" />
          <Input
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="查询：token / 标题 / 来源 / 分辨率"
            className="pl-8"
            aria-label="搜索条目"
          />
        </div>
        <Select value={status} onValueChange={setStatus} options={STATUS_OPTIONS} className="w-[120px]" aria-label="状态" />
        <Select value={source} onValueChange={setSource} options={sourceOptions} className="w-[120px]" aria-label="来源" />
        <Popover open={filterOpen} onOpenChange={setFilterOpen}>
          <PopoverTrigger asChild>
            <Button size="sm" variant="outline" className={cn(filterCount > 0 && 'border-[color:var(--primary)] text-[color:var(--primary-2)]')}>
              <SlidersHorizontal className="size-3.5" /> 筛选{filterCount > 0 ? ` ${filterCount}` : ''}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="max-h-[70vh] w-[340px] overflow-auto p-3">
            <div className="space-y-3">
              <div>
                <div className="ui-field-label mb-1.5">标记</div>
                <Select value={flag} onValueChange={setFlag} options={FLAG_OPTIONS} className="w-full" aria-label="标记" />
              </div>
              <div>
                <div className="ui-field-label mb-1.5">分辨率</div>
                <Select value={res} onValueChange={setRes} options={RES_OPTIONS} className="w-full" aria-label="分辨率" />
              </div>
              <div>
                <div className="ui-field-label mb-1.5">最短时长</div>
                <div className="flex items-center gap-2">
                  <Input type="number" min={0} value={minDur} onChange={e => setMinDur(e.target.value)} placeholder="不限" className="h-8" />
                  <span className="text-[10px] text-[color:var(--subtle)]">分钟</span>
                </div>
              </div>
              <div>
                <div className="ui-field-label mb-1.5">快捷排序</div>
                <Select
                  value={`${sortKey}:${sortDir}`}
                  onValueChange={value => {
                    const [k, d] = value.split(':') as [SortKey, SortDir]
                    setSortKey(k)
                    setSortDir(d)
                  }}
                  options={SORT_OPTIONS}
                  className="w-full"
                  aria-label="快捷排序"
                />
              </div>
              <div>
                <div className="ui-field-label mb-1.5">合集</div>
                <div className="max-h-[160px] space-y-0.5 overflow-auto">
                  <button
                    type="button"
                    className="flex w-full items-center rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]"
                    onClick={() => setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('collection'); return n })}
                  >
                    全部合集
                  </button>
                  {(collectionsQuery.data?.items || []).map(c => (
                    <button
                      key={c.id}
                      type="button"
                      className={cn('flex w-full items-center rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]', collectionId === c.id && 'bg-[color:var(--surface-2)] text-[color:var(--primary-2)]')}
                      onClick={() => setSearchParams(prev => { const n = new URLSearchParams(prev); n.set('collection', String(c.id)); return n })}
                    >
                      {c.name}
                    </button>
                  ))}
                </div>
              </div>
              <div>
                <div className="mb-1 flex items-center justify-between text-[10px] text-[color:var(--subtle)]">
                  <span className="ui-field-label">标签（多选为交集）</span>
                  {tagIds.length ? <button type="button" className="text-[color:var(--primary-2)]" onClick={() => setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('tags'); return n })}>清空</button> : null}
                </div>
                <div className="max-h-[180px] space-y-1 overflow-auto">
                  {allTags.map(tag => (
                    <label key={tag.id} className="flex cursor-pointer items-center gap-2 rounded-lg px-1.5 py-1 text-[11px] hover:bg-[color:var(--surface-2)]">
                      <Checkbox checked={tagIds.includes(tag.id)} onCheckedChange={() => toggleTag(tag.id)} />
                      <span className="size-2 rounded-full" style={{ background: tag.color || 'var(--primary)' }} />
                      <span className="truncate">{tag.name}</span>
                    </label>
                  ))}
                  {!allTags.length ? <div className="py-3 text-center text-[10px] text-[color:var(--subtle)]">还没有标签</div> : null}
                </div>
              </div>
              <Button
                size="sm"
                variant="ghost"
                className="w-full"
                onClick={() => {
                  setFlag('none')
                  setRes('all')
                  setMinDur('')
                  setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('tags'); n.delete('collection'); return n })
                }}
              >
                <RotateCcw className="size-3.5" /> 重置筛选
              </Button>
            </div>
          </PopoverContent>
        </Popover>
        <Popover open={displayOpen} onOpenChange={setDisplayOpen}>
          <PopoverTrigger asChild>
            <Button size="sm" variant="outline"><Columns3 className="size-3.5" /> 显示</Button>
          </PopoverTrigger>
          <PopoverContent className="w-[240px] p-3">
            <div className="space-y-2">
              {view === 'table' ? (
                <div className="space-y-1.5">
                  <div className="ui-field-label">表格列</div>
                  {(Object.keys(TABLE_COL_LABELS) as TableColKey[]).map(key => (
                    <label key={key} className="flex cursor-pointer items-center justify-between gap-2 text-[11px]">
                      <span>{TABLE_COL_LABELS[key]}</span>
                      <Checkbox checked={tableCols[key]?.visible !== false} onCheckedChange={v => setTableCol(key, { visible: v === true })} />
                    </label>
                  ))}
                  <Button size="sm" variant="ghost" className="w-full" onClick={resetTableCols}>
                    <RotateCcw className="size-3.5" /> 恢复默认列
                  </Button>
                </div>
              ) : null}
              <div className="space-y-1 border-t border-[color:var(--border)] pt-2">
                <Button size="sm" variant="ghost" className="w-full justify-start" onClick={saveView}>保存当前视图</Button>
                {savedViews.length ? savedViews.map(v => (
                  <Button key={v.name} size="sm" variant="ghost" className="w-full justify-start" onClick={() => { applyView(v.name); setDisplayOpen(false) }}>
                    {v.name}
                  </Button>
                )) : <div className="px-1 py-1 text-[10px] text-[color:var(--subtle)]">还没有保存的视图</div>}
              </div>
            </div>
          </PopoverContent>
        </Popover>
        <Button
          size="sm"
          variant={multi ? 'outline' : 'ghost'}
          onClick={() => setMulti(!multi)}
          aria-pressed={multi}
          aria-label="多选模式"
          title="多选模式"
          className={cn(multi && 'text-[color:var(--primary-2)]')}
        >
          <ListChecks className="size-3.5" />
        </Button>
        <div className="flex items-center gap-1 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] p-0.5">
          <Button size="sm" variant={view === 'table' ? 'outline' : 'ghost'} className="h-7 px-2" onClick={() => setView('table')} aria-pressed={view === 'table'}>
            <Table2 className="size-3.5" /> 表格
          </Button>
          <Button size="sm" variant={view === 'gallery' ? 'outline' : 'ghost'} className="h-7 px-2" onClick={() => setView('gallery')} aria-pressed={view === 'gallery'}>
            <LayoutGrid className="size-3.5" /> 画廊
          </Button>
        </div>
        <div className="flex items-center">
          <Button size="sm" variant="default" className="rounded-r-none" onClick={() => setScrapeOpen(true)}>
            <Plus className="size-3.5" /> 获取数据
          </Button>
          <Popover>
            <PopoverTrigger asChild>
              <Button size="sm" variant="default" className="rounded-l-none border-l border-[color:color-mix(in_srgb,var(--primary)_55%,transparent)] px-1.5" aria-label="更多抓取方式">
                <ChevronDown className="size-3.5" />
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[180px] p-1.5" align="end">
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]"
                onClick={() => setLinkOpen(true)}
              >
                <Link2 className="size-3.5" /> 链接抓取
              </button>
            </PopoverContent>
          </Popover>
        </div>
        <Popover open={moreOpen} onOpenChange={setMoreOpen}>
          <PopoverTrigger asChild>
            <Button size="sm" variant="outline" aria-label="更多操作">
              <MoreHorizontal className="size-3.5" /> 更多
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[200px] p-1.5" align="end">
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]"
              onClick={() => { setMoreOpen(false); void gacha() }}
              title="随机播放一部本地视频（快捷键 R）"
            >
              <Sparkles className="size-3.5" /> 抽一卡
              <span className="ml-auto text-[9px] text-[color:var(--subtle)]">R</span>
            </button>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]"
              onClick={() => { setMoreOpen(false); setStatus('all'); setFlag('towatch') }}
            >
              <CalendarClock className="size-3.5" /> 待看
            </button>
            {addedTokens.length && !batchMode ? (
              <button
                type="button"
                className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] hover:bg-[color:var(--surface-2)]"
                onClick={() => { setMoreOpen(false); enterBatch() }}
              >
                <Layers className="size-3.5" /> 本批新增 {addedTokens.length}
              </button>
            ) : null}
          </PopoverContent>
        </Popover>
      </div>

      {(query || status !== 'all' || source !== 'all' || flag !== 'none' || res !== 'all' || minDur !== '' || tagIds.length > 0 || collectionId) ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {query ? <FilterChip label={`搜索「${query}」`} onClear={() => setQuery('')} /> : null}
          {status !== 'all' ? <FilterChip label={STATUS_LABELS[status] || status} onClear={() => setStatus('all')} /> : null}
          {source !== 'all' ? <FilterChip label={sourceLabel} onClear={() => setSource('all')} /> : null}
          {flag !== 'none' ? <FilterChip label={flagLabel} onClear={() => setFlag('none')} /> : null}
          {res !== 'all' ? <FilterChip label={RES_LABELS[res] || res} onClear={() => setRes('all')} /> : null}
          {minDur !== '' ? <FilterChip label={`≥${minDur} 分钟`} onClear={() => setMinDur('')} /> : null}
          {collectionId && collectionName ? <FilterChip label={`合集「${collectionName}」`} onClear={() => setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('collection'); return n })} /> : null}
          {selectedTags.map(t => (
            <FilterChip
              key={t.id}
              label={`标签「${t.name}」`}
              onClear={() => setSearchParams(prev => {
                const n = new URLSearchParams(prev)
                const cur = (n.get('tags') || '').split(',').filter(Boolean).map(Number).filter(id => id !== t.id)
                if (cur.length) n.set('tags', cur.join(',')); else n.delete('tags')
                return n
              })}
            />
          ))}
          <button type="button" className="ml-1 text-[10px] text-[color:var(--primary-2)] hover:underline" onClick={reset}>重置全部</button>
        </div>
      ) : null}

      {job?.running ? (
        <div className="flex items-center gap-3 rounded-[10px] border border-[color:color-mix(in_srgb,var(--primary)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--primary)_6%,var(--surface))] px-3 py-2">
          <span className="shrink-0 text-[10px] text-[color:var(--primary-2)]">
            {job.progress?.phase || '抓取中'}{job.rank ? ` · ${job.rank}` : ''}
          </span>
          <Progress value={job.progress?.total ? Math.round(((job.progress.done || 0) / job.progress.total) * 100) : 0} className="max-w-[360px]" />
          <span className="text-[9.5px] text-[color:var(--subtle)]">
            {job.progress?.done || 0}/{job.progress?.total || 0}
          </span>
          <Button size="sm" variant="ghost" className="ml-auto h-7" onClick={async () => { await post('/api/job/cancel', {}); toast.success('已请求取消'); refetch() }}>取消任务</Button>
        </div>
      ) : null}

      {batchMode ? (
        <div className="flex items-center gap-2 rounded-[10px] border border-[color:color-mix(in_srgb,var(--primary)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--primary)_6%,var(--surface))] px-3 py-2 text-[10px] text-[color:var(--primary-2)]">
          临时视图：本批新增 {batchSet?.size || 0} 条
          <Button size="sm" variant="ghost" className="ml-auto h-7" onClick={exitBatch}>退出本批</Button>
        </div>
      ) : null}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="text-[10px] text-[color:var(--subtle)]">
            共 <b className="text-[color:var(--foreground)]">{displayTotal}</b> 条
            {source !== 'all' ? ` · ${sourceLabel}` : ''}
            {flag !== 'none' ? ` · ${flagLabel}` : ''}
            {query ? ` · 搜索“${query}”` : ''}
            {selectedTagNames.length ? ` · 标签「${selectedTagNames.join(' + ')}」` : ''}
            {tagIds.length ? (
              <button type="button" onClick={() => { setSearchParams(prev => { const n = new URLSearchParams(prev); n.delete('tags'); return n }) }} className="ml-1 text-[color:var(--primary-2)] hover:underline">清除标签</button>
            ) : null}
          </div>
          {isFetching ? <span className="text-[10px] text-[color:var(--primary-2)]">同步中…</span> : null}
        </div>

      <div className={cn('min-h-0 flex-1', view === 'gallery' && 'ui-scroll overflow-auto pr-0.5')}>
        {serverMode && serverQuery.isError ? (
          <div className="mb-2 flex items-center gap-2 rounded-[10px] border border-[color:color-mix(in_srgb,var(--danger)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--danger)_6%,var(--surface))] px-3 py-2 text-[10px] text-[color:var(--danger)]">
            列表加载失败：{(serverQuery.error as Error).message}
            <Button size="sm" variant="outline" className="ml-auto h-7" onClick={() => void serverQuery.refetch()} disabled={serverQuery.isFetching}>
              <RotateCcw className={cn('size-3.5', serverQuery.isFetching && 'animate-spin')} /> 重试
            </Button>
          </div>
        ) : null}
        {view === 'table' ? (
          <LibraryTable
            rows={displayRows}
            selected={selected}
            onToggle={toggleSelected}
            onTogglePage={togglePage}
            onOpen={openPreviewList}
            onFav={onFav}
            onRate={menu.rate}
            multi={multi}
            downloads={downloadMap}
            menu={menu}
            sortKey={sortKey}
            sortDir={sortDir}
            onSort={sort}
          />
        ) : (
            <LibraryGallery rows={displayRows} downloads={downloadMap} selected={selected} onToggle={toggleSelected} onOpen={openPreviewList} onFav={onFav} onRate={menu.rate} onDownload={menu.download} menu={menu} />
        )}
      </div>

      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-[color:var(--border)] pt-3">
        <div className="text-[11.5px] text-[color:var(--subtle)]">
          {displayTotal ? `${(displayPage - 1) * (per || displayTotal) + 1}-${Math.min(displayPage * (per || displayTotal), displayTotal)} / ${displayTotal}` : '0 / 0'}
        </div>
        <div className="flex items-center gap-1.5">
          <span className="mr-1 text-[10px] text-[color:var(--subtle)]">每页</span>
          <Select value={String(per)} onValueChange={v => setPer(Number(v))} options={PER_OPTIONS} className="w-[76px] h-8" aria-label="每页条数" />
          <Button size="icon-sm" variant="ghost" disabled={displayPage <= 1} onClick={() => setPage(1)} aria-label="第一页"><ChevronsLeft className="size-3.5" /></Button>
          <Button size="icon-sm" variant="ghost" disabled={displayPage <= 1} onClick={() => setPage(p => Math.max(1, p - 1))} aria-label="上一页"><ChevronLeft className="size-3.5" /></Button>
          {pageNumbers(displayPage, displayPages).map(n => (
            <Button key={n} size="icon-sm" variant={n === displayPage ? 'default' : 'ghost'} onClick={() => setPage(n)} aria-label={`第 ${n} 页`}>{n}</Button>
          ))}
          <Button size="icon-sm" variant="ghost" disabled={displayPage >= displayPages} onClick={() => setPage(p => Math.min(displayPages, p + 1))} aria-label="下一页"><ChevronRight className="size-3.5" /></Button>
          <Button size="icon-sm" variant="ghost" disabled={displayPage >= displayPages} onClick={() => setPage(displayPages)} aria-label="最后一页"><ChevronsRight className="size-3.5" /></Button>
        </div>
      </div>

      <SelectionBar
        count={multi ? selected.length : 0}
        onDownload={bulkDownload}
        onCollection={() => setPickerTokens(selected)}
        onTag={() => setTagPickerTokens(selected)}
        onRecycle={bulkRecycle}
        onFav={() => actions.fav(selected, true)}
        onWatched={() => actions.watched(selected, true)}
        onMarkDone={() => actions.mark(selected, 'done')}
        onCopy={kind => actions.copy(selected, data?.rows || [], kind)}
        onClear={clearSelected}
      />

      <CollectionPicker
        open={pickerTokens !== null}
        tokens={pickerTokens || []}
        onOpenChange={open => { if (!open) setPickerTokens(null) }}
      />
      <TagPicker
        open={tagPickerTokens !== null}
        tokens={tagPickerTokens || []}
        onOpenChange={open => { if (!open) setTagPickerTokens(null) }}
      />
      <ScrapeDialog open={scrapeOpen} onOpenChange={setScrapeOpen} sources={data?.sources || []} />
      <LinkScrapeDialog open={linkOpen} onOpenChange={setLinkOpen} />
    </div>
  )
}

function pageNumbers(page: number, pages: number) {
  const out: number[] = []
  const start = Math.max(1, Math.min(page - 2, pages - 4))
  const end = Math.min(pages, start + 4)
  for (let i = start; i <= end; i++) out.push(i)
  return out
}

function LibrarySkeleton() {
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 p-4">
      <div className="grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-2">
        {Array.from({ length: 7 }, (_, i) => <Skeleton key={i} className="h-[68px] rounded-[12px]" />)}
      </div>
      <div className="flex gap-2">
        <Skeleton className="h-[34px] flex-1" />
        <Skeleton className="h-[34px] w-[120px]" />
        <Skeleton className="h-[34px] w-[120px]" />
        <Skeleton className="h-[34px] w-[120px]" />
      </div>
      <Skeleton className="h-[420px] rounded-[14px]" />
    </div>
  )
}
