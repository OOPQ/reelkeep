import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import { FileVideo, RefreshCw } from 'lucide-react'
import { formatBytes } from '@/lib/utils'
import { usePreview } from '@/features/preview/preview-provider'
import type { Row } from '@/types'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

function fileRow(token: string, name: string): Row {
  return {
    token, status: 'done', fav: '0', rank: '', page: '', duration: '', seconds: '', res: '', views: '', likes: '',
    uploaded: '', url: '', page_url: '', site: '', source: '', source_id: '', title: name, cover_url: '',
    download_kind: '', download_headers: '', extra: '', first_seen: '', last_seen: '', sent_at: '', note: '',
    file_name: '', file_path: '', file_size: '', file_date: '', play_count: '0', last_play: '', rating: '0',
  }
}

type JobFile = {
  index: number
  path: string
  length: number
  selected: boolean
  state: string
  bytes: number
  error?: string
  mediaToken?: string
}

type Job = {
  id: string
  title: string
  state: string
  selected: number
  completed: number
  totalBytes: number
  peers?: number
  seeders?: number
  files: JobFile[]
}

export function TorrentJobDialog({ jobId, onClose }: { jobId: string | null; onClose: () => void }) {
  const preview = usePreview()
  const { data, isLoading } = useQuery<{ ok: boolean; jobs: Job[] }>({
    queryKey: ['torrent-jobs'],
    queryFn: async () => {
      const res = await fetch('/api/torrent/jobs', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取磁力任务失败')
      return res.json()
    },
    refetchInterval: jobId ? 3000 : false,
    enabled: !!jobId,
  })
  const job = data?.jobs.find(j => j.id === jobId)

  return (
    <Dialog open={!!jobId} onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent
        title={job ? `磁力任务 · ${job.title}` : '磁力任务'}
        description="一个 magnet 是一个任务，勾选的文件在任务内管理；完成的视频才会进入媒体库。"
        className="w-[min(680px,calc(100vw-40px))]"
        footer={<div className="ml-auto"><Button size="sm" variant="outline" onClick={onClose}>关闭</Button></div>}
      >
        <div className="max-h-[55vh] space-y-2 overflow-auto p-3">
          {isLoading ? (
            <div className="flex items-center gap-2 py-8 text-[11px] text-[color:var(--subtle)]">
              <RefreshCw className="size-3.5 animate-spin" /> 读取中…
            </div>
          ) : !job ? (
            <div className="py-8 text-center text-[11px] text-[color:var(--subtle)]">任务记录不存在</div>
          ) : (
            <>
              <div className="mb-2 flex flex-wrap items-center gap-3 text-[10px] text-[color:var(--muted)]">
                <span>状态：{job.state}</span>
                <span>已完成 {job.completed}/{job.selected}</span>
                <span>总大小 {formatBytes(job.totalBytes)}</span>
                <span>peers {job.peers || 0} / seeders {job.seeders || 0}</span>
              </div>
              {job.files.map(f => (
                <div key={f.index} className="flex items-center gap-2 rounded-lg border border-[color:var(--border)] px-2.5 py-2 text-[10.5px]">
                  <FileVideo className="size-3.5 shrink-0 text-[color:var(--subtle)]" />
                  <span className="min-w-0 flex-1 truncate" title={f.path}>{f.path}</span>
                  <span className="shrink-0 text-[9px] text-[color:var(--subtle)]">
                    {formatBytes(f.bytes)} / {formatBytes(f.length)}
                  </span>
                  <span className="shrink-0 text-[9px] text-[color:var(--subtle)]">{f.selected ? f.state : '未选择'}</span>
                  {f.mediaToken ? (
                    <Button size="sm" variant="outline" className="h-6 px-2 text-[9px]" onClick={() => { preview.open([fileRow(f.mediaToken!, f.path)], f.mediaToken!, { source: 'torrent' }); onClose() }}>
                      打开
                    </Button>
                  ) : null}
                </div>
              ))}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
