import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'

export type ThemeMode = 'light' | 'dark'

type ThemeCtxValue = {
  mode: ThemeMode
  resolved: ThemeMode
  setMode: (m: ThemeMode) => void
  toggle: () => void
}

const STORAGE_KEY = 'hy2p.theme'
const ThemeCtx = createContext<ThemeCtxValue | null>(null)

/** 读取已保存的主题；没有保存过就跟随系统首次取值。 */
function readStored(): ThemeMode {
  const v = localStorage.getItem(STORAGE_KEY)
  if (v === 'light' || v === 'dark') return v
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

/**
 * 主题提供者（只有浅色 / 深色两种，不含“跟随系统”选项）。
 *
 * 深色模式并非简单反色：由 styles.css 中独立的 dark 令牌定义。
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(readStored)

  useEffect(() => {
    document.documentElement.dataset.theme = mode
    document.documentElement.style.colorScheme = mode
    // 同步浏览器 / Android 状态栏颜色，避免深色模式下顶部出现一条亮条
    const color = mode === 'dark' ? '#0f1413' : '#f6f8f7'
    let meta = document.querySelector('meta[name="theme-color"]')
    if (!meta) {
      meta = document.createElement('meta')
      meta.setAttribute('name', 'theme-color')
      document.head.appendChild(meta)
    }
    meta.setAttribute('content', color)
  }, [mode])

  const value = useMemo<ThemeCtxValue>(
    () => ({
      mode,
      resolved: mode,
      setMode: (m: ThemeMode) => {
        localStorage.setItem(STORAGE_KEY, m)
        setModeState(m)
      },
      toggle: () => {
        const next: ThemeMode = mode === 'dark' ? 'light' : 'dark'
        localStorage.setItem(STORAGE_KEY, next)
        setModeState(next)
      },
    }),
    [mode],
  )

  return <ThemeCtx.Provider value={value}>{children}</ThemeCtx.Provider>
}

export function useTheme(): ThemeCtxValue {
  const v = useContext(ThemeCtx)
  if (!v) throw new Error('useTheme 必须在 ThemeProvider 内使用')
  return v
}
