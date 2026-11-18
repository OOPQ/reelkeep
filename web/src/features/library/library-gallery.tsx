import * as React from 'react'
import { Check, Download, Eye, Heart } from 'lucide-react'
import type { DownloadItem, Row } from '@/types'
import { cn, formatCount, formatDate, rowTitle } from '@/lib/utils'
import { useUiStore } from '@/store/ui'
import { Card, CardBody, CardFooter, CardMedia, CardMeta, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { IconButton } from '@/components/ui/icon-button'
import { RatingInput } from '@/components/rating-input'
import { CoverImage } from '@/components/cover-image'
import { downloadButtonLabel, isDownloadBusy, DownloadProgress } from './download-status'
import { RowContextMenu, type RowMenuActions } from './row-context-menu'

type Props = {
  rows: Row[]
  downloads: Map<string, DownloadItem>
  selected: string[]
  onToggle: (token: string) => void
  onOpen: (token: string, autoplay?: boolean) => void
  onFav: (row: Row) => void
  onRate: (row: Row, value: number) => void
  onDownload: (row: Row) => void
  menu: RowMenuActions
}

export function LibraryGallery({ rows, downloads, selected, onToggle, onOpen, onFav, onRate, onDownload, menu }: Props) {
  const density = useUiStore(s => s.density)
  const multi = useUiStore(s => s.multi)
  const minWidth = density === 'large' ? 220 : density === 'compact' ? 150 : 182

  return (
    <div
      className="grid gap-3"
      style={{ gridTemplateColumns: `repeat(auto-fill,minmax(${minWidth}px,1fr))` }}
    >
      {rows.map(row => {
        const isSelected = selected.includes(row.token)
        const dl = downloads.get(row.token)
        return (
          <RowContextMenu key={row.token} row={row} actions={menu}>
            <Card
              className={cn(
                'group cursor-pointer overflow-hidden transition-[border-color,transform] hover:-translate-y-0.5 hover:border-[color:var(--border-strong)] [content-visibility:auto] [contain-intrinsic-size:340px]',
                isSelected && 'border-[color:var(--primary)]',
              )}
              onClick={() => multi ? onToggle(row.token) : onOpen(row.token, true)}
            >
              <CardMedia className="aspect-[3/4]">
                <CoverImage token={row.token} full alt={rowTitle(row)} className="absolute inset-0" />

                {multi ? (
                  <div className="absolute left-2 top-2 z-10" onClick={e => e.stopPropagation()}>
                    <Checkbox
                      aria-label={`选择 ${rowTitle(row)}`}
                      checked={isSelected}
                      onCheckedChange={() => onToggle(row.token)}
                      className="size-[18px] border-white/70 bg-white/90 text-[#111] shadow-lg backdrop-blur data-[state=checked]:bg-[color:var(--primary)] data-[state=checked]:text-white"
                    />
                  </div>
                ) : null}

                <span className="absolute bottom-2 left-2 z-10 rounded-md border border-white/15 bg-black/55 px-2 py-0.5 text-[12px] font-semibold text-white/90 backdrop-blur">
                  {row.duration || '—'}
                </span>

                {dl && ['queued', 'downloading', 'paused'].includes(dl.state) ? (
                  <div className="absolute inset-x-2 bottom-2 z-10 rounded-md border border-white/15 bg-black/60 px-2 py-1 backdrop-blur">
                    <DownloadProgress item={dl} />
                  </div>
                ) : null}
              </CardMedia>

              <CardBody className="pb-2">
                <CardTitle className="text-[11.5px]">{row.title?.trim() || row.token}</CardTitle>
                <CardMeta>
                  {row.duration || '—'} · {row.res || '—'} · {formatDate(row.first_seen)}
                </CardMeta>
                <div className="mt-1.5 flex min-w-0 items-center gap-2 text-[9px] text-[color:var(--subtle)]">
                  <span className="inline-flex shrink-0 items-center gap-1"><Eye className="size-3" /> {formatCount(row.views)}</span>
                  <span className="inline-flex shrink-0 items-center gap-1"><Heart className="size-3" /> {formatCount(row.likes)}</span>
                  {row.tags?.length ? (
                    <div className="ml-auto flex min-w-0 items-center justify-end gap-1 overflow-hidden">
                      {row.tags.slice(0, 2).map(tag => (
                        <span
                          key={tag.id}
                          className="inline-flex min-w-0 max-w-[64px] items-center gap-1 rounded-[5px] border px-1.5 py-[2px] text-[8px] leading-none"
                          style={{
                            borderColor: `color-mix(in srgb, ${tag.color || 'var(--primary)'} 32%, transparent)`,
                            background: `color-mix(in srgb, ${tag.color || 'var(--primary)'} 11%, transparent)`,
                            color: tag.color || 'var(--primary-2)',
                          }}
                          title={tag.name}
                        >
                          <span className="size-1 shrink-0 rounded-full bg-current opacity-75" />
                          <span className="truncate">{tag.name}</span>
                        </span>
                      ))}
                      {row.tags.length > 2 ? (
                        <span className="shrink-0 rounded-[5px] border border-[color:var(--border)] bg-[color:var(--surface-2)] px-1.5 py-[2px] text-[8px] leading-none text-[color:var(--subtle)]">
                          +{row.tags.length - 2}
                        </span>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </CardBody>

              <CardFooter className="gap-1 overflow-hidden px-2 py-1.5">
                <div className="min-w-0 shrink" onClick={e => e.stopPropagation()}>
                  <RatingInput compact value={row.rating} onChange={value => onRate(row, value)} />
                </div>
                <div className="flex shrink-0 items-center gap-0.5" onClick={e => e.stopPropagation()}>
                  {row.file_name ? (
                    <IconButton label="打开文件位置" size="sm" className="text-[color:var(--success)]" onClick={() => menu.reveal(row)}>
                      <Check className="size-3.5" />
                    </IconButton>
                  ) : (
                    <IconButton
                      label={downloadButtonLabel(row, dl)}
                      size="sm"
                      disabled={!!dl && isDownloadBusy(row, dl)}
                      title={dl?.error || downloadButtonLabel(row, dl)}
                      onClick={() => onDownload(row)}
                    >
                      <Download className="size-3.5" />
                    </IconButton>
                  )}
                  <IconButton
                    label={row.fav === '1' ? '取消收藏' : '收藏'}
                    size="sm"
                    className={cn(row.fav === '1' && 'text-[color:var(--danger)]')}
                    onClick={() => onFav(row)}
                  >
                    <Heart className={cn('size-3.5', row.fav === '1' && 'fill-current')} />
                    </IconButton>
                </div>
              </CardFooter>
            </Card>
          </RowContextMenu>
        )
      })}
    </div>
  )
}
