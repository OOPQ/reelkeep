import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { Check, Download, FileVideo, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { post } from '@/lib/api'
import { formatBytes, rowTitle } from '@/lib/utils'
import type { Row } from '@/types'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent } from '@/components/ui/dialog'

type TorrentEntry = { index: number; path: string; length: number }

export function TorrentPickerDialog({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const navigate = useNavigate()
  const [files, setFiles] = React.useState<TorrentEntry[]>([])
  const [selected, setSelected] = React.useState<number[]>([])
  const [loading, setLoading] = React.useState(false)
  const [busy, setBusy] = React.useState(false)
  const [error, setError] = React.useState('')

  React.useEffect(() => {
    if (!row) {
      setFiles([])
      setSelected([])
      setError('')
      return
    }
    let alive = true
    setLoading(true)
    setError('')
    setFiles([])
    setSelected([])
    void post<{ ok: boolean; error?: string; files?: TorrentEntry[] }>('/api/torrent/files', { url: row.url })
      .then(r => {
        if (!alive) return
        if (!r.ok) throw new Error(r.error || '读取磁力文件失败')
        const list = (r.files || []).filter(f => f.length > 0)
        setFiles(list)
        const video = list
          .filter(f => /\.(mp4|mkv|webm|mov|m4v|avi|ts|flv|wmv)$/i.test(f.path))
          .sort((a, b) => b.length - a.length)[0]
        if (video) setSelected([video.index])
      })
      .catch(e => { if (alive) setError((e as Error).message) })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [row])

  const toggle = (index: number, checked: boolean) => {
    setSelected(prev => checked ? [...new Set([...prev, index])] : prev.filter(i => i !== index))
  }

  const start = async () => {
    if (!row || !selected.length) return
    setBusy(true)
    try {
      const r = await post<{ ok: boolean; error?: string; started?: number }>('/api/download', {
        tokens: [row.token],
        files: selected,
      })
      if (!r.ok) throw new Error(r.error || '开始下载失败')
      toast.success('已加入磁力下载队列', { action: { label: '查看队列', onClick: () => navigate('/queue') } })
      onClose()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={!!row} onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent
        title={`选择下载内容${row ? ` · ${rowTitle(row)}` : ''}`}
        description="只下载勾选的文件；mp4 直接写入媒体目录，其他格式会用 ffmpeg 转封装。"
        className="w-[min(720px,calc(100vw-40px))]"
        footer={
          <div className="ml-auto flex gap-2">
            <Button size="sm" variant="outline" onClick={onClose}>取消</Button>
            <Button size="sm" onClick={() => void start()} disabled={busy || loading || !selected.length}>
              <Download className={busy ? 'size-3.5 animate-pulse' : 'size-3.5'} />
              {busy ? '加入中…' : `下载 ${selected.length} 个文件`}
            </Button>
          </div>
        }
      >
        <div className="max-h-[55vh] overflow-auto p-3">
          {loading ? (
            <div className="flex items-center gap-2 py-10 text-[11px] text-[color:var(--subtle)]">
              <RefreshCw className="size-4 animate-spin" /> 正在读取磁力元数据…
            </div>
          ) : error ? (
            <div className="rounded-lg border border-[color:var(--danger)]/40 bg-[color:var(--danger)]/5 p-3 text-[10px] text-[color:var(--danger)]">
              {error}
            </div>
          ) : (
            <div className="space-y-1">
              {files.map(file => {
                const checked = selected.includes(file.index)
                return (
                  <label key={file.index} className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-[color:var(--surface-2)]">
                    <Checkbox checked={checked} onCheckedChange={v => toggle(file.index, v === true)} />
                    <FileVideo className="size-3.5 shrink-0 text-[color:var(--subtle)]" />
                    <span className="min-w-0 flex-1 truncate text-[10.5px]" title={file.path}>{file.path}</span>
                    <span className="shrink-0 text-[9px] text-[color:var(--subtle)]">{formatBytes(file.length)}</span>
                    {checked ? <Check className="size-3 shrink-0 text-[color:var(--success)]" /> : null}
                  </label>
                )
              })}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
