import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type ViewMode = 'table' | 'gallery'
export type Theme = 'dark' | 'light'
export type Density = 'compact' | 'cozy' | 'large'
export type TableColKey = 'cover' | 'title' | 'rank' | 'duration' | 'res' | 'views' | 'likes' | 'rating' | 'first_seen' | 'status'

export const TABLE_COL_LABELS: Record<TableColKey, string> = {
  cover: '封面',
  title: '标题',
  rank: '排名',
  duration: '时长',
  res: '分辨率',
  views: '观看',
  likes: '点赞',
  rating: '评分',
  first_seen: '入库',
  status: '状态',
}

export type TableColSetting = { visible: boolean; width: number }

const DEFAULT_TABLE_COLS: Record<TableColKey, TableColSetting> = {
  cover: { visible: true, width: 84 },
  title: { visible: true, width: 320 },
  rank: { visible: true, width: 64 },
  duration: { visible: true, width: 78 },
  res: { visible: true, width: 94 },
  views: { visible: true, width: 76 },
  likes: { visible: true, width: 76 },
  rating: { visible: true, width: 92 },
  first_seen: { visible: true, width: 108 },
  status: { visible: true, width: 74 },
}

type UiState = {
  sidebarCollapsed: boolean
  view: ViewMode
  theme: Theme
  density: Density
  multi: boolean
  tableCols: Record<TableColKey, TableColSetting>
  longPressRate: number
  coverRev: number
  selected: string[]
  previewToken: string | null
  previewAutoplay: boolean
  toggleSidebar: () => void
  setView: (view: ViewMode) => void
  setTheme: (theme: Theme) => void
  setDensity: (density: Density) => void
  setLongPressRate: (rate: number) => void
  bumpCoverRev: () => void
  setMulti: (multi: boolean) => void
  setTableCol: (key: TableColKey, patch: Partial<TableColSetting>) => void
  resetTableCols: () => void
  toggleSelected: (token: string) => void
  setSelected: (tokens: string[]) => void
  clearSelected: () => void
  openPreview: (token: string, autoplay?: boolean) => void
  closePreview: () => void
}

function applyTheme(theme: Theme) {
  if (typeof document === 'undefined') return
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.documentElement.classList.toggle('light', theme === 'light')
  try { localStorage.setItem('pektino.theme', theme) } catch (e) {}
}

export const useUiStore = create<UiState>()(
  persist(
    (set, get) => ({
      sidebarCollapsed: false,
      view: 'table',
      theme: 'dark',
      density: 'large',
      multi: false,
      tableCols: DEFAULT_TABLE_COLS,
      longPressRate: 2,
      coverRev: 0,
      selected: [],
      previewToken: null,
      previewAutoplay: false,
      toggleSidebar: () => set(s => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      setView: view => set({ view }),
      setTheme: theme => {
        applyTheme(theme)
        set({ theme })
      },
      setDensity: density => set({ density }),
      setLongPressRate: longPressRate => set({ longPressRate }),
      bumpCoverRev: () => set(s => ({ coverRev: s.coverRev + 1 })),
      setMulti: multi => set(multi ? { multi } : { multi, selected: [] }),
      setTableCol: (key, patch) => set(s => ({
        tableCols: { ...s.tableCols, [key]: { ...s.tableCols[key], ...patch } },
      })),
      resetTableCols: () => set({ tableCols: DEFAULT_TABLE_COLS }),
      toggleSelected: token => set(s => ({
        selected: s.selected.includes(token) ? s.selected.filter(t => t !== token) : [...s.selected, token],
      })),
      setSelected: tokens => set({ selected: tokens }),
      clearSelected: () => set({ selected: [] }),
      openPreview: (token, autoplay = false) => set({ previewToken: token, previewAutoplay: autoplay }),
      closePreview: () => set({ previewToken: null, previewAutoplay: false }),
    }),
    {
      name: 'pektino.ui',
      partialize: s => ({ sidebarCollapsed: s.sidebarCollapsed, view: s.view, theme: s.theme, density: s.density, longPressRate: s.longPressRate, coverRev: s.coverRev, tableCols: s.tableCols }),
      onRehydrateStorage: () => state => {
        if (state) applyTheme(state.theme)
      },
    },
  ),
)
