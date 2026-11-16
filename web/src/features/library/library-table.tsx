import * as React from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Copy,
  Download,
  Play,
  Star,
} from 'lucide-react'
import type { Row } from '@/types'
import { cn, formatCount, formatDate, rowTitle } from '@/lib/utils'
import { Checkbox } from '@/components/ui/checkbox'
import { IconButton } from '@/components/ui/icon-button'
import { RatingInput } from '@/components/rating-input'
import { StatusBadge } from '@/components/status-badge'
import { CoverImage } from '@/components/cover-image'
import { useUiStore, type TableColKey } from '@/store/ui'
import type { SortDir, SortKey } from './library-filters'
import { RowContextMenu, type RowMenuActions } from './row-context-menu'

type Props = {
  rows: Row[]
  selected: string[]
  onToggle: (token: string) => void
  onTogglePage: (checked: boolean) => void
  onOpen: (token: string, autoplay?: boolean) => void
  onFav: (row: Row) => void
  onRate: (row: Row, value: number) => void
  sortKey: SortKey
  sortDir: SortDir
  onSort: (key: SortKey) => void
  multi: boolean
  menu: RowMenuActions
}

const COL_ORDER: TableColKey[] = ['cover', 'title', 'rank', 'duration', 'res', 'views', 'likes', 'rating', 'first_seen', 'status']
const SORTABLE: Partial<Record<TableColKey, SortKey>> = {
  title: 'title',
  rank: 'rank',
  duration: 'duration',
  views: 'views',
  likes: 'likes',
  rating: 'rating',
  first_seen: 'first_seen',
}
const COL_LABEL: Record<TableColKey, string> = {
  cover: '封面', title: '标题', rank: '排名', duration: '时长', res: '分辨率',
  views: '观看', likes: '点赞', rating: '评分', first_seen: '入库', status: '状态',
}

function ResizeHandle({ col }: { col: TableColKey }) {
  const setTableCol = useUiStore(s => s.setTableCol)
  const width = useUiStore(s => s.tableCols[col]?.width || 80)
  return (
    <span
      role="separator"
      aria-orientation="vertical"
      aria-label={`调整${COL_LABEL[col]}列宽`}
      onPointerDown={e => {
        e.preventDefault()
        e.stopPropagation()
        const startX = e.clientX
        const startWidth = width
        const move = (ev: PointerEvent) => {
          setTableCol(col, { width: Math.max(56, Math.min(640, startWidth + ev.clientX - startX)) })
        }
        const up = () => {
          document.body.style.cursor = ''
          window.removeEventListener('pointermove', move)
          window.removeEventListener('pointerup', up)
        }
        document.body.style.cursor = 'col-resize'
        window.addEventListener('pointermove', move)
        window.addEventListener('pointerup', up)
      }}
      className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize select-none hover:bg-[color:var(--primary)]/30"
    />
  )
}

function SortHead({
  col,
  sortKey,
  sortDir,
  onSort,
}: {
  col: TableColKey
  sortKey: SortKey
  sortDir: SortDir
  onSort: (key: SortKey) => void
}) {
  const key = SORTABLE[col]
  if (!key) return null
  const active = sortKey === key
  return (
    <th className="relative px-2 py-2 text-left font-medium">
      <button
        type="button"
        onClick={() => onSort(key)}
        className={cn('inline-flex items-center gap-1 text-[9.5px] tracking-[.4px] text-[color:var(--subtle)] hover:text-[color:var(--foreground)]', active && 'text-[color:var(--foreground)]')}
      >
        {COL_LABEL[col]}
        {active ? (
          sortDir === 'asc' ? <ArrowUp className="size-3" /> : <ArrowDown className="size-3" />
        ) : (
          <ChevronDown className="size-3 opacity-30" />
        )}
      </button>
      <ResizeHandle col={col} />
    </th>
  )
}

function StaticHead({ col }: { col: TableColKey }) {
  return (
    <th className="relative px-2 py-2 text-left text-[9.5px] tracking-[.4px] text-[color:var(--subtle)]">
      {COL_LABEL[col]}
      <ResizeHandle col={col} />
    </th>
  )
}

