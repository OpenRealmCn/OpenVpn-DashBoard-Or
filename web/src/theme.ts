import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { theme as antd } from 'antd'
import type { ThemeConfig } from 'antd'

const STORAGE_KEY = 'ovpn-theme'

export interface ThemeState {
  dark: boolean
  toggle: () => void
}

export const ThemeContext = createContext<ThemeState>({ dark: false, toggle: () => {} })

export const useTheme = () => useContext(ThemeContext)

// 根组件持有主题状态:默认跟随系统偏好,手动切换后持久化到 localStorage
export function useThemeState(): ThemeState {
  const [dark, setDark] = useState<boolean>(() => {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'dark' || saved === 'light') return saved === 'dark'
    return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
  })

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    localStorage.setItem(STORAGE_KEY, dark ? 'dark' : 'light')
  }, [dark])

  const toggle = useCallback(() => setDark((d) => !d), [])
  return useMemo(() => ({ dark, toggle }), [dark, toggle])
}

export function buildTheme(dark: boolean): ThemeConfig {
  return {
    algorithm: dark ? antd.darkAlgorithm : antd.defaultAlgorithm,
    token: {
      colorPrimary: '#6366f1',
      colorInfo: '#6366f1',
      colorSuccess: '#22c55e',
      colorWarning: '#f59e0b',
      colorError: '#ef4444',
      borderRadius: 10,
      colorBgLayout: dark ? '#0e1220' : '#f4f6fb',
      fontFamily:
        "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
    },
    components: {
      Card: { borderRadiusLG: 14 },
      Modal: { borderRadiusLG: 14 },
      Menu: {
        darkItemBg: 'transparent',
        darkSubMenuItemBg: 'transparent',
        darkItemSelectedBg: '#6366f1',
        darkItemHoverBg: 'rgba(255, 255, 255, 0.08)',
        itemBorderRadius: 8,
        itemMarginInline: 10,
      },
    },
  }
}
