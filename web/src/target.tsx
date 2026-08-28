import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { api } from './api/client'
import { useSession } from './session'
import type { NodeGrant, NodePerms, NodeRow } from './types'

// 管理目标:主面板的仪表盘/客户端证书页可作用于宿主机('local')或任一授权子节点(节点 ID)。
// 纯子节点管理员(无宿主机权限、仅有节点授权)登录后自动指向其首个授权节点。

export type ActionKey = 'view' | 'certCreate' | 'certRevoke' | 'kick' | 'maintain'

interface TargetOption {
  value: string
  label: string
}

interface TargetState {
  target: string
  setTarget: (t: string) => void
  options: TargetOption[]
  isLocal: boolean
  targetName: string
  nodeInstalled: boolean
  apiPath: (p: string) => string
  canDo: (k: ActionKey) => boolean
}

const TargetContext = createContext<TargetState | null>(null)

export function useTarget(): TargetState {
  const ctx = useContext(TargetContext)
  if (!ctx) throw new Error('useTarget 必须在 TargetProvider 内使用')
  return ctx
}

const grantHasView = (g?: NodeGrant | null): boolean => !g || g.full || g.perms.view

export function TargetProvider({ children }: { children: ReactNode }) {
  const { session } = useSession()
  const user = session.user
  const isAdmin = !!user?.isAdmin
  const localAllowed = isAdmin || !!user?.perms.view
  const hasGrants = isAdmin || (user?.nodeGrants?.length ?? 0) > 0
  const [nodes, setNodes] = useState<NodeRow[]>([])
  const [target, setTarget] = useState<string>('local')

  useEffect(() => {
    if (!hasGrants) return
    api<{ nodes: NodeRow[] | null }>('/api/nodes')
      .then((r) => setNodes((r.nodes ?? []).filter((n) => grantHasView(n.grant))))
      .catch(() => {})
  }, [hasGrants])

  // 无宿主机查看权限时,自动落到首个可查看的授权节点
  useEffect(() => {
    if (!localAllowed && target === 'local' && nodes.length > 0) setTarget(nodes[0].id)
  }, [localAllowed, target, nodes])

  const options = useMemo(() => {
    const out: TargetOption[] = []
    if (localAllowed) out.push({ value: 'local', label: '宿主机(本机)' })
    nodes.forEach((n) => out.push({ value: n.id, label: `节点:${n.name}` }))
    return out
  }, [localAllowed, nodes])

  const node = nodes.find((n) => n.id === target)
  const isLocal = target === 'local'

  const apiPath = useCallback(
    (p: string) => (isLocal ? `/api/${p}` : `/api/nodes/${target}/proxy/${p}`),
    [isLocal, target],
  )

  const canDo = useCallback(
    (k: ActionKey): boolean => {
      if (isAdmin) return true
      if (isLocal) return !!user?.perms[k]
      const g = node?.grant
      if (!g || g.full) return true
      // 宿主机的「系统维护」对应节点授权中的「升级维护」
      if (k === 'maintain') return g.perms.upgrade
      return !!g.perms[k as keyof NodePerms]
    },
    [isAdmin, isLocal, user, node],
  )

  const value = useMemo<TargetState>(
    () => ({
      target,
      setTarget,
      options,
      isLocal,
      targetName: isLocal ? '宿主机' : node?.name ?? '',
      nodeInstalled: isLocal ? true : !!node?.health.installed,
      apiPath,
      canDo,
    }),
    [target, options, isLocal, node, apiPath, canDo],
  )

  return <TargetContext.Provider value={value}>{children}</TargetContext.Provider>
}
