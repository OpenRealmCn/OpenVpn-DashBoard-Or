import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  App as AntApp,
  Badge,
  Button,
  Card,
  Dropdown,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  ApiOutlined,
  ControlOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  RocketOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import { useSession } from '../session'
import NodeDrawer, { hostOf, RemoteInstallModal } from '../components/NodeDrawer'
import type { BatchResult, JoinCodeResp, NodeRow } from '../types'

const batchPresets: { key: string; label: string; method: string; path: string; danger?: boolean }[] = [
  { key: 'check', label: '检查更新', method: 'GET', path: 'version?check=1' },
  { key: 'svc-restart', label: '重启 OpenVPN 服务', method: 'POST', path: 'service/openvpn/restart', danger: true },
  { key: 'up-openvpn', label: '升级 OpenVPN', method: 'POST', path: 'update/openvpn' },
  { key: 'up-easyrsa', label: '升级 EasyRSA', method: 'POST', path: 'update/easyrsa' },
  { key: 'up-panel', label: '升级面板', method: 'POST', path: 'update/panel' },
  { key: 'panel-restart', label: '重启面板进程', method: 'POST', path: 'panel/restart', danger: true },
]

// 将子节点返回的 JSON 解析为可读结果,不再直接展示原始报文
function formatBatchBody(r: BatchResult): ReactNode {
  if (!r.body) return r.ok ? '完成' : '节点无响应'
  try {
    const d = JSON.parse(r.body)
    if (d.error) return <Typography.Text type="danger">{String(d.error)}</Typography.Text>
    if (Array.isArray(d.logs) && d.logs.length) {
      return (
        <div style={{ fontSize: 12, lineHeight: 1.8 }}>
          {d.logs.map((l: string, i: number) => (
            <div key={i}>{l}</div>
          ))}
          {d.needRestart && <Tag color="orange">需重启面板进程后生效</Tag>}
        </div>
      )
    }
    if (typeof d.panel === 'string') {
      const seg = [
        `面板 v${d.panel}` + (d.panelLatest && d.panelLatest !== d.panel ? `(可升级 v${d.panelLatest})` : '(已最新)'),
        `OpenVPN ${d.openvpn || '未安装'}` + (d.openvpnUpgrade ? `(可升级 ${d.openvpnUpgrade})` : ''),
        `EasyRSA ${d.easyrsa || '未安装'}` +
          (d.easyrsaLatest && d.easyrsa && d.easyrsaLatest !== d.easyrsa ? `(可升级 ${d.easyrsaLatest})` : ''),
      ]
      return seg.join(' · ')
    }
    if (d.ok) return '操作成功'
    return r.body.slice(0, 200)
  } catch {
    return r.body.slice(0, 200)
  }
}

