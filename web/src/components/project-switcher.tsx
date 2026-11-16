import { useEffect, useState } from 'react'
import { FolderOpen, HardDrive, Loader2 } from 'lucide-react'
import { toast } from 'sonner'

import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { post } from '@/lib/api'

type Entry = { path: string; name: string; current: boolean }
type ProjectsData = { current?: Entry; recent?: Entry[]; desktop?: boolean }
type OpenResult = { ok: boolean; action?: string; url?: string; error?: string }

/**
 * 项目切换器：最近项目、打开/新建文件夹。
 * 每个项目是一个独立数据目录；切换时桌面开新窗口，浏览器开新实例。
 */
export function ProjectSwitcher({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [data, setData] = useState<ProjectsData | null>(null)
  const [path, setPath] = useState('')
  const [busy, setBusy] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open) return
    let alive = true
    setLoading(true)
    fetch('/api/projects', { cache: 'no-store' })
      .then(r => r.json())
      .then((d: ProjectsData) => {
        if (!alive) return
        setData(d)
        setPath(p => p || d.current?.path || '')
      })
      .catch(() => {})
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [open])

  const openProject = async (target: string) => {
    const value = target.trim()
    if (!value) {
      toast.error('请填写项目文件夹路径')
      return
    }
    setBusy(value)
    try {
      const result = await post<OpenResult>('/api/projects/open', { path: value })
      if (result.action === 'new-instance' && result.url) {
        toast.success('正在打开新项目…')
        window.location.assign(result.url)
        return
      }
      toast.success('已在新窗口打开项目')
      onOpenChange(false)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy('')
    }
  }

  const revealCurrent = async () => {
    try {
      await post('/api/project/open', {})
      toast.success('已打开当前项目文件夹')
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const recent = data?.recent || []

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="项目"
        description="每个项目是一个独立文件夹；切换项目会打开一个新窗口/实例"
        className="w-[min(680px,calc(100vw-48px))]"
      >
        <div className="space-y-3 p-3">
          <div className="flex gap-2">
            <Input
              value={path}
              onChange={e => setPath(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter') void openProject(path)
              }}
              placeholder="项目文件夹路径，例如 D:\Reelkeep\我的项目"
              className="h-9 font-mono text-[11px]"
            />
            <Button
              onClick={() => void openProject(path)}
              disabled={!!busy}
              className="h-9 shrink-0"
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : <FolderOpen className="size-4" />}
              打开 / 新建
            </Button>
          </div>

          <div className="flex items-center justify-between pt-1">
            <div className="text-[9px] tracking-[.9px] text-[color:var(--subtle)]">最近项目</div>
            <Button variant="ghost" size="sm" className="h-7 text-[10px]" onClick={() => void revealCurrent()}>
              <HardDrive className="size-3.5" />
              在资源管理器中打开当前项目
            </Button>
          </div>

          <div className="max-h-[320px] space-y-1 overflow-auto">
            {loading && recent.length === 0 ? (
              <div className="px-3 py-6 text-center text-[10px] text-[color:var(--subtle)]">读取项目列表…</div>
            ) : null}
            {!loading && recent.length === 0 ? (
              <div className="rounded-lg border border-dashed border-[color:var(--border)] px-4 py-8 text-center text-[10px] leading-6 text-[color:var(--subtle)]">
                还没有最近项目。填一个文件夹路径，打开或新建一个项目。
              </div>
            ) : null}
            {recent.map(item => (
              <button
                key={item.path}
                type="button"
                disabled={!!busy}
                onClick={() => void openProject(item.path)}
                className={cn(
                  'flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-left transition-colors disabled:opacity-60',
                  item.current
                    ? 'border-[color:var(--border-strong)] bg-[color:var(--surface-2)]'
                    : 'border-transparent hover:bg-[color:var(--surface-2)]',
                )}
              >
                <FolderOpen className="size-4 shrink-0 text-[color:var(--accent)]" />
                <div className="min-w-0 flex-1">
                  <b className="block truncate text-[12px] font-medium">{item.name}</b>
                  <small className="block truncate font-mono text-[9.5px] text-[color:var(--subtle)]">{item.path}</small>
                </div>
                {item.current ? (
                  <span className="shrink-0 rounded-full border border-[color:var(--border)] px-2 py-0.5 text-[9px] text-[color:var(--subtle)]">
                    当前
                  </span>
                ) : null}
                {busy === item.path ? <Loader2 className="size-3.5 shrink-0 animate-spin" /> : null}
              </button>
            ))}
          </div>

          <p className="text-[9.5px] leading-5 text-[color:var(--subtle)]">
            新建时填一个空文件夹；非空且不是 Reelkeep 项目的目录会被拒绝。项目数据包含 project.json、data/ 与 media/。
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
