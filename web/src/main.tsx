import React from 'react'
import ReactDOM from 'react-dom/client'
import { App as AntApp, ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { buildTheme, ThemeContext, useThemeState } from './theme'
import './index.css'

function Root() {
  const themeState = useThemeState()
  return (
    <ThemeContext.Provider value={themeState}>
      <ConfigProvider locale={zhCN} theme={buildTheme(themeState.dark)}>
        <AntApp>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </AntApp>
      </ConfigProvider>
    </ThemeContext.Provider>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Root />
  </React.StrictMode>,
)
