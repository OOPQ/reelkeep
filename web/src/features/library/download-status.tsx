import type { DownloadItem, Row } from '@/types'
import { cn } from '@/lib/utils'

export function effectiveDownloadStatus(row: Row, item?: DownloadItem) {
  if (row.status !== 'new') return row.status
  return item?.state || 'new'
}

export function downloadPercent(item?: DownloadItem) {
  const total = Number(item?.expected || item?.total || 0)
  if (!total || total <= 0) return 0
  return Math.min(99, Math.max(0, Math.round((Number(item?.bytes || 0) / total) * 100)))
}

export function downloadButtonLabel(row: Row, item?: DownloadItem) {
  const state = effectiveDownloadStatus(row, item)
  const pct = downloadPercent(item)
  switch (state) {
    case 'queued': return '排队中'
    case 'downloading': return pct > 0 ? `下载中 ${pct}%` : '下载中'
    case 'paused': return '已暂停'
    case 'failed': return '下载失败'
    case 'dead': return '源失效'
    case 'cancelled': return '已取消'
    case 'done': return '已完成'
    default: return '下载'
  }
}

export function isDownloadBusy(row: Row, item?: DownloadItem) {
  return ['queued', 'downloading', 'paused'].includes(effectiveDownloadStatus(row, item))
}

// 把底层下载错误转成可读原因；完整错误仍放在 title 里。
export function downloadErrorText(item?: DownloadItem) {
  const raw = (item?.error || '').trim()
  if (!raw) return '未知原因'
  const lower = raw.toLowerCase()
  if (lower.includes('no space') || lower.includes('disk full')) return '磁盘空间不足'
  if (lower.includes('connection refused') || lower.includes('dialcontext') || lower.includes('connect:') || lower.includes('no route')) return '无法连接下载地址'
  if (lower.includes('timeout') || lower.includes('deadline exceeded')) return '连接超时'
  if (lower.includes('404') || lower.includes('not found')) return '资源不存在（404）'
  if (lower.includes('403') || lower.includes('forbidden')) return '没有访问权限（403）'
  if (lower.includes('401')) return '需要登录（401）'
  if (lower.includes('certificate') || lower.includes('x509') || lower.includes('tls')) return 'HTTPS 证书错误'
  if (lower.includes('context canceled') || lower.includes('cancelled')) return '已取消'
  return raw.length > 60 ? `${raw.slice(0, 60)}…` : raw
}

// DownloadProgress 是下载中的细进度条 + 百分比，卡片/浮窗共用。
export function DownloadProgress({ item, className, tone = 'dark' }: { item?: DownloadItem; className?: string; tone?: 'dark' | 'light' }) {
  if (!item || !['queued', 'downloading', 'paused'].includes(item.state)) return null
  const pct = downloadPercent(item)
  const queued = item.state === 'queued'
  return (
    <div className={cn('flex items-center gap-1.5', className)}>
      <div className={cn('h-1 min-w-[36px] flex-1 overflow-hidden rounded-full', tone === 'dark' ? 'bg-white/20' : 'bg-black/15')}>
        <div
          className={cn('h-full rounded-full transition-[width] duration-300', queued ? 'bg-[color:var(--warning)]' : 'bg-[color:var(--primary-2)]')}
          style={{ width: `${Math.max(queued ? 6 : 2, pct)}%` }}
        />
      </div>
      <span className={cn('shrink-0 text-[9px] tabular-nums', tone === 'dark' ? 'text-white/85' : 'text-[color:var(--muted)]')}>
        {queued ? '排队中' : `${pct}%`}
      </span>
    </div>
  )
}