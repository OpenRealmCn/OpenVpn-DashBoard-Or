import { useCallback, useEffect, useState } from 'react'
import {
  App as AntApp,
  Badge,
  Button,
  Card,
  Col,
  List,
  Popconfirm,
  Row,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  CloudServerOutlined,
  DesktopOutlined,
  ExperimentOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import { hasPerm, useSession } from '../session'
import StatCard from '../components/StatCard'
import type { Occupant, PortInfo, StatusResp } from '../types'

const portColumns: ColumnsType<PortInfo> = [
  {
    title: '协议',
    dataIndex: 'proto',
    width: 80,
    render: (v: string) => <Tag color={v === 'udp' ? 'blue' : 'green'}>{v.toUpperCase()}</Tag>,
  },
  { title: '监听地址', dataIndex: 'addr' },
  { title: '端口', dataIndex: 'port', width: 90 },
  { title: '进程', dataIndex: 'comm' },
  { title: 'systemd 单元', dataIndex: 'unit', render: (v: string) => v || '-' },
  { title: 'PID', dataIndex: 'pid', width: 90, render: (v: number) => (v > 0 ? v : '-') },
]

function occupantTag(o: Occupant) {
  switch (o.class) {
    case 'resolved':
      return <Tag color="blue">systemd-resolved</Tag>
    case 'known-dns':
      return <Tag color="gold">已知 DNS 服务</Tag>
    default:
      return (
        <Tooltip title="为安全起见,本工具不会自动停止未知进程,请自行确认后处理">
          <Tag color="red">未知进程(不会自动停止)</Tag>
        </Tooltip>
      )
  }
}

export default function Dashboard() {
  const [status, setStatus] = useState<StatusResp | null>(null)
  const [loading, setLoading] = useState(false)
  const [acting, setActing] = useState(false)
  const { message } = AntApp.useApp()
  const { session } = useSession()
  const canMaintain = hasPerm(session, 'maintain')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setStatus(await api<StatusResp>('/api/status'))
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '获取状态失败')
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void load()
  }, [load])

  const act = async (path: string, okMsg: string) => {
    setActing(true)
    try {
      await api(path, { method: 'POST' })
      message.success(okMsg)
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setActing(false)
    }
  }

  const svc = status?.openvpn.service
  const dns = status?.dns
  const ipfwd = status?.ipForward
  const svcTone = !svc || !svc.exists ? '#8f8f8f' : svc.active ? '#16a34a' : '#e5484d'

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }} className="stagger">
      <Row gutter={[16, 16]} className="stagger">
        <Col xs={24} sm={12} lg={6}>
          <StatCard title="OpenVPN 服务" icon={<CloudServerOutlined />} tone={svcTone}>
            <Space direction="vertical" size={4}>
              {!svc || !svc.exists ? (
                <Badge status="default" text="未安装" />
              ) : svc.active ? (
                <Badge status="processing" color="#16a34a" text="运行中" />
              ) : (
                <Badge status="error" text="已停止" />
              )}
              <span>{svc?.enabled ? <Tag color="green">开机自启</Tag> : <Tag>未设自启</Tag>}</span>
            </Space>
          </StatCard>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatCard title="IPv4 转发" icon={<ThunderboltOutlined />} tone={ipfwd?.runtime ? '#16a34a' : '#f59e0b'}>
            <Space direction="vertical" size={6}>
              <Space wrap size={6}>
                {ipfwd?.runtime ? (
                  <Badge status="success" text="已开启" />
                ) : (
                  <Badge status="warning" text="未开启" />
                )}
                {ipfwd?.persisted ? <Tag color="green">已持久化</Tag> : <Tag color="orange">未持久化</Tag>}
              </Space>
              {canMaintain && (!ipfwd?.runtime || !ipfwd?.persisted) && (
                <Button
                  size="small"
                  type="primary"
                  ghost
                  loading={acting}
                  onClick={() => act('/api/system/ipforward', 'IPv4 转发已开启并持久化到 sysctl.d')}
                >
                  一键开启并持久化
                </Button>
              )}
            </Space>
          </StatCard>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatCard title="操作系统" icon={<DesktopOutlined />} tone="#0070f3">
            <Typography.Text strong>{status?.os.pretty || '-'}</Typography.Text>
          </StatCard>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <StatCard
            title="运行模式"
            icon={<ExperimentOutlined />}
            tone={status?.mode === 'linux' ? '#0070f3' : '#f59e0b'}
          >
            {status?.mode === 'linux' ? (
              <Tag color="blue">Linux 生产模式</Tag>
            ) : (
              <Tag color="orange">mock 演示模式</Tag>
            )}
          </StatCard>
        </Col>
      </Row>

      <Card
        size="small"
        title="DNS Stub / UDP 53"
        extra={
          <Space>
            {canMaintain && dns?.resolvedExists && !dns.dropInPresent && (
              <Popconfirm
                title="关闭 systemd-resolved 的 DNS Stub?"
                description="通过 drop-in 配置关闭(不改写主配置),原状会保存为恢复点,可随时恢复。"
                onConfirm={() => act('/api/dns/stub/disable', 'DNS Stub 已关闭(drop-in 方式)')}
              >
                <Button size="small" danger loading={acting}>
                  关闭 DNS Stub
                </Button>
              </Popconfirm>
            )}
            {canMaintain && dns?.backupPresent && (
              <Button size="small" loading={acting} onClick={() => act('/api/dns/stub/restore', '已恢复 DNS 原状')}>
                恢复原状
              </Button>
            )}
          </Space>
        }
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space wrap>
            {dns?.resolvedExists ? (
              <Tag color={dns.resolvedActive ? 'blue' : 'default'}>
                systemd-resolved {dns.resolvedActive ? '运行中' : '未运行'}
              </Tag>
            ) : (
              <Tag>无 systemd-resolved</Tag>
            )}
            {dns?.dropInPresent && <Tag color="purple">Stub 已由 drop-in 关闭</Tag>}
            {dns?.resolvConfIsLink && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                /etc/resolv.conf → {dns.resolvConfTarget}
              </Typography.Text>
            )}
          </Space>
          {dns?.port53.free ? (
            <Badge status="success" text="UDP 53 空闲" />
          ) : (
            <List
              size="small"
              dataSource={dns?.port53.occupants ?? []}
              renderItem={(o) => (
                <List.Item>
                  <Space wrap>
                    {occupantTag(o)}
                    <Typography.Text code>
                      {o.comm || '?'} (PID {o.pid || '?'})
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      {o.addr}:{o.port} {o.unit && `· ${o.unit}`}
                    </Typography.Text>
                  </Space>
                </List.Item>
              )}
            />
          )}
        </Space>
      </Card>

      <Card
        title="监听端口"
        size="small"
        extra={
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading} size="small">
            刷新
          </Button>
        }
      >
        <Table<PortInfo>
          rowKey={(p) => `${p.proto}-${p.addr}-${p.port}-${p.pid}`}
          columns={portColumns}
          dataSource={status?.ports ?? []}
          loading={loading}
          size="small"
          pagination={false}
        />
      </Card>
    </Space>
  )
}
