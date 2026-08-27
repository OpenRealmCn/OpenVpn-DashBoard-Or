import { useCallback, useEffect, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { Spin } from 'antd'
import { api } from './api/client'
import type { Session } from './types'
import { SessionContext } from './session'
import AppLayout from './components/AppLayout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import InstallWizard from './pages/InstallWizard'
import Clients from './pages/Clients'
import Maintenance from './pages/Maintenance'
import Nodes from './pages/Nodes'
import Settings from './pages/Settings'
import Users from './pages/Users'

export default function App() {
  const [session, setSession] = useState<Session | null>(null)

  const refresh = useCallback(async () => {
    setSession(await api<Session>('/api/session'))
  }, [])

  useEffect(() => {
    refresh().catch(() =>
      setSession({ initialized: false, authenticated: false, mode: 'mock' }),
    )
  }, [refresh])

  if (!session) {
    return <Spin size="large" style={{ display: 'block', marginTop: '30vh' }} />
  }

  return (
    <SessionContext.Provider value={{ session, refresh }}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/"
          element={session.authenticated ? <AppLayout /> : <Navigate to="/login" replace />}
        >
          <Route index element={<Dashboard />} />
          <Route path="install" element={<InstallWizard />} />
          <Route path="clients" element={<Clients />} />
          <Route path="maintenance" element={<Maintenance />} />
          <Route path="nodes" element={<Nodes />} />
          <Route path="users" element={<Users />} />
          <Route path="settings" element={<Settings />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </SessionContext.Provider>
  )
}
