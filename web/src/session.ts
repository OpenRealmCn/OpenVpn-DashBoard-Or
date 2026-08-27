import { createContext, useContext } from 'react'
import type { Perms, Session } from './types'

export interface SessionState {
  session: Session
  refresh: () => Promise<void>
}

export const SessionContext = createContext<SessionState | null>(null)

export function useSession(): SessionState {
  const ctx = useContext(SessionContext)
  if (!ctx) throw new Error('useSession 必须在 SessionContext 内使用')
  return ctx
}

// hasPerm 管理员恒真;子用户按权限位。
export function hasPerm(s: Session, key: keyof Perms): boolean {
  if (!s.user) return false
  return s.user.isAdmin || s.user.perms[key]
}
