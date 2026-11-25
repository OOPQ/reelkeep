import * as React from 'react'
import { cn } from '@/lib/utils'

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        'w-full resize-y rounded-[9px] border border-[color:var(--border)] bg-[color:var(--surface-2)] px-2.5 py-2 text-[12px] leading-5 text-[color:var(--foreground)] placeholder:text-[color:var(--subtle)] focus-visible:border-[color:var(--border-strong)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[color:color-mix(in_srgb,var(--ring)_55%,transparent)]',
        className,
      )}
      {...props}
    />
  ),
)
Textarea.displayName = 'Textarea'
