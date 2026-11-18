import { RotateCcw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// 统一的接口错误提示：不要把它显示成“空数据”，否则用户会以为内容没了。
export function ApiError({
  title = '加载失败',
  error,
  onRetry,
  busy,
  className,
}: {
  title?: string
  error: unknown
  onRetry?: () => void
  busy?: boolean
  className?: string
}) {
  const message = error instanceof Error ? error.message : String(error || '未知错误')
  return (
    <div className={cn('rounded-[var(--radius-card)] border border-[color:color-mix(in_srgb,var(--danger)_35%,var(--border))] bg-[color:color-mix(in_srgb,var(--danger)_5%,var(--surface))] p-6', className)}>
      <div className="text-[10px] tracking-[.6px] text-[color:var(--danger)]">API ERROR</div>
      <div className="mt-1 text-[14px] font-semibold">{title}</div>
      <div className="mt-1.5 max-w-[560px] text-[11px] leading-5 text-[color:var(--muted)]">{message}</div>
      {onRetry ? (
        <Button className="mt-3" size="sm" variant="outline" onClick={onRetry} disabled={busy}>
          <RotateCcw className={cn('size-3.5', busy && 'animate-spin')} /> 重试
        </Button>
      ) : null}
    </div>
  )
}
