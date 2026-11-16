import { useEffect, useState } from 'react'
import { ChevronDown, Copy, FolderOpen, Minus, Square, X } from 'lucide-react'

import { ProjectSwitcher } from '@/components/project-switcher'
import { useAppState } from '@/features/use-app-state'
import { projectNameFromRoot } from '@/lib/utils'

/**
 * 桌面壳的自绘标题栏：拖动、项目切换、最小化/最大化/关闭。
 * 浏览器里直接不渲染，不影响 Web 形态。
 */
export function TitleBar() {
  const [desktop, setDesktop] = useState(false)
  const [maximized, setMaximized] = useState(false)
  const [open, setOpen] = useState(false)
  const { data } = useAppState()

  useEffect(() => {
    let alive = true
    fetch('/api/desktop', { cache: 'no-store' })
      .then(r => r.json())
      .then((d: { desktop?: boolean; maximized?: boolean }) => {
        if (!alive) return
        setDesktop(!!d.desktop)
        setMaximized(!!d.maximized)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  if (!desktop) return null

  const project = projectNameFromRoot((data?.project as { root?: string } | undefined)?.root)

  const windowAction = async (action: 'minimize' | 'maximize' | 'close') => {
    try {
      const res = await fetch('/api/desktop/window', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      })
      const body = await res.json()
      if (typeof body.maximized === 'boolean') setMaximized(body.maximized)
    } catch {
      // 窗口关闭时请求中断是正常的
    }
  }

  return (
    <>
      <div
        className="app-drag flex h-[38px] shrink-0 select-none items-center gap-2 border-b border-[color:var(--border)] bg-[color:var(--surface)] pl-3"
        onDoubleClick={() => windowAction('maximize')}
      >
        <img src="/api/logo" alt="" className="size-5 shrink-0 rounded-[6px] object-cover" />
        <span className="shrink-0 text-[11.5px] font-semibold tracking-[.3px]">Reelkeep</span>

        <button
          type="button"
          onClick={() => setOpen(true)}
          onDoubleClick={e => e.stopPropagation()}
          title="切换项目"
          className="app-no-drag ml-1 flex max-w-[320px] items-center gap-1.5 rounded-md border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2 py-1 text-[10px] text-[color:var(--muted)] transition-colors hover:border-[color:var(--border-strong)] hover:text-[color:var(--foreground)]"
        >
          <FolderOpen className="size-3 shrink-0" />
          <span className="truncate">{project}</span>
          <ChevronDown className="size-3 shrink-0" />
        </button>

        <div className="min-w-0 flex-1" />

        <div className="app-no-drag flex h-full items-stretch" onDoubleClick={e => e.stopPropagation()}>
          <button
            type="button"
            title="最小化"
            aria-label="最小化"
            onClick={() => windowAction('minimize')}
            className="grid w-11 place-items-center text-[color:var(--muted)] transition-colors hover:bg-[color:var(--surface-3)] hover:text-[color:var(--foreground)]"
          >
            <Minus className="size-4" />
          </button>
          <button
            type="button"
            title={maximized ? '还原' : '最大化'}
            aria-label={maximized ? '还原' : '最大化'}
            onClick={() => windowAction('maximize')}
            className="grid w-11 place-items-center text-[color:var(--muted)] transition-colors hover:bg-[color:var(--surface-3)] hover:text-[color:var(--foreground)]"
          >
            {maximized ? <Copy className="size-3.5" /> : <Square className="size-3.5" />}
          </button>
          <button
            type="button"
            title="关闭"
            aria-label="关闭"
            onClick={() => windowAction('close')}
            className="grid w-11 place-items-center text-[color:var(--muted)] transition-colors hover:bg-[#d33b3b] hover:text-white"
          >
            <X className="size-4" />
          </button>
        </div>
      </div>

      <ProjectSwitcher open={open} onOpenChange={setOpen} />
    </>
  )
}
