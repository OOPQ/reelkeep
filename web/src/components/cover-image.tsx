import * as React from 'react'
import { ImageOff } from 'lucide-react'
import { cn, coverUrl } from '@/lib/utils'
import { useUiStore } from '@/store/ui'

type CoverImageProps = {
  token: string
  alt?: string
  className?: string
  imgClassName?: string
  full?: boolean
  fit?: 'cover' | 'contain'
  eager?: boolean
}

export function CoverImage({
  token,
  alt = '',
  className,
  imgClassName,
  full = false,
  fit = 'cover',
  eager = false,
}: CoverImageProps) {
  const coverRev = useUiStore(s => s.coverRev)
  const src = `${coverUrl(token, full)}&c=${coverRev}`
  // 用 src 记录加载结果，而不是布尔值 + effect。否则切到下一条时，
  // 缓存的图片可能先触发 onLoad，随后 effect 把 loaded 重置为 false，骨架屏就永远不消失。
  const [loadedSrc, setLoadedSrc] = React.useState<string | null>(null)
  const [failedSrc, setFailedSrc] = React.useState<string | null>(null)
  const loaded = loadedSrc === src
  const failed = failedSrc === src

  return (
    <div className={cn('relative overflow-hidden bg-[color:var(--surface-2)]', className)}>
      {!loaded && !failed ? <div className="ui-skeleton absolute inset-0" /> : null}
      {failed ? (
        <div className="absolute inset-0 grid place-items-center bg-[linear-gradient(135deg,var(--surface-3),var(--surface-2)_55%,var(--surface))] text-[color:var(--subtle)]">
          <ImageOff className="size-5 opacity-60" />
        </div>
      ) : (
        <img
          key={src}
          src={src}
          alt={alt}
          loading={eager ? 'eager' : 'lazy'}
          decoding="async"
          draggable={false}
          onLoad={() => setLoadedSrc(src)}
          onError={() => setFailedSrc(src)}
          className={cn(
            'h-full w-full transition-opacity duration-200',
            fit === 'cover' ? 'object-cover' : 'object-contain',
            loaded ? 'opacity-100' : 'opacity-0',
            imgClassName,
          )}
        />
      )}
    </div>
  )
}
