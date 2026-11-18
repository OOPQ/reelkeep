import * as React from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Database,
  Download,
  ExternalLink,
  FolderOpen,
  Gauge,
  Image,
  LayoutGrid,
  RefreshCw,
  RotateCcw,
  Save,
  ScanLine,
  Trash2,
  Wrench,
} from 'lucide-react'
import { toast } from 'sonner'
import { useAppState } from '@/features/use-app-state'
import { useUiStore } from '@/store/ui'
import { confirmAction } from '@/store/confirm'
import { post } from '@/lib/api'
import { cn, formatBytes, formatCount } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { ApiError } from '@/components/api-error'

export function SettingsPage() {
  const qc = useQueryClient()
  const { data, isLoading, refetch, isFetching, error } = useAppState(5_000)
  const density = useUiStore(s => s.density)
  const setDensity = useUiStore(s => s.setDensity)
  const longPressRate = useUiStore(s => s.longPressRate)
  const setLongPressRate = useUiStore(s => s.setLongPressRate)
  const [proxy, setProxy] = React.useState('')
  const [sourceIndex, setSourceIndex] = React.useState('')
  const [ffmpeg, setFfmpeg] = React.useState('')
  const [concurrent, setConcurrent] = React.useState('3')
  const [retryCount, setRetryCount] = React.useState('4')
  const [retryDelay, setRetryDelay] = React.useState('1')
  const [httpLimit, setHttpLimit] = React.useState('0')
  const [busy, setBusy] = React.useState<string | null>(null)
  const [checking, setChecking] = React.useState(false)
  const [updateInfo, setUpdateInfo] = React.useState<{ current: string; latest: string; hasUpdate: boolean; url: string } | null>(null)
  const [updateErr, setUpdateErr] = React.useState('')
  const [desktop, setDesktop] = React.useState(false)
  const [platform, setPlatform] = React.useState('')
  const [applying, setApplying] = React.useState(false)
  const [invalidOpen, setInvalidOpen] = React.useState(false)
  type InvalidFile = { token: string; title: string; path: string; reason: string }
  const invalidQuery = useQuery<{ ok: boolean; count: number; items: InvalidFile[] }>({
    queryKey: ['invalid-files'],
    queryFn: async () => {
      const res = await fetch('/api/invalid-files', { cache: 'no-store' })
      if (!res.ok) throw new Error('读取无效文件失败')
      return res.json()
    },
    staleTime: 30_000,
  })
  const invalidItems = invalidQuery.data?.items || []
  const revealInvalid = async (token: string) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/reveal', { token })
      if (!r.ok) throw new Error(r.error || '打开失败')
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  React.useEffect(() => {
    fetch('/api/desktop', { cache: 'no-store' })
      .then(r => r.json())
      .then((d: { desktop?: boolean; platform?: string }) => {
        setDesktop(!!d.desktop)
        setPlatform(d.platform || '')
      })
      .catch(() => {})
  }, [])

  React.useEffect(() => {
    if (!data?.settings) return
    setProxy(data.settings.proxy || '')
    setSourceIndex(data.settings.sourceIndexUrl || '')
    setFfmpeg(data.settings.ffmpegPath || '')
    setConcurrent(String(data.settings.concurrent || 3))
    setRetryCount(String(data.settings.retryCount || 4))
    setRetryDelay(String(data.settings.retryDelay || 1))
    setHttpLimit(String(data.settings.httpLimitKB || 0))
  }, [data?.settings])

  const after = () => qc.invalidateQueries({ queryKey: ['state'] })

  const checkUpdate = async () => {
    setChecking(true)
    setUpdateErr('')
    try {
      const res = await fetch('/api/update', { cache: 'no-store' })
      const data = await res.json()
      if (!data.ok) throw new Error(data.error || '检查更新失败')
      setUpdateInfo(data.update)
    } catch (err) {
      setUpdateInfo(null)
      setUpdateErr((err as Error).message)
    } finally {
      setChecking(false)
    }
  }

  const applyUpdate = async () => {
    setApplying(true)
    try {
      const r = await post<{ ok: boolean; error?: string; version?: string }>('/api/update/apply', {})
      if (!r.ok) throw new Error(r.error || '自动安装失败')
      toast.success(`正在安装 v${r.version}，应用即将退出…`, { duration: 10_000 })
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setApplying(false)
    }
  }

  const installFFmpeg = async () => {
    setBusy('ffmpeg')
    try {
      const r = await post<{ ok: boolean; error?: string; path?: string }>('/api/ffmpeg/install', {})
      if (!r.ok) throw new Error(r.error || 'FFmpeg 下载失败')
      if (r.path) setFfmpeg(r.path)
      toast.success('FFmpeg 已安装，HLS 下载现在可用')
      after()
    } catch (e) {
      toast.error((e as Error).message, { duration: 8_000 })
    } finally {
      setBusy(null)
    }
  }

  const save = async () => {    setBusy('save')
    try {
      const payload: Record<string, unknown> = {
        proxy: proxy.trim(),
        ffmpegPath: ffmpeg.trim(),
        concurrent: Number(concurrent) || 3,
        retryCount: Number(retryCount) || 4,
        retryDelay: Number(retryDelay) || 1,
        httpLimitKB: Number(httpLimit) || 0,
        sourceIndexUrl: sourceIndex.trim(),
      }
      const r = await post<{ ok: boolean; error?: string }>('/api/config', payload)
      if (!r.ok) throw new Error(r.error || '保存失败')
      toast.success('下载设置已保存')
      after()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const action = async (key: string, fn: () => Promise<{ ok?: boolean; error?: string } | unknown>, message: string) => {
    setBusy(key)
    try {
      const r = await fn()
      if (r && typeof r === 'object' && 'ok' in r && !(r as { ok?: boolean }).ok) {
        throw new Error((r as { error?: string }).error || '操作失败')
      }
      toast.success(message)
      after()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  const openProject = () => action('open', () => post('/api/project/open', {}), '已打开项目文件夹')
  const importFiles = () => action('import', () => post('/api/project/import', {}), '识别完成')
  const scan = () => action('scan', () => post('/api/scan', {}), '扫描完成')
  const repairCovers = async () => {
    if (!(await confirmAction({ title: '修复缺失封面', description: '会尝试抓取详情页并补封面，最多处理 40 条；需要联网。', confirmText: '开始修复' }))) return
    action('covers', () => post('/api/covers/repair', { limit: 40 }), '封面修复完成')
  }
  const repairParts = async () => {
    if (!(await confirmAction({ title: '整理 .part', description: '把已完成但仍残留 .part 后缀的文件改名。', confirmText: '整理' }))) return
    action('parts', () => post('/api/download/repair', {}), '已整理 .part')
  }
  const resetDownload = async () => {
    if (!(await confirmAction({ title: '解除下载占用', description: '会结束残留的下载进程并解除「已有任务在跑」状态。', confirmText: '解除', danger: true }))) return
    action('reset', () => post('/api/download/reset', {}), '已解除下载占用')
  }
  const clearCache = async () => {
    if (!(await confirmAction({ title: '清理封面缓存', description: '只删除本地封面/缩略图缓存，不删除视频；下次打开会重新生成或回源。', confirmText: '清理', danger: true }))) return
    action('cache', () => post('/api/cache/clear', {}), '封面缓存已清理')
  }

  if (isLoading) return <SettingsSkeleton />
  if (error) {
    return (
      <div className="p-4">
        <ApiError title="设置加载失败" error={error} onRetry={() => void refetch()} busy={isFetching} />
      </div>
    )
  }

  const project = data?.project as { root?: string; data?: string; media?: string; cache?: string; database?: string; dbMB?: number; videoCount?: number; videoGB?: number; coverCount?: number } | undefined
  const videoGB = Number(project?.videoGB || data?.stats?.diskGB || 0)
  const coverCount = project?.coverCount || Math.max(0, Number(data?.stats?.total || 0) - Number(data?.stats?.coverMissing || 0))
  const metrics = data?.metrics as { total?: number; avgMs?: number; maxMs?: number; maxPath?: string; uptime?: number; slow?: Array<{ path?: string; ms?: number }>; clientErrors?: Array<{ t?: string; msg?: string }> } | undefined
  const logs = (data?.log as Array<{ time?: string; kind?: string; message?: string }> | undefined) || []

  return (
    <div className="grid gap-3 p-4 min-[1100px]:grid-cols-2">
      <Card className="p-4">
        <SectionTitle icon={FolderOpen} title="当前项目" sub="数据与媒体都在这个文件夹里" />
        <div className="mt-3 space-y-2">
          <PathRow label="项目根目录" value={project?.root || '—'} />
          <PathRow label="数据目录" value={project?.data || '—'} />
          <PathRow label="媒体目录" value={project?.media || '—'} />
          <PathRow label="数据库" value={project?.database || '—'} />
        </div>
        <div className="mt-3 grid grid-cols-3 gap-2">
          <MiniStat label="视频" value={formatCount(project?.videoCount || 0)} />
          <MiniStat label="占用" value={`${videoGB.toFixed(1)} GB`} />
          <MiniStat label="封面" value={formatCount(coverCount)} />
        </div>
        <div className="mt-3 flex flex-wrap gap-1.5">
          <Button size="sm" variant="outline" onClick={openProject} disabled={busy === 'open'}><ExternalLink className="size-3.5" /> 打开项目文件夹</Button>
          <Button size="sm" variant="ghost" onClick={importFiles} disabled={busy === 'import'}><ScanLine className="size-3.5" /> 识别已有文件</Button>
          <Button size="sm" variant="ghost" onClick={scan} disabled={busy === 'scan'}><RefreshCw className={cn('size-3.5', busy === 'scan' && 'animate-spin')} /> 重新扫描</Button>
        </div>
      </Card>

      <Card className="p-4">
        <SectionTitle icon={Gauge} title="下载设置" sub="保存后立即写入项目配置" />
        <div className="mt-3 space-y-2.5">
          <Field label="代理">
            <Input value={proxy} onChange={e => setProxy(e.target.value)} placeholder="http://127.0.0.1:7890（留空 = 系统代理）" />
          </Field>
          <Field label="源索引地址">
            <Input value={sourceIndex} onChange={e => setSourceIndex(e.target.value)} placeholder="https://…/sources/index.json（留空 = 内置默认）" />
            <div className="mt-1 text-[9px] text-[color:var(--subtle)]">「源管理 → 可用源」从这个 JSON 读取可安装/可更新的源包。</div>
          </Field>
          <Field label="FFmpeg 路径">
            <Input value={ffmpeg} onChange={e => setFfmpeg(e.target.value)} placeholder="D:\ffmpeg\bin\ffmpeg.exe（留空 = 自动查找）" />
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[9px] text-[color:var(--subtle)]">
              <span>留空会依次查找 PATH、程序目录、工具目录和常见安装目录。</span>
              <Button size="sm" variant="outline" className="h-6 px-2 text-[9px]" onClick={installFFmpeg} disabled={busy === 'ffmpeg'}>
                <Download className={cn('size-3', busy === 'ffmpeg' && 'animate-pulse')} />
                {busy === 'ffmpeg' ? '下载中…' : '自动下载 FFmpeg'}
              </Button>
            </div>
          </Field>
          <Field label="并发下载数">
            <Select value={concurrent} onValueChange={setConcurrent} options={Array.from({ length: 16 }, (_, i) => ({ value: String(i + 1), label: String(i + 1) }))} className="w-[100px]" aria-label="并发下载数" />
          </Field>
          <Field label="HTTP 下载限速">
            <div className="flex items-center gap-2">
              <Input type="number" min={0} max={102400} value={httpLimit} onChange={e => setHttpLimit(e.target.value)} className="w-[96px]" aria-label="HTTP 限速 KB/s" />
              <span className="text-[10px] text-[color:var(--subtle)]">KB/s，0 表示不限速</span>
            </div>
          </Field>
          <Field label="最多尝试次数">
            <div className="flex items-center gap-2">
              <Input type="number" min={1} max={8} value={retryCount} onChange={e => setRetryCount(e.target.value)} className="w-[72px]" aria-label="最多尝试次数" />
              <span className="text-[10px] text-[color:var(--subtle)]">次，失败后间隔</span>
              <Input type="number" min={1} max={30} value={retryDelay} onChange={e => setRetryDelay(e.target.value)} className="w-[72px]" aria-label="重试间隔秒" />
              <span className="text-[10px] text-[color:var(--subtle)]">秒</span>
            </div>
          </Field>
        </div>
        <Button className="mt-3" size="sm" onClick={save} disabled={busy === 'save'}>
          <Save className="size-3.5" /> {busy === 'save' ? '保存中…' : '保存下载设置'}
        </Button>
      </Card>

      <Card className="p-4">
        <SectionTitle icon={LayoutGrid} title="界面" sub="只影响本地显示偏好" />
        <div className="mt-3">
          <div className="ui-field-label mb-1.5">卡片密度</div>
          <div className="flex items-center gap-1 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] p-0.5">
            {([
              ['compact', '紧凑'],
              ['cozy', '标准'],
              ['large', '宽松'],
            ] as const).map(([value, label]) => (
              <Button key={value} size="sm" variant={density === value ? 'outline' : 'ghost'} className="h-7 flex-1 px-2" onClick={() => setDensity(value)}>{label}</Button>
            ))}
          </div>
          <div className="mt-2 text-[9.5px] leading-5 text-[color:var(--subtle)]">宽松会放大画廊卡片和封面，适合大屏浏览；紧凑适合一次看更多。</div>
          <div className="mt-4">
            <div className="ui-field-label mb-1.5">长按空格倍速</div>
            <div className="flex items-center gap-1 rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] p-0.5">
              {[1.5, 2, 2.5, 3].map(rate => (
                <Button key={rate} size="sm" variant={longPressRate === rate ? 'outline' : 'ghost'} className="h-7 flex-1 px-2" onClick={() => setLongPressRate(rate)}>{rate}×</Button>
              ))}
            </div>
            <div className="mt-2 text-[9.5px] text-[color:var(--subtle)]">预览里长按空格按这个倍速播放，松开恢复 1×。</div>
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <SectionTitle icon={Wrench} title="维护" sub="扫描、封面与下载残留" />
        <div className="mt-3 divide-y divide-[color:var(--border)]">
          <ActionRow title="修复缺失封面" desc={`当前缺失 ${data?.stats?.coverMissing || 0} 条，最多处理 40 条`} onClick={repairCovers} busy={busy === 'covers'} icon={Image} />
          <ActionRow title="整理 .part" desc="把已完成但仍残留 .part 后缀的文件改名" onClick={repairParts} busy={busy === 'parts'} icon={RotateCcw} />
          <ActionRow title="清理封面缓存" desc="删除本地封面/缩略图缓存，不删除视频" onClick={clearCache} busy={busy === 'cache'} icon={Trash2} />
          <ActionRow title="解除下载占用" desc="结束残留进程，解除「已有任务在跑」" onClick={resetDownload} busy={busy === 'reset'} icon={AlertTriangle} danger />
        </div>
      </Card>

      <Card className="p-4">
        <SectionTitle icon={Database} title="数据体检" sub="找出数据库里指向丢失/空文件的条目" />
        <div className="mt-3 divide-y divide-[color:var(--border)]">
          <ActionRow
            title="无效视频文件"
            desc={invalidQuery.isLoading ? '正在统计…' : `共 ${invalidQuery.data?.count ?? 0} 个，点击查看列表`}
            onClick={() => setInvalidOpen(true)}
            icon={AlertTriangle}
            actionLabel="查看"
          />
        </div>
      </Card>

      <Card className="p-4">
        <SectionTitle icon={Database} title="运行指标" sub="后端单线程，慢请求会影响所有页面" />
        <div className="mt-3 grid grid-cols-2 gap-2">
          <MiniStat label="请求总数" value={formatCount(metrics?.total || 0)} />
          <MiniStat label="平均耗时" value={`${metrics?.avgMs ?? 0} ms`} />
          <MiniStat label="最慢请求" value={`${metrics?.maxMs ?? 0} ms`} />
          <MiniStat label="运行时长" value={`${Math.round((metrics?.uptime || 0) / 60)} 分钟`} />
        </div>
        <div className="mt-3 text-[9.5px] text-[color:var(--subtle)]">
          最慢：{metrics?.maxPath || '—'}
        </div>
        {metrics?.slow?.length ? (
          <div className="mt-2 max-h-[90px] space-y-1 overflow-auto">
            {metrics.slow.slice(0, 8).map((s, i) => (
              <div key={i} className="flex justify-between gap-2 text-[9.5px] text-[color:var(--warning)]">
                <span className="truncate">{s.path || '—'}</span><span>{s.ms || 0} ms</span>
              </div>
            ))}
          </div>
        ) : null}
      </Card>

      <Card className="p-4">
        <SectionTitle icon={RefreshCw} title="版本与更新" sub="检查 GitHub Releases；Windows 桌面版可下载并安装" />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => void checkUpdate()} disabled={checking}>
            <RefreshCw className={cn('size-3.5', checking && 'animate-spin')} />
            {checking ? '检查中…' : '检查更新'}
          </Button>
          {updateInfo?.hasUpdate && desktop && platform === 'windows' ? (
            <Button size="sm" onClick={() => void applyUpdate()} disabled={applying}>
              <Download className="size-3.5" />
              {applying ? '下载中…' : '下载并安装'}
            </Button>
          ) : null}
          {updateErr ? <span className="text-[10px] text-[color:var(--danger)]">{updateErr}</span> : null}
          {updateInfo ? (
            updateInfo.hasUpdate ? (
              <span className="text-[10px] text-[color:var(--muted)]">
                发现 v{updateInfo.latest}，
                <a
                  href={updateInfo.url}
                  target="_blank"
                  rel="noreferrer"
                  className="text-[color:var(--primary-2)] underline underline-offset-2"
                >
                  前往下载
                </a>
              </span>
            ) : (
              <span className="text-[10px] text-[color:var(--success)]">已是最新（v{updateInfo.current}）</span>
            )
          ) : null}
        </div>
        <p className="mt-2 text-[9px] leading-relaxed text-[color:var(--subtle)]">
          自动安装会从 GitHub 下载安装包并校验 SHA256；当前未做代码签名，Windows 可能提示「未知发布者」。
        </p>
      </Card>

      <Card className="p-4 min-[1100px]:col-span-2">
        <div className="flex items-center justify-between gap-2">
          <SectionTitle icon={RefreshCw} title="日志" sub="最近 100 条；错误和慢请求会标记颜色" />
          <Button size="sm" variant="ghost" onClick={() => refetch()} disabled={isFetching}><RefreshCw className={cn('size-3.5', isFetching && 'animate-spin')} /> 刷新</Button>
        </div>
        <div className="mt-2 max-h-[280px] overflow-auto rounded-lg border border-[color:var(--border)] bg-[color:var(--background-2)] p-2">
          {logs.length ? logs.slice(0, 100).map((l, i) => (
            <div key={i} className="flex gap-2 border-b border-[color:var(--border)] py-1 text-[9.5px] last:border-0">
              <span className="w-14 shrink-0 text-[color:var(--subtle)]">{l.time || '—'}</span>
              <span className={cn('w-16 shrink-0 truncate', (l.kind || '').includes('err') ? 'text-[color:var(--danger)]' : (l.kind || '').includes('warn') ? 'text-[color:var(--warning)]' : 'text-[color:var(--primary-2)]')}>{l.kind || 'log'}</span>
              <span className="min-w-0 flex-1 whitespace-pre-wrap break-all text-[color:var(--muted)]">
                {String(l.message || '').split(/(\/api\/diagnostics\/scrape\/file\?[^\s]+)/g).map((part, idx) =>
                  part.startsWith('/api/diagnostics/scrape/file?')
                    ? <a key={idx} href={part} className="text-[color:var(--primary-2)] underline underline-offset-2" download>{part}</a>
                    : <span key={idx}>{part}</span>,
                )}
              </span>
            </div>
          )) : <div className="p-4 text-center text-[10px] text-[color:var(--subtle)]">暂无日志</div>}
        </div>
        {metrics?.clientErrors?.length ? (
          <div className="mt-2">
            <div className="mb-1 text-[9.5px] text-[color:var(--danger)]">前端错误（最近 {metrics.clientErrors.length} 条）</div>
            <div className="max-h-[90px] overflow-auto rounded-lg border border-[color:color-mix(in_srgb,var(--danger)_25%,var(--border))] bg-[color:color-mix(in_srgb,var(--danger)_4%,transparent)] p-2 text-[9.5px] text-[color:var(--muted)]">
              {metrics.clientErrors.slice(0, 10).map((e, i) => <div key={i} className="flex gap-2 py-0.5"><span className="w-14 shrink-0 text-[color:var(--subtle)]">{e.t}</span><span className="truncate">{e.msg}</span></div>)}
            </div>
          </div>
        ) : null}
      </Card>

      <Dialog open={invalidOpen} onOpenChange={setInvalidOpen}>
        <DialogContent
          title="无效视频文件"
          description="这些条目的本地文件已丢失、为空或路径异常；可打开所在位置排查。"
          className="w-[min(780px,calc(100vw-40px))]"
        >
          <div className="max-h-[60vh] overflow-auto p-3">
            {invalidItems.length ? invalidItems.map(it => (
              <div key={it.token} className="flex items-center gap-3 border-b border-[color:var(--border)] py-2 text-[10px] last:border-b-0">
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{it.title || it.token}</div>
                  <div className="truncate text-[9px] text-[color:var(--subtle)]" title={it.path}>{it.path}</div>
                </div>
                <Badge tone="bad">{it.reason}</Badge>
                <Button size="sm" variant="outline" onClick={() => void revealInvalid(it.token)}>打开位置</Button>
              </div>
            )) : (
              <div className="p-6 text-center text-[10px] text-[color:var(--subtle)]">没有无效文件</div>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function SectionTitle({ icon: Icon, title, sub }: { icon: React.ComponentType<{ className?: string }>; title: string; sub: string }) {
  return (
    <div className="flex items-start gap-2">
      <span className="grid size-7 shrink-0 place-items-center rounded-lg border border-[color:var(--border)] text-[color:var(--primary-2)]"><Icon className="size-3.5" /></span>
      <div>
        <div className="text-[11.5px] font-semibold">{title}</div>
        <div className="text-[9.5px] text-[color:var(--subtle)]">{sub}</div>
      </div>
    </div>
  )
}

function PathRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 py-1.5">
      <span className="w-16 shrink-0 text-[9px] text-[color:var(--subtle)]">{label}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-[9.5px] text-[color:var(--muted)]" title={value}>{value}</span>
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 py-1.5">
      <div className="text-[9px] text-[color:var(--subtle)]">{label}</div>
      <div className="truncate text-[11px] font-medium">{value}</div>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="ui-field-label mb-1.5">{label}</div>
      {children}
    </div>
  )
}

function ActionRow({ title, desc, onClick, busy, icon: Icon, danger, actionLabel = '执行' }: { title: string; desc: string; onClick: () => void; busy?: boolean; icon: React.ComponentType<{ className?: string }>; danger?: boolean; actionLabel?: string }) {
  return (
    <div className="flex items-center gap-3 py-2.5">
      <Icon className={cn('size-3.5 shrink-0', danger ? 'text-[color:var(--danger)]' : 'text-[color:var(--subtle)]')} />
      <div className="min-w-0 flex-1">
        <div className="text-[11px] font-medium">{title}</div>
        <div className="text-[9.5px] text-[color:var(--subtle)]">{desc}</div>
      </div>
      <Button size="sm" variant={danger ? 'danger' : 'outline'} onClick={onClick} disabled={busy}>{busy ? '处理中…' : actionLabel}</Button>
    </div>
  )
}

function SettingsSkeleton() {
  return (
    <div className="grid gap-3 p-4 min-[1100px]:grid-cols-2">
      {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} className="h-[220px] rounded-[14px]" />)}
    </div>
  )
}
