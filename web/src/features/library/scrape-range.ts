// 抓取数量：500 => 前 500 条；300,400 => 第 300–400 条。
// 单次最多取 500 条；自动翻页上限 50 页，因此范围终点最多到第 1000 条。
export const MAX_SCRAPE_COUNT = 500
export const MAX_SCRAPE_POSITION = 1000

export function parseScrapeRange(raw: string): { skip: number; limit: number } | null {
  const s = raw.trim().replace(/，/g, ',').replace(/\s+/g, '')
  if (!/^\d+(,\d+)?$/.test(s)) return null
  const [a, b] = s.split(',').map(Number)
  if (b === undefined) {
    return a >= 1 && a <= MAX_SCRAPE_COUNT ? { skip: 0, limit: a } : null
  }
  if (a < 1 || b < a || b > MAX_SCRAPE_POSITION) return null
  const limit = b - a + 1
  if (limit > MAX_SCRAPE_COUNT) return null
  return { skip: a - 1, limit }
}
