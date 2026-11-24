import { Check, ChevronDown, Copy, Download, Eye, FileDown, Heart, Hash, Layers, Link2, Trash2, Type, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { CopyKind } from '@/lib/utils'

export function SelectionBar({
  count,
  onDownload,
  onCollection,
  onTag,
  onRecycle,
  onFav,
  onWatched,
  onMarkDone,
  onCopy,
  onClear,
}: {
  count: number
  onDownload: () => void
  onCollection: () => void
  onTag: () => void
  onRecycle: () => void
  onFav: () => void
  onWatched: () => void
  onMarkDone: () => void
  onCopy: (kind: CopyKind) => void
  onClear: () => void
}) {
  if (!count) return null
  return (
    <div className="fixed bottom-4 left-1/2 z-40 flex max-w-[calc(100vw-24px)] -translate-x-1/2 items-center gap-1 overflow-x-auto rounded-[12px] border border-[color:var(--border-strong)] bg-[color:var(--surface)] px-2 py-1.5 shadow-[0_20px_60px_rgba(0,0,0,.5)]">
      <span className="whitespace-nowrap px-1.5 text-[10px] text-[color:var(--muted)]">已选 <b className="text-[color:var(--foreground)]">{count}</b></span>
      <Button size="sm" variant="default" className="h-7 px-2" onClick={onDownload}><Download className="size-3.5" /> 下载</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onCollection}><Layers className="size-3.5" /> 合集</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onTag}><Layers className="size-3.5" /> 标签</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onFav}><Heart className="size-3.5" /> 收藏</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onWatched}><Eye className="size-3.5" /> 已看</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onMarkDone}><Check className="size-3.5" /> 完成</Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="sm" variant="ghost" className="h-7 px-2" aria-label="复制">
            <Copy className="size-3.5" /> 复制 <ChevronDown className="size-3" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="center" sideOffset={10}>
          <DropdownMenuItem onSelect={() => onCopy('download')}>
            <FileDown className="size-3.5" /> 复制文件下载链接
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onCopy('page')}>
            <Link2 className="size-3.5" /> 复制页面链接
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <Type className="size-3.5" /> 复制其他
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuItem onSelect={() => onCopy('title')}><Type className="size-3.5" /> 复制标题</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onCopy('token')}>
                <Hash className="size-3.5" /> 复制 Token
              </DropdownMenuItem>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>
      <Button size="sm" variant="danger" className="h-7 px-2" onClick={onRecycle}><Trash2 className="size-3.5" /> 删除</Button>
      <Button size="sm" variant="ghost" className="h-7 px-2" onClick={onClear}><X className="size-3.5" /> 清空</Button>
    </div>
  )
}
