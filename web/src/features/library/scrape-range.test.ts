import { describe, expect, it } from 'vitest'
import { parseScrapeRange } from './scrape-range'

describe('parseScrapeRange', () => {
  it('单个数字表示前 N 条', () => {
    expect(parseScrapeRange('500')).toEqual({ skip: 0, limit: 500 })
    expect(parseScrapeRange(' 50 ')).toEqual({ skip: 0, limit: 50 })
    expect(parseScrapeRange('501')).toBeNull()
  })

  it('逗号表示范围（含中文逗号）', () => {
    expect(parseScrapeRange('300,400')).toEqual({ skip: 299, limit: 101 })
    expect(parseScrapeRange('300，400')).toEqual({ skip: 299, limit: 101 })
    expect(parseScrapeRange('501,501')).toEqual({ skip: 500, limit: 1 })
    expect(parseScrapeRange('300,600')).toEqual({ skip: 299, limit: 301 })
  })

  it('非法输入返回 null', () => {
    expect(parseScrapeRange('')).toBeNull()
    expect(parseScrapeRange('400,300')).toBeNull()
    expect(parseScrapeRange('0')).toBeNull()
    expect(parseScrapeRange('300,6000')).toBeNull()
    expect(parseScrapeRange('1,600')).toBeNull()
    expect(parseScrapeRange('abc')).toBeNull()
  })
})
