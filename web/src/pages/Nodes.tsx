import { useCallback, useEffect, useState } from 'react'
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
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import type { BatchResult, JoinCodeResp, NodeRow } from '../types'

const batchPresets: { key: string; label: string; method: string; path: string; danger?: boolean }[] = [
  { key: 'svc-restart', label: '重启 OpenVPN 服务', method: 'POST', path: 'service/openvpn/restart', danger: true },
  { key: 'check', label: '检查更新', method: 'GET', path: 'version?check=1' },
  { key: 'up-openvpn', label: '升级 OpenVPN', method: 'POST', path: 'update/openvpn' },
  { key: 'up-easyrsa', label: '升级 EasyRSA', method: 'POST', path: 'update/easyrsa' },
  { key: 'up-panel', label: '升级面板', method: 'POST', path: 'update/panel' },
  { key: 'panel-restart', label: '重启面板进程', method: 'POST', path: 'panel/restart', danger: true },
]

export default function Nodes() {
  const { message, modal } = AntApp.useApp()
  const [list, setList] = useState<NodeRow[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [addOpen, setAddOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState<NodeRow | null>(null)
  const [joinInfo, setJoinInfo] = useState<JoinCodeResp | null>(null)
  const [batching, setBatching] = useState(false)
  const [addForm] = Form.useForm()
  const [editForm] = Form.useForm()

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const res = await api<{ nodes: NodeRow[] | null }>('/api/nodes')
      setList(res.nodes ?? [])
    } catch (e) {
      if (!silent) message.error(e instanceof ApiError ? e.message : '加载节点失败')
    } finally {
      if (!silent) setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void load()
    const t = setInterval(() => void load(true), 30000)
    return () => clearInterval(t)
  }, [load])

  // 快捷绑定弹窗打开期间轮询,注册成功即自动出现在列表
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
      message.success('子节点已接管')
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
      message.success('已移除(子节点面板本身不受影响)')
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
            title: `批量结果: ${ok}/${res.results.length} 成功`,
            width: 640,
            content: (
              <Table<BatchResult>
                rowKey="id"
                size="small"
                pagination={false}
                dataSource={res.results}
                columns={[
                  { title: '节点', dataIndex: 'name' },
                  {
                    title: '结果',
                    key: 'r',
                    width: 90,
                    render: (_, r) =>
                      r.ok ? <Tag color="green">{r.status}</Tag> : <Tag color="red">{r.status || '不可达'}</Tag>,
                  },
                  {
                    title: '响应',
                    dataIndex: 'body',
                    ellipsis: true,
                    render: (v: string) => (
                      <Typography.Text style={{ fontSize: 12 }} copyable={{ text: v }}>
                        {v.slice(0, 120)}
                      </Typography.Text>
                    ),
                  },
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
          <Typography.Text strong>{v}</Typography.Text>
          <Typography.Link href={n.url} target="_blank" style={{ fontSize: 12 }}>
            {n.url}
          </Typography.Link>
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'health',
      width: 110,
      render: (_, n) =>
        n.health.reachable ? (
          <Badge status="success" text="在线" />
        ) : (
          <Tooltip title={n.health.error}>
            <Badge status="error" text="不可达" />
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
      width: 130,
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
    {
      title: '在线客户端',
      key: 'online',
      width: 100,
      render: (_, n) => (n.health.reachable ? n.health.online : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 150,
      render: (_, n) => (
        <Space>
          <Button
            size="small"
            icon={<EditOutlined />}
            onClick={() => {
              editForm.setFieldsValue({ name: n.name, url: n.url, token: '', insecureTLS: n.insecureTLS })
              setEditing(n)
            }}
          />
          <Popconfirm
            title={`移除节点 ${n.name}?`}
            description="仅从主节点解绑,子节点面板与其上的 OpenVPN 不受影响。"
            okButtonProps={{ danger: true }}
            onConfirm={() => remove(n.id)}
          >
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
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
            主节点直连各子节点面板 API,健康状态 30 秒自动刷新
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
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
            添加子节点
          </Button>
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
        locale={{ emptyText: '还没有子节点,点右上角「添加子节点」开始' }}
      />

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
                    生成一次性绑定码后,在<b>子节点服务器</b>上执行下方命令:自动安装面板(如未装)、
                    生成接入令牌并注册到本主节点。要求:子节点面板端口可被主节点访问。
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
                        绑定码 {joinInfo.code} · 到期 {new Date(joinInfo.expiresAt).toLocaleTimeString()} ·
                        注册成功后列表会自动出现该节点
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
                    <Input placeholder="如 hk-01" />
                  </Form.Item>
                  <Form.Item name="url" label="节点面板地址" rules={[{ required: true, message: '请输入地址' }]}>
                    <Input placeholder="http://1.2.3.4:8686" />
                  </Form.Item>
                  <Form.Item
                    name="token"
                    label="接入令牌"
                    tooltip="在子节点执行: sudo ovpn-web -config /etc/openvpntools/config.yaml -gen-node-token && sudo systemctl restart ovpn-web"
                    rules={[{ required: true, message: '请输入令牌' }]}
                  >
                    <Input.Password placeholder="64 位十六进制" />
                  </Form.Item>
                  <Form.Item
                    name="insecureTLS"
                    label="跳过 TLS 证书校验(仅子节点用自签 HTTPS 时,有中间人风险)"
                    valuePropName="checked"
                  >
                    <Switch />
                  </Form.Item>
                  <Button type="primary" loading={saving} onClick={manualAdd}>
                    验证并接管
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
          <Form.Item name="token" label="接入令牌(留空不改)">
            <Input.Password placeholder="留空则保持不变" />
          </Form.Item>
          <Form.Item name="insecureTLS" label="跳过 TLS 证书校验" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
