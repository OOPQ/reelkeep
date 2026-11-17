import * as React from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Download,
  FileJson,
  KeyRound,
  LogIn,
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
import { Dialog, DialogContent } from '@/components/ui/dialog'

type Rank = { key: string; label: string; group?: string }

type RankTestResult = {
  rank: string
  label: string
  ok: boolean
  count: number
  listURL?: string
  listOK?: boolean
  detailOK?: boolean
  coverOK?: boolean
  resolveOK?: boolean
  mode?: string
  error?: string
}

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
  authType?: string
  authConfigured?: boolean
  authUpdatedAt?: string
  loginUrl?: string
  checkUrl?: string
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
  const [rankReport, setRankReport] = React.useState<{ id: string; name: string; results: RankTestResult[] } | null>(null)
  const [preview, setPreview] = React.useState<{ id: string; rows: PreviewRow[] } | null>(null)
  const [authFor, setAuthFor] = React.useState<string | null>(null)
  const [cookieDraft, setCookieDraft] = React.useState('')
  const [authMsg, setAuthMsg] = React.useState('')
  const [authBusy, setAuthBusy] = React.useState(false)
  const [jsonFor, setJsonFor] = React.useState<string | null>(null)
  const [jsonText, setJsonText] = React.useState('')
  const [jsonBusy, setJsonBusy] = React.useState(false)
  const [desktop, setDesktop] = React.useState(false)

  React.useEffect(() => {
    fetch('/api/desktop', { cache: 'no-store' })
      .then(r => r.json())
      .then((d: { desktop?: boolean }) => setDesktop(!!d.desktop))
      .catch(() => {})
  }, [])

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
    setError('')
    if (file.name.toLowerCase().endsWith('.zip') || file.type === 'application/zip') {
      setBusy('import-zip')
      try {
        const res = await fetch(`/api/sources/import-zip?overwrite=${overwrite ? '1' : '0'}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/zip' },
          body: await file.arrayBuffer(),
        })
        const r = await res.json() as { ok: boolean; error?: string; imported?: Array<{ name?: string; id?: string }>; errors?: string[] }
        if (!r.ok) throw new Error(r.error || 'ZIP 导入失败')
        toast.success(`已导入 ${r.imported?.length || 0} 个源`)
        if (r.errors?.length) toast.warning(`${r.errors.length} 个源导入失败`, { duration: 8000 })
        setOverwrite(false)
        await refresh()
      } catch (e) {
        setError((e as Error).message)
        toast.error((e as Error).message)
      } finally {
        setBusy(null)
      }
      return
    }
    const text = await file.text()
    setDraft(text)
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

  const testAll = async (item: SourceItem) => {
    setBusy('testall:' + item.id)
    try {
      const r = await post<{
        ok: boolean
        error?: string
        passed?: number
        total?: number
        results?: RankTestResult[]
      }>('/api/sources/test-all', { id: item.id, limit: 1 })
      if (!r.ok) {
        toast.error(r.error || '测试失败')
        return
      }
      setRankReport({ id: item.id, name: item.name, results: r.results || [] })
      toast.success(`榜单测试完成：${r.passed ?? 0}/${r.total ?? r.results?.length ?? 0} 通过`)
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

  const showJson = async (item: SourceItem) => {
    setJsonBusy(true)
    try {
      const res = await fetch(`/api/sources/package?id=${encodeURIComponent(item.id)}`, { cache: 'no-store' })
      const data = await res.json()
      if (!res.ok || !data.ok) throw new Error(data.error || '读取源包失败')
      setJsonText(JSON.stringify(data.package, null, 2))
      setJsonFor(item.id)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setJsonBusy(false)
    }
  }

  const openAuth = (item: SourceItem) => {
    setAuthFor(authFor === item.id ? null : item.id)
    setCookieDraft('')
    setAuthMsg('')
  }

  const desktopLogin = async (item: SourceItem) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/desktop/login', { id: item.id })
      if (!r.ok) throw new Error(r.error || '打开登录窗口失败')
      toast.success('已打开登录窗口：首次登录后关闭窗口会自动保存 Cookie')
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const captureCookie = async (item: SourceItem) => {
    try {
      const r = await post<{ ok: boolean; error?: string; length?: number }>('/api/desktop/login/capture', { id: item.id })
      if (!r.ok) throw new Error(r.error || '读取 Cookie 失败')
      toast.success(`已读取并保存 Cookie（${r.length || 0} 字符）`)
      setAuthMsg('登录凭据已保存')
      void refresh()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  const saveAuth = async (item: SourceItem) => {
    if (!cookieDraft.trim()) {
      setAuthMsg('先粘贴 Cookie')
      return
    }
    setAuthBusy(true)
    setAuthMsg('')
    try {
      const r = await post<{
        ok: boolean
        error?: string
        loggedIn?: boolean
        message?: string
        testError?: string
      }>('/api/sources/auth', { id: item.id, cookie: cookieDraft })
      if (!r.ok) {
        setAuthMsg(r.error || '保存失败')
        return
      }
      setAuthMsg(r.message || (r.loggedIn ? '登录有效' : '已保存，但未确认登录状态'))
      setCookieDraft('')
      toast.success('登录凭据已加密保存')
      await refresh()
    } catch (e) {
      setAuthMsg((e as Error).message)
    } finally {
      setAuthBusy(false)
    }
  }

  const testAuth = async (item: SourceItem) => {
    setAuthBusy(true)
    setAuthMsg('')
    try {
      const r = await post<{ ok: boolean; error?: string; loggedIn?: boolean; message?: string }>(
        '/api/sources/auth/test',
        { id: item.id },
      )
      if (!r.ok) {
        setAuthMsg(r.error || '测试失败')
        return
      }
      setAuthMsg(r.message || (r.loggedIn ? '登录有效' : '登录可能已失效'))
    } catch (e) {
      setAuthMsg((e as Error).message)
    } finally {
      setAuthBusy(false)
    }
  }

  const clearAuth = async (item: SourceItem) => {
    const ok = await confirmAction({
      title: '清除登录凭据？',
      description: '只删除本机保存的 Cookie，不会注销浏览器里的登录。',
      confirmText: '清除',
      danger: true,
    })
    if (!ok) return
    setAuthBusy(true)
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/sources/auth/clear', { id: item.id })
      if (!r.ok) throw new Error(r.error || '清除失败')
      setAuthMsg('已清除')
      toast.success('已清除登录凭据')
      await refresh()
    } catch (e) {
      setAuthMsg((e as Error).message)
    } finally {
      setAuthBusy(false)
    }
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
              accept=".json,.zip,application/json,application/zip"
              className="hidden"
              onChange={e => {
                void importFromFile(e.target.files?.[0])
                e.target.value = ''
              }}
            />
            <span className="inline-flex h-8 items-center gap-1.5 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 text-[11px] hover:bg-[color:var(--surface-3)]">
              <FileJson className="size-3.5" /> 选择源包文件（JSON/ZIP）
            </span>
            <span className="text-[color:var(--subtle)]">或直接把 JSON 粘贴到下面；ZIP 会导入其中所有 .json</span>
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
                  {item.authType ? (
                    item.authConfigured ? <Badge tone="ok">已登录</Badge> : <Badge tone="muted">未登录</Badge>
                  ) : null}
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
                {item.ranks.length > 1 ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void testAll(item)}
                    disabled={busy !== null || !item.enabled}
                  >
                    {busy === 'testall:' + item.id ? '测试中…' : '全部榜单'}
                  </Button>
                ) : null}
                {item.authType ? (
                  <Button size="sm" variant="outline" onClick={() => openAuth(item)} disabled={busy !== null}>
                    <KeyRound className="size-3.5" />
                    {item.authConfigured ? '重新登录' : '登录'}
                  </Button>
                ) : null}
                <Button size="icon-sm" variant="ghost" title="查看完整 JSON" onClick={() => void showJson(item)} disabled={jsonBusy}>
                  <FileJson className="size-3.5" />
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

            {authFor === item.id ? (
              <div className="mt-3 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] p-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1.5 text-[10.5px] font-medium">
                    <LogIn className="size-3.5 text-[color:var(--primary-2)]" />
                    登录 {item.name}
                  </div>
                  {item.loginUrl ? (
                    <div className="flex items-center gap-2">
                      <a
                        href={item.loginUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="text-[10px] text-[color:var(--primary-2)] underline underline-offset-2"
                      >
                        打开登录页 ↗
                      </a>
                      {desktop ? (
                        <Button size="sm" variant="outline" onClick={() => void desktopLogin(item)}>
                          应用内登录
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                </div>
                <div className="mt-1 text-[9.5px] leading-relaxed text-[color:var(--subtle)]">
                  桌面端点「应用内登录」：首次登录后关闭登录窗口会自动读取并保存 Cookie（含 HttpOnly）；
                  已保存过凭据时，重新登录后请点「读取 Cookie」覆盖。若读取失败，仍可手动粘贴。
                  凭据只保存在本机（Windows 用 DPAPI 加密），<b>不会</b>写进源包，也不会出现在导出文件里。
                </div>
                <textarea
                  value={cookieDraft}
                  onChange={e => {
                    setCookieDraft(e.target.value)
                    setAuthMsg('')
                  }}
                  spellCheck={false}
                  autoComplete="off"
                  placeholder="PHPSESSID=...; token=..."
                  className="mt-2 h-[72px] w-full resize-y rounded-[9px] border border-[color:var(--border)] bg-[color:var(--background-2)] p-2.5 font-mono text-[10px] leading-relaxed outline-none placeholder:text-[color:var(--subtle)] focus:border-[color:var(--border-strong)]"
                />
                {authMsg ? (
                  <div className="mt-1.5 text-[9.5px] text-[color:var(--muted)]">{authMsg}</div>
                ) : null}
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <Button size="sm" onClick={() => void saveAuth(item)} disabled={authBusy}>
                    {authBusy ? '处理中…' : '保存并测试'}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void testAuth(item)}
                    disabled={authBusy || !item.authConfigured}
                  >
                    测试登录
                  </Button>
                  {desktop ? (
                    <Button size="sm" variant="outline" onClick={() => void captureCookie(item)}>
                      读取 Cookie
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant="danger"
                    onClick={() => void clearAuth(item)}
                    disabled={authBusy || !item.authConfigured}
                  >
                    清除
                  </Button>
                  <Button size="sm" variant="ghost" className="ml-auto" onClick={() => setAuthFor(null)}>
                    关闭
                  </Button>
                </div>
                {item.authConfigured && item.authUpdatedAt ? (
                  <div className="mt-1.5 text-[9px] text-[color:var(--subtle)]">已保存：{item.authUpdatedAt}</div>
                ) : null}
              </div>
            ) : null}

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

      <Dialog open={!!rankReport} onOpenChange={open => { if (!open) setRankReport(null) }}>
        <DialogContent
          title={`全部榜单测试 · ${rankReport?.name || ''}`}
          description="每个榜单抓 1 条验证列表页和详情页；不写台账。"
          className="w-[min(680px,calc(100vw-40px))]"
        >
          <div className="max-h-[60vh] overflow-auto p-3">
            <div className="mb-2 text-[10px] text-[color:var(--muted)]">
              通过 {rankReport?.results.filter(r => r.ok).length || 0} / {rankReport?.results.length || 0}
            </div>
            {rankReport?.results.map(r => (
              <div key={r.rank} className="border-b border-[color:var(--border)] py-2 text-[10px] last:border-b-0">
                <div className="flex items-center gap-2">
                  <span className="w-48 shrink-0 truncate font-medium" title={`${r.rank} · ${r.label}`}>{r.label || r.rank}</span>
                  <Badge tone={r.ok ? 'ok' : 'bad'}>{r.ok ? '通过' : '失败'}</Badge>
                  <span className="text-[9px] text-[color:var(--subtle)]">列表 {r.count} 条</span>
                  <span className={cn('text-[9px]', r.detailOK ? 'text-[color:var(--success)]' : 'text-[color:var(--danger)]')}>详情 {r.detailOK ? '✓' : '✗'}</span>
                  <span className={cn('text-[9px]', r.coverOK ? 'text-[color:var(--success)]' : 'text-[color:var(--warning)]')}>封面 {r.coverOK ? '✓' : '✗'}</span>
                  <span className={cn('text-[9px]', r.resolveOK ? 'text-[color:var(--success)]' : 'text-[color:var(--danger)]')}>解析 {r.resolveOK ? '✓' : '✗'}</span>
                  {r.mode ? <span className="text-[9px] text-[color:var(--subtle)]">{r.mode}</span> : null}
                </div>
                {r.error ? <div className="mt-1 line-clamp-2 text-[9px] text-[color:var(--danger)]" title={r.error}>{r.error}</div> : null}
                {r.listURL ? <div className="mt-0.5 truncate font-mono text-[8.5px] text-[color:var(--subtle)]" title={r.listURL}>{r.listURL}</div> : null}
              </div>
            ))}
          </div>
        </DialogContent>
      </Dialog>

      <Dialog open={!!jsonFor} onOpenChange={open => { if (!open) setJsonFor(null) }}>
        <DialogContent
          title={`源包 JSON · ${jsonFor ?? ''}`}
          description="后端加载并校验的就是这份完整数据；列表里的字段只是摘要"
          className="w-[min(900px,calc(100vw-48px))]"
        >
          <pre className="max-h-[65vh] overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[10px] leading-5 text-[color:var(--muted)]">
            {jsonText}
          </pre>
        </DialogContent>
      </Dialog>
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
