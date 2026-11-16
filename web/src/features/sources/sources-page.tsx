import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Download,
  FileJson,
  Plug,
  RefreshCw,
  Trash2,
  Upload,
} from 'lucide-react'
import { toast } from 'sonner'
import { post } from '@/lib/api'
import { cn } from '@/lib/utils'
import { confirmAction } from '@/store/confirm'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import { Skeleton } from '@/components/ui/skeleton'

type Rank = { key: string; label: string; group?: string }

type SourceItem = {
  id: string
  name: string
  version: string
  author?: string
  description?: string
  baseUrl: string
  hosts: string[]
  capabilities: string[]
  ranks: Rank[]
  enabled: boolean
  origin: string
  path: string
  installedAt?: string
  lastRunAt?: string
  lastRunOk?: boolean
  lastError?: string
}

type PreviewRow = {
  token: string
  title: string
  playable: string
  cover: string
  duration: string
  res: string
}

const CAP_LABEL: Record<string, string> = {
  scrape: '列表',
  detail: '详情',
  cover: '封面',
  download: '下载',
}

export function SourcesPage() {
  const [draft, setDraft] = React.useState('')
  const [overwrite, setOverwrite] = React.useState(false)
  const [error, setError] = React.useState('')
  const [busy, setBusy] = React.useState<string | null>(null)
  const [preview, setPreview] = React.useState<{ id: string; rows: PreviewRow[] } | null>(null)

  const query = useQuery<{ ok: boolean; items: SourceItem[] }>({
    queryKey: ['sources'],
    queryFn: async () => {
      const res = await fetch('/api/sources', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取源列表失败')
      return res.json()
    },
    refetchInterval: 15_000,
  })

  const items = query.data?.items || []
  const refresh = () => query.refetch()

  const importFromFile = async (file: File | undefined) => {
    if (!file) return
    const text = await file.text()
    setDraft(text)
    setError('')
  }

  const doImport = async () => {
    if (!draft.trim()) {
      setError('先粘贴源包 JSON，或选择一个 .json 文件')
      return
    }
    setBusy('import')
    setError('')
    try {
      const r = await post<{ ok: boolean; error?: string; source?: { id: string; name: string } }>(
        '/api/sources/import',
        { data: draft, overwrite },
      )
      if (!r.ok) {
        setError(r.error || '导入失败')
        toast.error('导入失败')
        return
      }
      toast.success(`已导入 ${r.source?.name || r.source?.id || '源'}`)
      setDraft('')
      setOverwrite(false)
      await refresh()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const toggle = async (item: SourceItem) => {
    setBusy('toggle:' + item.id)
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/sources/enable', {
        id: item.id,
        enabled: !item.enabled,
      })
      if (!r.ok) throw new Error(r.error || '操作失败')
      await refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const remove = async (item: SourceItem) => {
    const ok = await confirmAction({
      title: `删除源「${item.name}」？`,
      description: '只删除导入的源包文件，不影响已经入库的台账。',
      confirmText: '删除',
      danger: true,
    })
    if (!ok) return
    setBusy('remove:' + item.id)
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/sources/remove', { id: item.id })
      if (!r.ok) throw new Error(r.error || '删除失败')
      toast.success('已删除')
      if (preview?.id === item.id) setPreview(null)
      await refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const test = async (item: SourceItem) => {
    setBusy('test:' + item.id)
    setPreview(null)
    try {
      const r = await post<{
        ok: boolean
        error?: string
        count?: number
        rows?: PreviewRow[]
      }>('/api/sources/test', {
        id: item.id,
        rank: item.ranks[0]?.key || '',
        limit: 2,
        resolve: true,
      })
      if (!r.ok) {
        toast.error(r.error || '测试失败')
        return
      }
      setPreview({ id: item.id, rows: r.rows || [] })
      toast.success(`测试通过：抓到 ${r.count ?? r.rows?.length ?? 0} 条`)
      await refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const exportItem = (item: SourceItem) => {
    const a = document.createElement('a')
    a.href = `/api/sources/export?id=${encodeURIComponent(item.id)}`
    a.download = `${item.id}.json`
    a.click()
  }

  if (query.isLoading) {
    return (
      <div className="grid gap-3 p-4">
        <Skeleton className="h-[220px] rounded-[14px]" />
        <Skeleton className="h-[160px] rounded-[14px]" />
      </div>
    )
  }

  return (
    <div className="grid gap-3 p-4">
      <Card className="p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <SectionTitle
            icon={Plug}
            title="外部源"
            sub="声明式 JSON 源包：导入即用，不需要重新编译。安装目录 %APPDATA%\Reelkeep\sources"
          />
          <div className="flex items-center gap-2">
            <Badge tone="muted">{items.length} 个已安装</Badge>
            <Button size="sm" variant="outline" onClick={() => refresh()} disabled={query.isFetching}>
              <RefreshCw className={cn('size-3.5', query.isFetching && 'animate-spin')} />
              刷新
            </Button>
          </div>
        </div>

        <div className="mt-4 grid gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-[10px] text-[color:var(--muted)]">
            <input
              type="file"
              accept=".json,application/json"
              className="hidden"
              onChange={e => {
                void importFromFile(e.target.files?.[0])
                e.target.value = ''
              }}
            />
            <span className="inline-flex h-8 items-center gap-1.5 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 text-[11px] hover:bg-[color:var(--surface-3)]">
              <FileJson className="size-3.5" /> 选择源包文件
            </span>
            <span className="text-[color:var(--subtle)]">或直接把 JSON 粘贴到下面</span>
          </label>

          <textarea
            value={draft}
            onChange={e => {
              setDraft(e.target.value)
              setError('')
            }}
            spellCheck={false}
            placeholder={'{\n  "schema": 1,\n  "id": "example",\n  "name": "Example",\n  ...\n}'}
            className="h-[150px] w-full resize-y rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] p-2.5 font-mono text-[10px] leading-relaxed text-[color:var(--foreground)] outline-none placeholder:text-[color:var(--subtle)] focus:border-[color:var(--border-strong)]"
          />

          {error ? (
            <pre className="max-h-[180px] overflow-auto whitespace-pre-wrap rounded-[9px] border border-[color:color-mix(in_srgb,var(--danger)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--danger)_6%,transparent)] p-2.5 font-mono text-[9.5px] leading-relaxed text-[color:var(--danger)]">
              {error}
            </pre>
          ) : null}

          <div className="flex flex-wrap items-center gap-3">
            <label className="flex items-center gap-1.5 text-[10px] text-[color:var(--muted)]">
              <input
                type="checkbox"
                checked={overwrite}
                onChange={e => setOverwrite(e.target.checked)}
                className="size-3.5 accent-[color:var(--primary)]"
              />
              覆盖同 id 的已安装源
            </label>
            <div className="ml-auto flex items-center gap-2">
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setDraft('')
                  setError('')
                }}
                disabled={!draft && !error}
              >
                清空
              </Button>
              <Button size="sm" onClick={() => void doImport()} disabled={busy !== null}>
                <Upload className="size-3.5" />
                {busy === 'import' ? '校验中…' : '校验并导入'}
              </Button>
            </div>
          </div>
        </div>
      </Card>

      {items.length === 0 ? (
        <Card className="p-8 text-center">
          <div className="text-[12px] font-medium">还没有安装外部源</div>
          <div className="mt-1 text-[10px] text-[color:var(--subtle)]">
            可以从仓库的 sources\ 目录拿现成的源包，例如 pektino.json、hsex.json
          </div>
        </Card>
      ) : (
        items.map(item => (
          <Card key={item.id} className="p-4">
            <div className="flex flex-wrap items-start gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-[12.5px] font-semibold">{item.name}</span>
                  <Badge tone="muted">{item.id}</Badge>
                  <Badge tone="muted">v{item.version}</Badge>
                  {item.enabled ? <Badge tone="ok">启用</Badge> : <Badge tone="bad">已禁用</Badge>}
                  {item.lastRunAt ? (
                    item.lastRunOk ? (
                      <Badge tone="ok">最近正常</Badge>
                    ) : (
                      <Badge tone="bad">最近失败</Badge>
                    )
                  ) : (
                    <Badge tone="muted">未运行</Badge>
                  )}
                </div>
                {item.description ? (
                  <div className="mt-1.5 text-[10px] leading-relaxed text-[color:var(--muted)]">{item.description}</div>
                ) : null}
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  {item.capabilities.map(c => (
                    <Badge key={c} tone="muted">
                      {CAP_LABEL[c] || c}
                    </Badge>
                  ))}
                  {item.ranks.map(r => (
                    <Badge key={r.key} tone="muted">
                      {r.label}
                    </Badge>
                  ))}
                </div>
                <div className="mt-2 truncate font-mono text-[9.5px] text-[color:var(--subtle)]" title={item.baseUrl}>
                  {item.baseUrl}
                  {item.hosts.length ? ` · ${item.hosts.join(', ')}` : ''}
                </div>
                {item.lastError ? (
                  <div className="mt-1.5 line-clamp-2 text-[9.5px] text-[color:var(--danger)]">{item.lastError}</div>
                ) : null}
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  checked={item.enabled}
                  onCheckedChange={() => void toggle(item)}
                  disabled={busy !== null}
                  aria-label={item.enabled ? '禁用源' : '启用源'}
                />
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void test(item)}
                  disabled={busy !== null || !item.enabled}
                >
                  {busy === 'test:' + item.id ? '测试中…' : '测试'}
                </Button>
                <Button size="icon-sm" variant="ghost" title="导出" onClick={() => exportItem(item)}>
                  <Download className="size-3.5" />
                </Button>
                <Button
                  size="icon-sm"
                  variant="ghost"
                  title="删除"
                  onClick={() => void remove(item)}
                  disabled={busy !== null}
                >
                  <Trash2 className="size-3.5 text-[color:var(--danger)]" />
                </Button>
              </div>
            </div>

            {preview?.id === item.id ? (
              <div className="mt-3 overflow-hidden rounded-[9px] border border-[color:var(--border)]">
                <div className="flex items-center gap-2 border-b border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 py-1.5 text-[9.5px] text-[color:var(--subtle)]">
                  预览（不写库）· {preview.rows.length} 条
                </div>
                {preview.rows.map(row => (
                  <div
                    key={row.token}
                    className="grid grid-cols-[minmax(0,1.2fr)_minmax(0,2fr)_70px] items-center gap-2 border-b border-[color:var(--border)] px-2.5 py-1.5 last:border-b-0"
                  >
                    <span className="truncate font-mono text-[9.5px]" title={row.token}>
                      {row.token}
                    </span>
                    <span className="truncate text-[10px]" title={row.title}>
                      {row.title || '—'}
                    </span>
                    <span className="text-right font-mono text-[9.5px] text-[color:var(--subtle)]">{row.duration || '—'}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </Card>
        ))
      )}
    </div>
  )
}

function SectionTitle({ icon: Icon, title, sub }: { icon: React.ComponentType<{ className?: string }>; title: string; sub: string }) {
  return (
    <div className="flex items-start gap-2">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border border-[color:var(--border)] text-[color:var(--primary-2)]">
        <Icon className="size-3.5" />
      </span>
      <div>
        <div className="text-[11.5px] font-semibold">{title}</div>
        <div className="text-[9.5px] text-[color:var(--subtle)]">{sub}</div>
      </div>
    </div>
  )
}