export function LibraryTable({
  rows,
  selected,
  onToggle,
  onTogglePage,
  onOpen,
  onFav,
  onRate,
  sortKey,
  sortDir,
  onSort,
  multi,
  menu,
}: Props) {
  const tableCols = useUiStore(s => s.tableCols)
  const pageTokens = rows.map(r => r.token)
  const selectedOnPage = pageTokens.filter(t => selected.includes(t)).length
  const allSelected = pageTokens.length > 0 && selectedOnPage === pageTokens.length
  const someSelected = selectedOnPage > 0 && !allSelected
  const [copied, setCopied] = React.useState<string | null>(null)
  const scrollRef = React.useRef<HTMLDivElement>(null)

  const visible = COL_ORDER.filter(key => tableCols[key]?.visible !== false)
  const minWidth = (multi ? 40 : 0) + visible.reduce((sum, key) => sum + (tableCols[key]?.width || 80), 0) + 104
  const colSpan = visible.length + (multi ? 1 : 0) + 1
  const virtual = rows.length > 80
  const rowHeight = 112
  const virtualizer = useVirtualizer({
    count: virtual ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 8,
    scrollMargin: 36,
  })
  const virtualItems = virtualizer.getVirtualItems()
  const padTop = virtualItems.length ? Math.max(0, virtualItems[0].start - 36) : 0
  const padBottom = virtualItems.length ? Math.max(0, virtualizer.getTotalSize() - (virtualItems[virtualItems.length - 1].end - 36)) : 0

  const copy = async (row: Row) => {
    try {
      await navigator.clipboard.writeText(row.page_url || row.url)
      setCopied(row.token)
      window.setTimeout(() => setCopied(c => (c === row.token ? null : c)), 1600)
    } catch (e) {}
  }

  const renderRow = (row: Row) => {
    const isSelected = selected.includes(row.token)
    return (
      <RowContextMenu key={row.token} row={row} actions={menu}>
        <tr
          className={cn(
            'group border-b border-[color:color-mix(in_srgb,var(--border)_65%,transparent)] transition-colors last:border-0 hover:bg-[color:color-mix(in_srgb,var(--primary)_4%,transparent)]',
            isSelected && 'bg-[color:color-mix(in_srgb,var(--primary)_6%,transparent)]',
          )}
          style={virtual ? { height: rowHeight } : undefined}
        >
          {multi ? (
            <td className="px-2 py-2.5 text-center align-middle">
              <div className="flex items-center justify-center">
                <Checkbox aria-label={`选择 ${rowTitle(row)}`} checked={isSelected} onCheckedChange={() => onToggle(row.token)} />
              </div>
            </td>
          ) : null}
          {visible.includes('cover') ? (
            <td className="px-2 py-2.5">
              <button type="button" onClick={() => onOpen(row.token, true)} className="block" aria-label="打开预览">
                <CoverImage token={row.token} alt={rowTitle(row)} className="h-[86px] w-[64px] rounded-[7px]" />
              </button>
            </td>
          ) : null}
          {visible.includes('title') ? (
            <td className="max-w-0 px-2 py-2.5">
              <button type="button" onClick={() => onOpen(row.token, true)} className="block max-w-full text-left">
                <div className="truncate text-[11.5px] font-medium text-[color:var(--foreground)] hover:text-[color:var(--primary-2)]">
                  {row.title?.trim() || row.token}
                </div>
                <div className="mt-0.5 truncate text-[9.5px] text-[color:var(--subtle)]">
                  <span className="ui-mono">{row.title?.trim() ? row.token : row.source || row.site || '—'}</span>
                  <span className="mx-1.5 opacity-40">·</span>
                  {formatDate(row.first_seen)}
                </div>
              </button>
            </td>
          ) : null}
          {visible.includes('rank') ? <td className="px-2 py-2.5 text-[color:var(--muted)]">{row.rank || '—'}</td> : null}
          {visible.includes('duration') ? <td className="px-2 py-2.5 text-[color:var(--muted)]">{row.duration || '—'}</td> : null}
          {visible.includes('res') ? <td className="px-2 py-2.5 text-[10px] text-[color:var(--muted)]">{row.res || '—'}</td> : null}
          {visible.includes('views') ? <td className="px-2 py-2.5 text-[10px] text-[color:var(--muted)]">{formatCount(row.views)}</td> : null}
          {visible.includes('likes') ? <td className="px-2 py-2.5 text-[10px] text-[color:var(--muted)]">{formatCount(row.likes)}</td> : null}
          {visible.includes('rating') ? <td className="px-2 py-2.5"><RatingInput value={row.rating} onChange={value => onRate(row, value)} /></td> : null}
          {visible.includes('first_seen') ? <td className="px-2 py-2.5 text-[10px] text-[color:var(--muted)]">{formatDate(row.first_seen)}</td> : null}
          {visible.includes('status') ? <td className="px-2 py-2.5"><StatusBadge status={row.status} /></td> : null}
          <td className="sticky right-0 border-l border-[color:var(--border)] bg-[color:var(--surface)] px-2 py-2.5 group-hover:bg-[color:color-mix(in_srgb,var(--primary)_4%,var(--surface))]">
            <div className="flex items-center justify-center gap-0.5 opacity-70 transition-opacity group-hover:opacity-100">
              <IconButton label="预览 / 播放" size="sm" onClick={() => onOpen(row.token, true)}>
                <Play className="size-3.5" />
              </IconButton>
              <IconButton
                label={row.fav === '1' ? '取消收藏' : '收藏'}
                size="sm"
                className={row.fav === '1' ? 'text-[color:var(--danger)]' : 'text-[color:var(--muted)]'}
                onClick={() => onFav(row)}
              >
                <Star className={cn('size-3.5', row.fav === '1' && 'fill-current')} />
              </IconButton>
              <IconButton label={copied === row.token ? '已复制' : '复制链接'} size="sm" onClick={() => copy(row)}>
                <Copy className={cn('size-3.5', copied === row.token && 'text-[color:var(--success)]')} />
              </IconButton>
              <IconButton label="下载" size="sm" onClick={() => menu.download(row)}>
                <Download className="size-3.5" />
              </IconButton>
            </div>
          </td>
        </tr>
      </RowContextMenu>
    )
  }

  return (
    <div ref={scrollRef} className="h-full min-h-0 overflow-auto rounded-[var(--radius-card)] border border-[color:var(--border)] bg-[color:var(--surface)]">
      <table className="w-full table-fixed border-collapse text-[11.5px]" style={{ minWidth }}>
        <colgroup>
          {multi ? <col style={{ width: 40 }} /> : null}
          {visible.map(key => <col key={key} style={{ width: tableCols[key]?.width || 80 }} />)}
          <col style={{ width: 104 }} />
        </colgroup>
        <thead className="sticky top-0 z-10 bg-[color:var(--surface-2)]">
          <tr className="border-b border-[color:var(--border)]">
            {multi ? (
              <th className="w-10 px-2 py-2 text-center align-middle">
                <div className="flex items-center justify-center">
                  <Checkbox
                    aria-label="全选本页"
                    checked={allSelected ? true : someSelected ? 'indeterminate' : false}
                    onCheckedChange={checked => onTogglePage(checked === true)}
                  />
                </div>
              </th>
            ) : null}
            {visible.map(key => (
              SORTABLE[key] ? (
                <SortHead key={key} col={key} sortKey={sortKey} sortDir={sortDir} onSort={onSort} />
              ) : (
                <StaticHead key={key} col={key} />
              )
            ))}
            <th className="sticky right-0 z-20 w-[104px] border-l border-[color:var(--border)] bg-[color:var(--surface-2)] px-2 py-2 text-center text-[9.5px] tracking-[.4px] text-[color:var(--subtle)]">操作</th>
          </tr>
        </thead>
        <tbody>
          {virtual ? (
            <>
              {padTop > 0 ? <tr style={{ height: padTop }}><td colSpan={colSpan} /></tr> : null}
              {virtualItems.map(item => renderRow(rows[item.index]))}
              {padBottom > 0 ? <tr style={{ height: padBottom }}><td colSpan={colSpan} /></tr> : null}
            </>
          ) : rows.map(renderRow)}
        </tbody>
      </table>
      {!rows.length ? (
        <div className="grid min-h-[220px] place-items-center px-6 text-center">
          <div>
            <div className="text-[12px] font-medium">没有符合条件的条目</div>
            <div className="mt-1 text-[10px] text-[color:var(--subtle)]">试试清空查询或筛选条件。</div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
