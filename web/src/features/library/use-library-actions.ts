import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { post } from '@/lib/api'
import { copyText, copyValue, type CopyKind } from '@/lib/utils'
import { unlockAudio } from '@/lib/sound'
import type { Row } from '@/types'

export function useLibraryActions() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['state'] })
    qc.invalidateQueries({ queryKey: ['library-rows'] })
  }

  const download = async (tokens: string[], limit = 0) => {
    unlockAudio()
    try {
      const r = await post<{ ok: boolean; error?: string; started?: number; skipped?: number; errors?: string[] }>('/api/download', {
        tokens,
        limit,
        status: 'new',
      })
      if (!r.ok) throw new Error(r.error || '开始下载失败')
      const started = r.started || 0
      const skipped = r.skipped || 0
      toast.success(`已加入下载队列 · ${started} 条${skipped ? `，跳过 ${skipped} 条` : ''}`, {
        action: { label: '查看队列', onClick: () => navigate('/queue') },
      })
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const fav = async (tokens: string[], on: boolean) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/fav', { tokens, on })
      if (!r.ok) throw new Error(r.error || '操作失败')
      toast.success(on ? '已收藏' : '已取消收藏')
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const mark = async (tokens: string[], status: string) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/mark', { tokens, status })
      if (!r.ok) throw new Error(r.error || '操作失败')
      toast.success(status === 'done' ? '已标记完成' : '已更新状态')
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const watched = async (tokens: string[], on: boolean) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/watched', { tokens, on })
      if (!r.ok) throw new Error(r.error || '操作失败')
      toast.success(on ? '已标记已看' : '已取消已看')
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const rate = async (tokens: string[], value: number) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/rate', { tokens, value })
      if (!r.ok) throw new Error(r.error || '评分失败')
      toast.success(value ? `已评分 ${value} 星` : '已清除评分')
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const recycle = async (tokens: string[]) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/mark', { tokens, status: 'recycled' })
      if (!r.ok) throw new Error(r.error || '移入回收站失败')
      toast.success(`已移入回收站 · ${tokens.length} 条`, {
        action: {
          label: '撤销',
          onClick: async () => {
            try {
              const rr = await post<{ ok: boolean; error?: string }>('/api/restore', { tokens })
              if (!rr.ok) throw new Error(rr.error || '撤销失败')
              toast.success('已撤销')
              invalidate()
            } catch (e) {
              toast.error((e as Error).message)
            }
          },
        },
      })
      invalidate()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const reveal = async (token: string) => {
    try {
      const r = await post<{ ok: boolean; error?: string }>('/api/reveal', { token })
      if (!r.ok) throw new Error(r.error || '定位失败')
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const copy = async (tokens: string[], rows: Row[], kind: CopyKind = 'page') => {
    const values = tokens.map(t => {
      const row = rows.find(r => r.token === t)
      return row ? copyValue(row, kind) : ''
    }).filter(Boolean)
    const label: Record<CopyKind, string> = {
      download: '下载链接',
      page: '页面链接',
      title: '标题',
      token: 'Token',
    }
    if (!values.length) {
      toast.info(kind === 'download' ? '选中的条目没有可复制的下载地址' : '没有可复制的内容')
      return
    }
    if (await copyText(values.join('\n'))) toast.success(`已复制 ${values.length} 个${label[kind]}`)
    else toast.error('复制失败')
  }

  return { download, fav, mark, watched, rate, recycle, reveal, copy }
}