export default function Nodes() {
  const { message, modal } = AntApp.useApp()
  const { session } = useSession()
  const isAdmin = !!session.user?.isAdmin
  const [list, setList] = useState<NodeRow[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [addOpen, setAddOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState<NodeRow | null>(null)
  const [detail, setDetail] = useState<NodeRow | null>(null)
  const [installOpen, setInstallOpen] = useState(false)
  const [joinInfo, setJoinInfo] = useState<JoinCodeResp | null>(null)
  const [batching, setBatching] = useState(false)
  const [addForm] = Form.useForm()
  const [editForm] = Form.useForm()

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const res = await api<{ nodes: NodeRow[] | null }>('/api/nodes')
        setList(res.nodes ?? [])
      } catch (e) {
        if (!silent) message.error(e instanceof ApiError ? e.message : '加载节点失败')
      } finally {
        if (!silent) setLoading(false)
      }
    },
    [message],
  )

  useEffect(() => {
    void load()
    const t = setInterval(() => void load(true), 30000)
    return () => clearInterval(t)
  }, [load])

  // 抽屉打开时保持详情数据与列表同步
  useEffect(() => {
    if (!detail) return
    const next = list.find((n) => n.id === detail.id)
    if (next && next !== detail) setDetail(next)
  }, [list, detail])

  // 绑定窗口打开期间轮询,子节点注册成功后自动出现在列表
  useEffect(() => {
    if (!addOpen || !joinInfo) return
    const t = setInterval(() => void load(true), 5000)
    return () => clearInterval(t)
  }, [addOpen, joinInfo, load])

  const genJoinCode = async () => {
    try {
      setJoinInfo(await api<JoinCodeResp>('/api/nodes/joincode', { method: 'POST' }))
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '生成绑定码失败')
    }
  }

  const manualAdd = async () => {
    const v = await addForm.validateFields()
    setSaving(true)
    try {
      await api('/api/nodes', { method: 'POST', body: JSON.stringify(v) })
      message.success('子节点已接入')
      setAddOpen(false)
      addForm.resetFields()
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '添加失败')
    } finally {
      setSaving(false)
    }
  }

  const saveEdit = async () => {
    const v = await editForm.validateFields()
    setSaving(true)
    try {
      await api(`/api/nodes/${editing!.id}`, {
        method: 'PUT',
        body: JSON.stringify({ ...v, token: v.token || undefined }),
      })
      message.success('已保存')
      setEditing(null)
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: string) => {
    try {
      await api(`/api/nodes/${id}`, { method: 'DELETE' })
      message.success('已解除接入,子节点面板及其服务不受影响')
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '删除失败')
    }
  }

  const runBatch = (preset: (typeof batchPresets)[number]) => {
    modal.confirm({
      title: `对选中的 ${selected.length} 个节点执行「${preset.label}」?`,
      okButtonProps: { danger: preset.danger },
      onOk: async () => {
        setBatching(true)
        try {
          const res = await api<{ results: BatchResult[] }>('/api/nodes/batch', {
            method: 'POST',
            body: JSON.stringify({ ids: selected, method: preset.method, path: preset.path }),
          })
          const ok = res.results.filter((r) => r.ok).length
          modal.info({
            title: `${preset.label}:${ok}/${res.results.length} 个节点成功`,
            width: 720,
            content: (
              <Table<BatchResult>
                rowKey="id"
                size="small"
                pagination={false}
                dataSource={res.results}
                columns={[
                  { title: '节点', dataIndex: 'name', width: 140, render: (v: string, r) => v || r.id },
                  {
                    title: '结果',
                    key: 'r',
                    width: 80,
                    render: (_, r) => (r.ok ? <Tag color="green">成功</Tag> : <Tag color="red">失败</Tag>),
                  },
                  { title: '详情', key: 'body', render: (_, r) => formatBatchBody(r) },
                ]}
              />
            ),
          })
          await load(true)
        } catch (e) {
          message.error(e instanceof ApiError ? e.message : '批量下发失败')
        } finally {
          setBatching(false)
        }
      },
    })
  }

  const columns: ColumnsType<NodeRow> = [
    {
      title: '节点',
      dataIndex: 'name',
      render: (v: string, n) => (
        <Space direction="vertical" size={0}>
          <Typography.Link strong onClick={() => setDetail(n)}>
            {v}
          </Typography.Link>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {hostOf(n.url)}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'health',
      width: 100,
      render: (_, n) =>
        n.health.reachable ? (
          <Badge status="success" text="在线" />
        ) : (
          <Tooltip title={n.health.error}>
            <Badge status="error" text="离线" />
          </Tooltip>
        ),
    },
    {
      title: '面板版本',
      key: 'ver',
      width: 100,
      render: (_, n) => (n.health.version ? `v${n.health.version}` : '-'),
    },
    {
      title: 'OpenVPN',
      key: 'ovpn',
      width: 110,
      render: (_, n) =>
        !n.health.reachable ? (
          '-'
        ) : !n.health.installed ? (
          <Tag>未安装</Tag>
        ) : n.health.serviceActive ? (
          <Tag color="green">运行中</Tag>
        ) : (
          <Tag color="red">已停止</Tag>
        ),
    },
    { title: '在线客户端', key: 'online', width: 100, render: (_, n) => (n.health.reachable ? n.health.online : '-') },
    {
      title: '操作',
      key: 'actions',
      width: isAdmin ? 210 : 100,
      render: (_, n) => (
        <Space>
          <Button size="small" icon={<ControlOutlined />} onClick={() => setDetail(n)}>
            管理
          </Button>
          {isAdmin && (
            <>
              <Button
                size="small"
                icon={<EditOutlined />}
                onClick={() => {
                  editForm.setFieldsValue({ name: n.name, url: n.url, token: '', insecureTLS: n.insecureTLS })
                  setEditing(n)
                }}
              />
              <Popconfirm
                title={`解除节点 ${n.name} 的接入?`}
                description="仅解除与本面板的关联,子节点及其 OpenVPN 服务不受影响。"
                okButtonProps={{ danger: true }}
                onConfirm={() => remove(n.id)}
              >
                <Button size="small" danger icon={<DeleteOutlined />} />
              </Popconfirm>
            </>
          )}
        </Space>
      ),
    },
  ]

  return (
    <Card
      title={
        <Space>
          <ApiOutlined />
          节点管理
          <Typography.Text type="secondary" style={{ fontSize: 12, fontWeight: 'normal' }}>
            状态每 30 秒自动刷新
          </Typography.Text>
        </Space>
      }
      extra={
        <Space>
          <Dropdown
            disabled={selected.length === 0}
            menu={{
              items: batchPresets.map((p) => ({ key: p.key, label: p.label, danger: p.danger })),
              onClick: ({ key }) => {
                const preset = batchPresets.find((p) => p.key === key)
                if (preset) runBatch(preset)
              },
            }}
          >
            <Button icon={<ThunderboltOutlined />} loading={batching}>
              批量操作({selected.length})
            </Button>
          </Dropdown>
          <Button icon={<RocketOutlined />} onClick={() => setInstallOpen(true)}>
            远程安装
          </Button>
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>
            刷新
          </Button>
          {isAdmin && (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
              添加子节点
            </Button>
          )}
        </Space>
      }
    >
      <Table<NodeRow>
        rowKey="id"
        columns={columns}
        dataSource={list}
        loading={loading}
        size="middle"
        pagination={false}
        rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys as string[]) }}
        locale={{ emptyText: isAdmin ? '尚未接入子节点,点击右上角「添加子节点」开始' : '暂无分配给您的节点' }}
      />

      <NodeDrawer node={detail} onClose={() => setDetail(null)} refreshList={() => void load(true)} />
      <RemoteInstallModal open={installOpen} nodes={list} onClose={() => setInstallOpen(false)} onDone={() => void load(true)} />

      <Modal
        title="添加子节点"
        open={addOpen}
        onCancel={() => {
          setAddOpen(false)
          setJoinInfo(null)
        }}
        footer={null}
        width={640}
        destroyOnClose
      >
        <Tabs
          items={[
            {
              key: 'quick',
              label: '快捷绑定(推荐)',
              children: (
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                    生成绑定命令后,在目标服务器执行即可自动完成面板安装、令牌配置与注册。要求目标服务器的面板端口可被本面板访问。
                  </Typography.Paragraph>
                  <Button type="primary" onClick={genJoinCode}>
                    生成绑定命令(15 分钟内有效,单次)
                  </Button>
                  {joinInfo && (
                    <>
                      <Typography.Paragraph
                        code
                        copyable={{ text: joinInfo.command }}
                        className="term-box"
                        style={{ wordBreak: 'break-all', color: '#95de64' }}
                      >
                        {joinInfo.command}
                      </Typography.Paragraph>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        绑定码 {joinInfo.code} · 有效期至 {new Date(joinInfo.expiresAt).toLocaleTimeString()} ·
                        注册完成后节点将自动出现在列表中
                      </Typography.Text>
                    </>
                  )}
                </Space>
              ),
            },
            {
              key: 'manual',
              label: '手动添加',
              children: (
                <Form form={addForm} layout="vertical" initialValues={{ insecureTLS: false }}>
                  <Form.Item name="name" label="节点名称" rules={[{ required: true, message: '请输入名称' }]}>
                    <Input placeholder="例如 hk-01" />
                  </Form.Item>
                  <Form.Item name="url" label="节点面板地址" rules={[{ required: true, message: '请输入地址' }]}>
                    <Input placeholder="http://203.0.113.10:8686" />
                  </Form.Item>
                  <Form.Item
                    name="token"
                    label="接入令牌"
                    tooltip="在子节点执行 sudo ovpn-web -config /etc/openvpntools/config.yaml -gen-node-token 生成,并重启其面板服务"
                    rules={[{ required: true, message: '请输入令牌' }]}
                  >
                    <Input.Password placeholder="64 位十六进制" />
                  </Form.Item>
                  <Form.Item
                    name="insecureTLS"
                    label="跳过 TLS 证书校验(仅适用于子节点使用自签名 HTTPS 的场景,存在中间人风险)"
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Button type="primary" loading={saving} onClick={manualAdd}>
                    验证并接入
                  </Button>
                </Form>
              ),
            },
          ]}
        />
      </Modal>

      <Modal
        title={`编辑节点 ${editing?.name ?? ''}`}
        open={!!editing}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText="保存"
        destroyOnClose
      >
        <Form form={editForm} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="url" label="地址" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="token" label="接入令牌(留空保持不变)">
            <Input.Password placeholder="留空则不修改" />
          </Form.Item>
          <Form.Item name="insecureTLS" label="跳过 TLS 证书校验" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
