// 播放器音量/静音偏好：切视频和重启应用后都沿用上次的值。
const VOLUME_KEY = 'pektino.player.volume'
const MUTED_KEY = 'pektino.player.muted'
const PLAY_MODE_KEY = 'pektino.player.playMode'

export type PlayMode = 'off' | 'one' | 'next' | 'all'

export function normalizePlayMode(value: unknown): PlayMode {
  return value === 'one' || value === 'next' || value === 'all' ? value : 'off'
}

export function loadPlayerPlayMode(): PlayMode | null {
  try {
    const raw = localStorage.getItem(PLAY_MODE_KEY)
    return raw == null ? null : normalizePlayMode(raw)
  } catch {
    return null
  }
}

export function savePlayerPlayMode(mode: PlayMode) {
  try {
    localStorage.setItem(PLAY_MODE_KEY, mode)
  } catch {}
}

export function clampVolume(value: number) {
  if (!Number.isFinite(value)) return 1
  return Math.min(1, Math.max(0, value))
}

export function hasStoredPlayerPrefs() {
  try {
    return localStorage.getItem(VOLUME_KEY) != null || localStorage.getItem(MUTED_KEY) != null
  } catch {
    return false
  }
}

export function loadPlayerVolume() {
  try {
    const raw = localStorage.getItem(VOLUME_KEY)
    if (raw == null || raw === '') return 1
    return clampVolume(Number(raw))
  } catch {
    return 1
  }
}

export function savePlayerVolume(value: number) {
  try {
    localStorage.setItem(VOLUME_KEY, String(clampVolume(value)))
  } catch {}
}

export function loadPlayerMuted() {
  try {
    return localStorage.getItem(MUTED_KEY) === '1'
  } catch {
    return false
  }
}

export function savePlayerMuted(muted: boolean) {
  try {
    localStorage.setItem(MUTED_KEY, muted ? '1' : '0')
  } catch {}
}
