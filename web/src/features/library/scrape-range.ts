// 抓取数量：500 => 前 500 条；300,400 => 第 300–400 条。
export function parseScrapeRange(raw: string): { skip: number; limit: number } | null {
  const s = raw.trim().replace(/，/g, ',').replace(/\s+/g, '')
  if (!/^\d+(,\d+)?$/.test(s)) return null
  const [a, b] = s.split(',').map(Number)
  if (b === undefined) {
    return a >= 1 && a <= 500 ? { skip: 0, limit: a } : null
  }
  if (a < 1 || b < a || b > 500) return null
  return { skip: a - 1, limit: b - a + 1 }
}
