import { useCallback, useEffect, useState } from 'react'
import { App as AntApp, Badge, Button, Card, Popconfirm, Space, Table, Typography } from 'antd'
import { DisconnectOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import { useTarget } from '../target'
import type { OnlineClient } from '../types'

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = n
  let i = -1
  do {
    v /= 1024
    i++
  } while (v >= 1024 && i < units.length - 1)
  return `${v.toFixed(1)} ${units[i]}`
}

export default function OnlineClients({ canKick = true }: { canKick?: boolean }) {
  const { message } = AntApp.useApp()
  const { apiPath } = useTarget()
  const [list, setList] = useState<OnlineClient[]>([])
  const [loading, setLoading] = useState(false)

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const res = await api<{ online: OnlineClient[] | null }>(apiPath('online'))
        setList(res.online ?? [])
      } catch {
        // 未安装或服务未运行时静默,证书表会给出引导
      } finally {
        if (!silent) setLoading(false)
      }
    },
    [apiPath],
  )

  useEffect(() => {
    void load()
    const timer = setInterval(() => void load(true), 10000)
    return () => clearInterval(timer)
  }, [load])

  const kick = async (cn: string) => {
    try {
      await api(apiPath(`online/${encodeURIComponent(cn)}/kick`), { method: 'POST' })
      message.success(`已断开 ${cn} 的当前会话;如需永久禁用,请吊销其证书`)
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '断开失败')
    }
  }

  const columns: ColumnsType<OnlineClient> = [
    {
      title: '客户端',
      dataIndex: 'cn',
      render: (v: string) => (
        <Space>
          <Badge status="success" />
          <Typography.Text strong>{v}</Typography.Text>
        </Space>
      ),
    },
    { title: '来源地址', dataIndex: 'realAddr' },
    { title: 'VPN 地址', dataIndex: 'virtualAddr', render: (v: string) => v || '-' },
    { title: '下行', dataIndex: 'bytesRecv', width: 110, render: formatBytes },
    { title: '上行', dataIndex: 'bytesSent', width: 110, render: formatBytes },
    {
      title: '连接时间',
      dataIndex: 'since',
      width: 180,
      render: (v: string) => (v ? new Date(v).toLocaleString() : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 110,
      render: (_, c) =>
        canKick ? (
          <Popconfirm
            title={`断开 ${c.cn} 的连接?`}
            description="仅断开当前会话,客户端可能自动重连;如需永久禁用,请吊销其证书。"
            onConfirm={() => kick(c.cn)}
          >
            <Button size="small" danger icon={<DisconnectOutlined />}>
              断开
            </Button>
          </Popconfirm>
        ) : null,
    },
  ]

  return (
    <Card
      size="small"
      title={
        <Space>
          在线客户端
          <Typography.Text type="secondary" style={{ fontSize: 12, fontWeight: 'normal' }}>
            列表每 10 秒自动刷新;流量数据由服务端约每 60 秒更新
          </Typography.Text>
        </Space>
      }
      extra={
        <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={() => load()}>
          刷新
        </Button>
      }
    >
      <Table<OnlineClient>
        rowKey={(c) => c.cn + c.realAddr}
        columns={columns}
        dataSource={list}
        size="small"
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '当前没有客户端在线' }}
      />
    </Card>
  )
}
