import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clampVolume, hasStoredPlayerPrefs, loadPlayerMuted, loadPlayerVolume, savePlayerMuted, savePlayerVolume } from './player-prefs'

let data: Map<string, string>

beforeEach(() => {
  data = new Map()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => { data.set(key, value) },
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('player prefs', () => {
  it('defaults to full volume and unmuted', () => {
    expect(loadPlayerVolume()).toBe(1)
    expect(loadPlayerMuted()).toBe(false)
    expect(hasStoredPlayerPrefs()).toBe(false)
  })

  it('remembers volume and mute state', () => {
    savePlayerVolume(0.35)
    savePlayerMuted(true)
    expect(hasStoredPlayerPrefs()).toBe(true)
    expect(loadPlayerVolume()).toBe(0.35)
    expect(loadPlayerMuted()).toBe(true)
    savePlayerMuted(false)
    expect(loadPlayerMuted()).toBe(false)
  })

  it('clamps volume', () => {
    expect(clampVolume(-1)).toBe(0)
    expect(clampVolume(2)).toBe(1)
    expect(clampVolume(Number.NaN)).toBe(1)
  })
})
