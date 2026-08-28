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

// Vercel(Geist)风格:黑白主色、细边框、小圆角、无按钮阴影;
// 语义色克制(蓝 #0070f3 / 绿 / 琥珀 / 红),仅用于状态表达。
export function buildTheme(dark: boolean): ThemeConfig {
  return {
    algorithm: dark ? antd.darkAlgorithm : antd.defaultAlgorithm,
    token: {
      colorPrimary: dark ? '#ededed' : '#171717',
      // 暗色下主按钮为白底,文字必须反转为黑,否则白底白字
      colorTextLightSolid: dark ? '#000000' : '#ffffff',
      colorInfo: '#0070f3',
      colorLink: dark ? '#3291ff' : '#0070f3',
      colorSuccess: '#16a34a',
      colorWarning: '#f59e0b',
      colorError: '#e5484d',
      borderRadius: 6,
      colorBgLayout: dark ? '#000000' : '#fafafa',
      colorBgContainer: dark ? '#0a0a0a' : '#ffffff',
      colorBorder: dark ? '#2e2e2e' : '#e5e5e5',
      colorBorderSecondary: dark ? '#242424' : '#eaeaea',
      colorText: dark ? '#ededed' : '#171717',
      colorTextSecondary: dark ? '#a1a1a1' : '#666666',
      colorTextTertiary: '#8f8f8f',
      controlOutline: dark ? 'rgba(50, 145, 255, 0.24)' : 'rgba(0, 112, 243, 0.18)',
      fontFamily:
        "'Geist Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif",
      fontFamilyCode: "'Geist Mono', 'Cascadia Code', Consolas, Menlo, monospace",
    },
    components: {
      Button: { primaryShadow: 'none', defaultShadow: 'none', dangerShadow: 'none', fontWeight: 500 },
      Card: { borderRadiusLG: 10 },
      Modal: { borderRadiusLG: 12 },
      Table: { headerBg: dark ? '#111111' : '#fafafa' },
      Menu: {
        itemBg: 'transparent',
        itemColor: dark ? '#a1a1a1' : '#555555',
        itemHoverBg: dark ? 'rgba(255, 255, 255, 0.06)' : 'rgba(0, 0, 0, 0.04)',
        itemHoverColor: dark ? '#ededed' : '#171717',
        itemSelectedBg: dark ? '#1f1f1f' : '#ececec',
        itemSelectedColor: dark ? '#ffffff' : '#000000',
        itemBorderRadius: 6,
        itemMarginInline: 10,
        itemHeight: 36,
        activeBarBorderWidth: 0,
      },
    },
  }
}
