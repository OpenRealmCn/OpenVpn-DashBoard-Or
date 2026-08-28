import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  App as AntApp,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Result,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  DownloadOutlined,
  PlusOutlined,
  QrcodeOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons'
import { QRCodeSVG } from 'qrcode.react'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import OnlineClients from '../components/OnlineClients'
import { hasPerm, useSession } from '../session'
import type { ClientCert, ShareResp } from '../types'

const statusTag = (c: ClientCert) => {
  switch (c.status) {
    case 'V':
      return <Tag color="green">有效</Tag>
    case 'R':
      return <Tag color="red">已吊销</Tag>
    default:
      return <Tag color="orange">已过期</Tag>
  }
}

interface CreateForm {
  name: string
  withPass: boolean
  passphrase?: string
  expireDays: number
}

export default function Clients() {
  const { message } = AntApp.useApp()
  const { session, refresh } = useSession()
  const me = session.user
  const canCreate = hasPerm(session, 'certCreate')
  const canRevoke = hasPerm(session, 'certRevoke')
  const canKick = hasPerm(session, 'kick')
  const ownCert = (c: ClientCert) => !!me && (me.isAdmin || c.owner === me.username)
  const [list, setList] = useState<ClientCert[]>([])
  const [loading, setLoading] = useState(false)
  const [notInstalled, setNotInstalled] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [share, setShare] = useState<{ cn: string; data: ShareResp } | null>(null)
  const [form] = Form.useForm<CreateForm>()
  const withPass = Form.useWatch('withPass', form)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api<{ clients: ClientCert[] | null }>('/api/clients')
      setList(res.clients ?? [])
      setNotInstalled(false)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setNotInstalled(true)
      else message.error(e instanceof ApiError ? e.message : '加载客户端列表失败')
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void load()
  }, [load])

  const create = async (v: CreateForm) => {
    setCreating(true)
    try {
      await api('/api/clients', {
        method: 'POST',
        body: JSON.stringify({
          name: v.name,
          passphrase: v.withPass ? v.passphrase : '',
          expireDays: v.expireDays,
        }),
      })
      message.success(`客户端 ${v.name} 已创建`)
      setCreateOpen(false)
      form.resetFields()
      await load()
      await refresh() // 更新配额计数
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  const revoke = async (cn: string) => {
    try {
      await api(`/api/clients/${encodeURIComponent(cn)}/revoke`, { method: 'POST' })
      message.success(`已吊销 ${cn},新连接立即被拒绝`)
      await load()
      await refresh() // 更新配额计数
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '吊销失败')
    }
  }

  const openShare = async (cn: string) => {
    try {
      const data = await api<ShareResp>(`/api/clients/${encodeURIComponent(cn)}/share`, {
        method: 'POST',
      })
      setShare({ cn, data })
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '生成分享链接失败')
    }
  }

  const columns: ColumnsType<ClientCert> = [
    {
      title: '客户端',
      dataIndex: 'cn',
      render: (v: string) => <Typography.Text strong>{v}</Typography.Text>,
    },
    { title: '状态', key: 'status', width: 100, render: (_, c) => statusTag(c) },
    {
      title: '创建者',
      dataIndex: 'owner',
      width: 110,
      render: (v: string) => v || '-',
    },
    {
      title: '证书到期',
      dataIndex: 'expiry',
      width: 180,
      render: (v: string) => new Date(v).toLocaleString(),
    },
    {
      title: '吊销时间',
      dataIndex: 'revokedAt',
      width: 180,
      render: (v?: string) => (v ? new Date(v).toLocaleString() : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 300,
      render: (_, c) =>
        c.status === 'V' ? (
          <Space>
            {canCreate && ownCert(c) && (
              <>
                <Button
                  size="small"
                  icon={<DownloadOutlined />}
                  href={`/api/clients/${encodeURIComponent(c.cn)}/config`}
                >
                  下载
                </Button>
                <Button size="small" icon={<QrcodeOutlined />} onClick={() => openShare(c.cn)}>
                  二维码
                </Button>
              </>
            )}
            {canRevoke && ownCert(c) && (
              <Popconfirm
                title={`吊销 ${c.cn}?`}
                description="吊销后该证书立即无法建立新连接,且不可恢复。"
                okButtonProps={{ danger: true }}
                onConfirm={() => revoke(c.cn)}
              >
                <Button size="small" danger icon={<StopOutlined />}>
                  吊销
                </Button>
              </Popconfirm>
            )}
          </Space>
        ) : null,
    },
  ]

  if (notInstalled) {
    return (
      <Card>
        <Result
          status="warning"
          title="OpenVPN 尚未安装"
          subTitle="请先通过安装向导完成部署,再管理客户端证书"
          extra={
            <Link to="/install">
              <Button type="primary">前往安装向导</Button>
            </Link>
          }
        />
      </Card>
    )
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <OnlineClients canKick={canKick} />
      <Card
        title={
          <Space>
            客户端证书
            {me && !me.isAdmin && me.certLimit > 0 && (
              <Tag color={me.certsUsed >= me.certLimit ? 'red' : 'blue'}>
                配额 {me.certsUsed}/{me.certLimit}
              </Tag>
            )}
          </Space>
        }
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
              刷新
            </Button>
            {canCreate && (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                新建客户端
              </Button>
            )}
          </Space>
        }
      >
        <Table<ClientCert>
          rowKey="serial"
          columns={columns}
          dataSource={list}
          loading={loading}
          size="middle"
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
        />
      </Card>

      <Modal
        title="新建客户端证书"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={creating}
        okText="创建"
        destroyOnClose
      >
        <Form<CreateForm>
          form={form}
          layout="vertical"
          initialValues={{ withPass: false, expireDays: 825 }}
          onFinish={create}
        >
          <Form.Item
            name="name"
            label="客户端名称"
            rules={[
              { required: true, message: '请输入名称' },
              {
                pattern: /^[A-Za-z0-9_-]{1,64}$/,
                message: '只允许字母、数字、下划线和连字符(1-64 位)',
              },
            ]}
          >
            <Input placeholder="例如 iphone-01" autoFocus />
          </Form.Item>
          <Form.Item
            name="expireDays"
            label="证书有效期(天)"
            rules={[{ required: true, message: '请输入有效期' }]}
          >
            <InputNumber min={1} max={3650} style={{ width: 180 }} />
          </Form.Item>
          <Form.Item
            name="withPass"
            label="私钥加密(导入时需输入密码)"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          {withPass && (
            <Form.Item
              name="passphrase"
              label="证书密码"
              rules={[
                { required: true, message: '请输入证书密码' },
                { min: 4, message: '至少 4 位' },
              ]}
            >
              <Input.Password placeholder="导入 .ovpn 时需要输入" />
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title={share ? `扫码下载:${share.cn}.ovpn` : ''}
        open={!!share}
        onCancel={() => setShare(null)}
        footer={
          share && (
            <Button
              onClick={() => {
                navigator.clipboard.writeText(share.data.url)
                message.success('链接已复制')
              }}
            >
              复制链接
            </Button>
          )
        }
      >
        {share && (
          <Space direction="vertical" align="center" style={{ width: '100%' }}>
            <div style={{ background: '#fff', padding: 16, borderRadius: 8 }}>
              <QRCodeSVG value={share.data.url} size={220} />
            </div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              一次性链接,下载一次即失效;{Math.round(share.data.ttlSec / 60)} 分钟后过期
            </Typography.Text>
            <Typography.Text code copyable style={{ fontSize: 12, wordBreak: 'break-all' }}>
              {share.data.url}
            </Typography.Text>
          </Space>
        )}
      </Modal>
    </Space>
  )
}
