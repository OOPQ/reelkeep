// 抓取数量：500 => 前 500 条；300,400 => 第 300–400 条。
// 只限制单次抓取总数（≤500），起点/终点不限制（由自动翻页负责）。
export const MAX_SCRAPE_COUNT = 500

export function parseScrapeRange(raw: string): { skip: number; limit: number } | null {
  const s = raw.trim().replace(/，/g, ',').replace(/\s+/g, '')
  if (!/^\d+(,\d+)?$/.test(s)) return null
  const [a, b] = s.split(',').map(Number)
  if (b === undefined) {
    return a >= 1 && a <= MAX_SCRAPE_COUNT ? { skip: 0, limit: a } : null
  }
  if (a < 1 || b < a) return null
  const limit = b - a + 1
  if (limit > MAX_SCRAPE_COUNT) return null
  return { skip: a - 1, limit }
}
