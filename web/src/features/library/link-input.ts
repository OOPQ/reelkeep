export const MAX_SCRAPE_LINKS = 50

export function parseScrapeLinks(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const part of raw.split(/[\s,;]+/)) {
    const link = part.trim()
    if (!link || seen.has(link)) continue
    seen.add(link)
    out.push(link)
  }
  return out
}
