import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Download, FileVideo, Link2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import type { Source } from '@/types'
import { post } from '@/lib/api'
import { formatBytes } from '@/lib/utils'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export function ScrapeDialog({
  open,
  onOpenChange,
  sources,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  sources: Source[]
}) {
  const qc = useQueryClient()
  const scrapeSources = React.useMemo(() => sources
    .filter(s => s.key !== 'web' && s.key !== 'other' && (s.capabilities || []).includes('scrape')), [sources])
  const [mode, setMode] = React.useState('online')
  const [source, setSource] = React.useState('')
  const [rank, setRank] = React.useState('')
  const [limit, setLimit] = React.useState('50')
  const [incremental, setIncremental] = React.useState(true)
  const [skipDeleted, setSkipDeleted] = React.useState(true)
  const [path, setPath] = React.useState('')
  const [busy, setBusy] = React.useState(false)

  React.useEffect(() => {
    if (!open) return
    const first = scrapeSources[0]
    if (first && !scrapeSources.some(s => s.key === source)) setSource(first.key)
  }, [open, scrapeSources, source])

  const src = scrapeSources.find(s => s.key === source) || scrapeSources[0]
  const ranks = src?.ranks || []

  React.useEffect(() => {
    if (!src) return
    if (!ranks.some(r => r.key === rank)) setRank(ranks[0]?.key || 'default')
  }, [src, ranks, rank])

  const start = async () => {
    setBusy(true)
    try {
      if (mode === 'local') {
        if (!path.trim()) throw new Error('请填写本地视频文件夹')
        const r = await post<{ ok: boolean; error?: string }>('/api/import/local', { path: path.trim() })
        if (!r.ok) throw new Error(r.error || '导入失败')
        toast.success('已开始识别本地视频')
      } else {
        if (!source) throw new Error('请选择来源')
        const r = await post<{ ok: boolean; error?: string }>('/api/scrape', {
          source,
          rank,
          limit: Number(limit) || 50,
          incremental,
          skipDeleted,
        })
        if (!r.ok) throw new Error(r.error || '启动抓取失败')
        toast.success('已开始获取数据')
      }
      qc.invalidateQueries({ queryKey: ['state'] })
      try { sessionStorage.setItem('pektino.scrapePending', '1'); sessionStorage.setItem('pektino.scrapePendingAt', String(Date.now())) } catch { }
      onOpenChange(false)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="获取数据"
        className="w-[min(440px,calc(100vw-40px))]"
        footer={
          <div className="ml-auto flex gap-2">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>取消</Button>
            <Button size="sm" onClick={start} disabled={busy}>
              {busy ? <RefreshCw className="size-3.5 animate-spin" /> : <Link2 className="size-3.5" />}
              {mode === 'local' ? '识别本地视频' : '开始获取'}
            </Button>
          </div>
        }
      >
        <div className="space-y-3 p-4">
          <Tabs value={mode} onValueChange={setMode}>
            <TabsList>
              <TabsTrigger value="online">在线榜单</TabsTrigger>
              <TabsTrigger value="local">本地视频</TabsTrigger>
            </TabsList>
          </Tabs>

          {mode === 'online' ? (
            <>
              <div>
                <div className="ui-field-label mb-1.5">来源</div>
                <Select
                  value={source}
                  onValueChange={setSource}
                  options={scrapeSources.map(s => ({ value: s.key, label: s.name || s.key }))}
                  className="w-full"
                  aria-label="抓取来源"
                />
              </div>
              <div>
                <div className="ui-field-label mb-1.5">榜单</div>
                <Select
                  value={rank}
                  onValueChange={setRank}
                  options={ranks.map(r => ({ value: r.key, label: r.group ? `${r.group} · ${r.label}` : r.label }))}
                  className="w-full"
                  aria-label="抓取榜单"
                />
              </div>
              <div>
                <div className="ui-field-label mb-1.5">抓取数量（条）</div>
                <Input type="number" min={1} max={500} value={limit} onChange={e => setLimit(e.target.value)} />
                <div className="mt-1 text-[9.5px] text-[color:var(--subtle)]">
                  每页约 {src?.pageSize || 50} 条；会按数量自动翻页，超出会截断。单次最多 500 条。
                </div>
              </div>
              <div className="space-y-1.5 pt-1">
                <label className="flex items-center gap-2 text-[11px] text-[color:var(--muted)]">
                  <Checkbox checked={incremental} onCheckedChange={v => setIncremental(v === true)} /> 增量（跳过最近 3 天抓过的）
                </label>
                <label className="flex items-center gap-2 text-[11px] text-[color:var(--muted)]">
                  <Checkbox checked={skipDeleted} onCheckedChange={v => setSkipDeleted(v === true)} /> 跳过此前手动删除的条目
                </label>
              </div>
            </>
          ) : (
            <>
              <div>
                <div className="ui-field-label mb-1.5">本地视频文件夹</div>
                <Input value={path} onChange={e => setPath(e.target.value)} placeholder="D:\project\testVideo" />
              </div>
              <div className="text-[10px] leading-5 text-[color:var(--subtle)]">
                只移动 mp4；同名同大小文件会跳过，不覆盖目标。导入后会进入台账，可继续补封面/时长。
              </div>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

export function LinkScrapeDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [url, setUrl] = React.useState('')
  const [busy, setBusy] = React.useState(false)
  const [step, setStep] = React.useState<'input' | 'files'>('input')
  const [files, setFiles] = React.useState<{ index: number; path: string; length: number }[]>([])
  const [selected, setSelected] = React.useState<number[]>([])

  const reset = () => {
    setUrl('')
    setBusy(false)
    setStep('input')
    setFiles([])
    setSelected([])
  }
  const close = () => { reset(); onOpenChange(false) }

  const start = async () => {
    const value = url.trim()
    if (!value) return
    if (value.toLowerCase().startsWith('magnet:')) {
      toast.error('暂不支持磁力链接')
      return
    }
    setBusy(true)
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/scrape', { url: value })
      if (!r.ok) throw new Error(r.error || '抓取失败')
      toast.success('已开始抓取链接')
      try { sessionStorage.setItem('pektino.scrapePending', '1'); sessionStorage.setItem('pektino.scrapePendingAt', String(Date.now())) } catch { }
      qc.invalidateQueries({ queryKey: ['state'] })
      close()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const confirm = async () => {
    if (!selected.length) return
    setBusy(true)
    try {
      const r = await post<{ ok: boolean; error?: string; tokens?: string[] }>('/api/torrent/add', {
        url: url.trim(),
        files: files.filter(f => selected.includes(f.index)),
      })
      if (!r.ok) throw new Error(r.error || '加入下载失败')
      toast.success('已开始磁力下载', { action: { label: '查看队列', onClick: () => navigate('/queue') } })
      qc.invalidateQueries({ queryKey: ['state'] })
      close()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const toggle = (index: number, checked: boolean) => {
    setSelected(prev => checked ? [...new Set([...prev, index])] : prev.filter(i => i !== index))
  }

  return (
    <Dialog open={open} onOpenChange={o => { if (!o) reset(); onOpenChange(o) }}>
      <DialogContent
        title={step === 'files' ? '选择磁力下载内容' : '通过链接抓取'}
        className="w-[min(520px,calc(100vw-40px))]"
        footer={
          <div className="ml-auto flex gap-2">
            <Button size="sm" variant="outline" onClick={close}>取消</Button>
            {step === 'files' ? (
              <Button size="sm" onClick={() => void confirm()} disabled={busy || !selected.length}>
                <Download className="size-3.5" /> {busy ? '加入中…' : `下载 ${selected.length} 个文件`}
              </Button>
            ) : (
              <Button size="sm" onClick={start} disabled={busy || !url.trim()}>
                {busy ? <RefreshCw className="size-3.5 animate-spin" /> : <Link2 className="size-3.5" />} 确认链接
              </Button>
            )}
          </div>
        }
      >
        <div className="space-y-3 p-4">
          {step === 'input' ? (
            <>
              <Input value={url} onChange={e => setUrl(e.target.value)} placeholder="视频页 / .m3u8 / .mp4" autoFocus />
              <div className="text-[10px] leading-5 text-[color:var(--subtle)]">
                目前仅支持网页链接和直链；磁力链接暂不支持。
              </div>
            </>
          ) : (
            <>
              <div className="text-[10px] leading-5 text-[color:var(--subtle)]">
                已读取磁力元数据，勾选要下载的文件。取消会放弃本次磁力任务。
              </div>
              <div className="max-h-[42vh] overflow-auto rounded-lg border border-[color:var(--border)]">
                {files.map(file => {
                  const checked = selected.includes(file.index)
                  return (
                    <label key={file.index} className="flex cursor-pointer items-center gap-2 border-b border-[color:var(--border)] px-2.5 py-2 last:border-0 hover:bg-[color:var(--surface-2)]">
                      <Checkbox checked={checked} onCheckedChange={v => toggle(file.index, v === true)} />
                      <FileVideo className="size-3.5 shrink-0 text-[color:var(--subtle)]" />
                      <span className="min-w-0 flex-1 truncate text-[10.5px]" title={file.path}>{file.path}</span>
                      <span className="shrink-0 text-[9px] text-[color:var(--subtle)]">{formatBytes(file.length)}</span>
                      {checked ? <Check className="size-3 shrink-0 text-[color:var(--success)]" /> : null}
                    </label>
                  )
                })}
              </div>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
