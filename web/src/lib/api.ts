import type { AppState } from '@/types'

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let message = `HTTP ${res.status}`
    try {
      const body = await res.json()
      message = body?.error || body?.message || message
    } catch (e) {}
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

let lastRowsRev = 0

export async function fetchState(signal?: AbortSignal, rowsRev = lastRowsRev): Promise<AppState> {
  const suffix = rowsRev > 0 ? `?rowsRev=${rowsRev}` : ''
  const res = await fetch(`/api/state${suffix}`, { signal, cache: 'no-store' })
  const data = await json<AppState>(res)
  if (typeof data.rowsRev === 'number') lastRowsRev = data.rowsRev
  return data
}

export async function post<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return json<T>(res)
}
