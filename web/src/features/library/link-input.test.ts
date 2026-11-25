import { describe, expect, it } from 'vitest'
import { parseScrapeLinks } from './link-input'

describe('parseScrapeLinks', () => {
  it('splits newlines and spaces and dedupes', () => {
    const links = parseScrapeLinks(' https://a.example/v/1\nhttps://a.example/v/2  https://a.example/v/1 ')
    expect(links).toEqual(['https://a.example/v/1', 'https://a.example/v/2'])
  })

  it('accepts comma and semicolon separated links', () => {
    expect(parseScrapeLinks('https://a/1,https://a/2;https://a/3')).toEqual([
      'https://a/1',
      'https://a/2',
      'https://a/3',
    ])
  })

  it('ignores empty input', () => {
    expect(parseScrapeLinks('  \n  ')).toEqual([])
  })
})
