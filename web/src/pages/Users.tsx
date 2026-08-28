import { useCallback, useEffect, useState } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  Checkbox,
  Divider,
  Dropdown,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import type { NodeGrant, NodePerms, Perms, UserRow } from '../types'

const permOptions: { key: keyof Perms; label: string }[] = [
  { key: 'view', label: '查看' },
  { key: 'certCreate', label: '创建证书' },
  { key: 'certRevoke', label: '吊销证书' },
  { key: 'install', label: '安装' },
  { key: 'kick', label: '断开客户端' },
  { key: 'maintain', label: '系统维护' },
]

const permLabel: Record<string, string> = Object.fromEntries(
  permOptions.map((p) => [p.key, p.label]),
)

interface FormValues {
  username: string
  password?: string
  permKeys: (keyof Perms)[]
  certLimit: number
  disabled?: boolean
}

const nodePermOptions: { key: keyof NodePerms; label: string }[] = [
  { key: 'view', label: '查看' },
  { key: 'certCreate', label: '创建证书' },
  { key: 'certRevoke', label: '吊销证书' },
  { key: 'install', label: '安装部署' },
  { key: 'rollback', label: '失败回滚' },
  { key: 'service', label: '服务控制' },
  { key: 'kick', label: '断开客户端' },
  { key: 'upgrade', label: '升级维护' },
  { key: 'panelRestart', label: '重启面板' },
]

const emptyNodePerms = (): NodePerms => ({
  view: false, certCreate: false, certRevoke: false, install: false, rollback: false,
  service: false, kick: false, upgrade: false, panelRestart: false,
})

// 权限模板:「完整管理」即整体管理模板,其余为常用预设,套用后仍可微调
const grantTemplates: { key: string; label: string; full: boolean; perms: Partial<NodePerms> }[] = [
  { key: 'full', label: '完整管理', full: true, perms: {} },
  { key: 'readonly', label: '只读', full: false, perms: { view: true } },
  { key: 'cert', label: '证书管理员', full: false, perms: { view: true, certCreate: true, certRevoke: true } },
  {
    key: 'ops',
    label: '运维',
    full: false,
    perms: { view: true, service: true, kick: true, rollback: true, upgrade: true, panelRestart: true },
  },
]

const permKeysOf = (p: NodePerms): (keyof NodePerms)[] =>
  nodePermOptions.filter((o) => p[o.key]).map((o) => o.key)

const toNodePerms = (keys: (keyof NodePerms)[]): NodePerms => {
  const p = emptyNodePerms()
  keys.forEach((k) => {
    p[k] = true
  })
  return p
}

function toPerms(keys: (keyof Perms)[]): Perms {
  const p: Perms = {
    view: false, certCreate: false, certRevoke: false,
    install: false, kick: false, maintain: false,
  }
  keys.forEach((k) => { p[k] = true })
  return p
}

export default function Users() {
  const { message } = AntApp.useApp()
  const [list, setList] = useState<UserRow[]>([])
  const [loading, setLoading] = useState(false)
  const [editing, setEditing] = useState<UserRow | null>(null) // null=关闭,username=''=新建
  const [saving, setSaving] = useState(false)
  const [nodeOpts, setNodeOpts] = useState<{ label: string; value: string }[]>([])
  const [grants, setGrants] = useState<NodeGrant[]>([])
  const [form] = Form.useForm<FormValues>()

  useEffect(() => {
    api<{ nodes: { id: string; name: string }[] | null }>('/api/nodes')
      .then((res) => setNodeOpts((res.nodes ?? []).map((n) => ({ label: n.name, value: n.id }))))
      .catch(() => setNodeOpts([]))
  }, [])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api<{ users: UserRow[] | null }>('/api/users')
      setList(res.users ?? [])
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '加载用户失败')
    } finally {
      setLoading(false)
    }
  }, [message])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = () => {
    form.setFieldsValue({
      username: '', password: '', permKeys: ['view', 'certCreate'], certLimit: 5, disabled: false,
    })
    setGrants([])
    setEditing({
      username: '', perms: toPerms([]), certLimit: 0, certsUsed: 0, nodeGrants: [], disabled: false, createdAt: '',
    })
  }

  const openEdit = (u: UserRow) => {
    form.setFieldsValue({
      username: u.username,
      password: '',
      permKeys: permOptions.filter((p) => u.perms[p.key]).map((p) => p.key),
      certLimit: u.certLimit,
      disabled: u.disabled,
    })
    setGrants((u.nodeGrants ?? []).map((g) => ({ ...g, perms: { ...g.perms } })))
    setEditing(u)
  }

  const save = async () => {
    const v = await form.validateFields()
    const isCreate = editing?.username === ''
    setSaving(true)
    try {
      if (isCreate) {
        await api('/api/users', {
          method: 'POST',
          body: JSON.stringify({
            username: v.username, password: v.password,
            perms: toPerms(v.permKeys), certLimit: v.certLimit, nodeGrants: grants,
          }),
        })
        message.success('子用户已创建')
      } else {
        await api(`/api/users/${encodeURIComponent(editing!.username)}`, {
          method: 'PUT',
          body: JSON.stringify({
            perms: toPerms(v.permKeys), certLimit: v.certLimit, nodeGrants: grants,
            disabled: v.disabled, password: v.password || '',
          }),
        })
        message.success('已保存,权限即时生效')
      }
      setEditing(null)
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const remove = async (name: string) => {
    try {
      await api(`/api/users/${encodeURIComponent(name)}`, { method: 'DELETE' })
      message.success('已删除(其名下证书保留,归属记录不变)')
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '删除失败')
    }
  }

  const columns: ColumnsType<UserRow> = [
    {
      title: '用户名',
      dataIndex: 'username',
      render: (v: string, u) => (
        <Space>
          <Typography.Text strong>{v}</Typography.Text>
          {u.disabled && <Tag color="red">已禁用</Tag>}
        </Space>
      ),
    },
    {
      title: '权限',
      key: 'perms',
      render: (_, u) => (
        <Space size={4} wrap>
          {permOptions.filter((p) => u.perms[p.key]).map((p) => (
            <Tag key={p.key} color="blue">{permLabel[p.key]}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '证书配额',
      key: 'quota',
      width: 110,
      render: (_, u) => (u.certLimit > 0 ? `${u.certsUsed} / ${u.certLimit}` : `${u.certsUsed} / 不限`),
    },
    {
      title: '节点授权',
      key: 'nodes',
      width: 200,
      render: (_, u) => {
        const gs = u.nodeGrants ?? []
        if (gs.length === 0) return '-'
        return (
          <Space size={4} wrap>
            {gs.map((g) => {
              const opt = nodeOpts.find((o) => o.value === g.nodeId)
              return (
                <Tag key={g.nodeId} color={g.full ? 'geekblue' : undefined}>
                  {(opt?.label ?? g.nodeId) + (g.full ? ' · 完整管理' : ' · 部分权限')}
                </Tag>
              )
            })}
          </Space>
        )
      },
    },
    { title: '创建时间', dataIndex: 'createdAt', width: 150 },
    {
      title: '操作',
      key: 'actions',
      width: 170,
      render: (_, u) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(u)}>
            编辑
          </Button>
          <Popconfirm
            title={`删除用户 ${u.username}?`}
            description="其创建的证书不会被吊销,仅删除账号。"
            okButtonProps={{ danger: true }}
            onConfirm={() => remove(u.username)}
          >
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const isCreate = editing?.username === ''

  return (
    <Card
      title="子用户管理"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建子用户
          </Button>
        </Space>
      }
    >
      <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
        子账户仅可在授权范围内使用面板;宿主机证书的下载、分享与吊销仅限其本人创建的证书。
        宿主机权限与节点授权相互独立,可仅授予节点授权以创建纯子节点管理员;全部修改即时生效。
      </Typography.Paragraph>
      <Table<UserRow>
        rowKey="username"
        columns={columns}
        dataSource={list}
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '还没有子用户' }}
      />

      <Modal
        title={isCreate ? '新建子用户' : `编辑 ${editing?.username}`}
        open={!!editing}
        onCancel={() => setEditing(null)}
        onOk={save}
        confirmLoading={saving}
        okText="保存"
        destroyOnClose
      >
        <Form<FormValues> form={form} layout="vertical">
          {isCreate && (
            <Form.Item
              name="username"
              label="用户名"
              rules={[
                { required: true, message: '请输入用户名' },
                { pattern: /^[a-z0-9][a-z0-9_-]{2,31}$/, message: '3-32 位小写字母/数字/下划线/连字符' },
              ]}
            >
              <Input autoFocus />
            </Form.Item>
          )}
          <Form.Item
            name="password"
            label={isCreate ? '密码' : '重置密码(留空不改)'}
            rules={isCreate ? [{ required: true, message: '请输入密码' }, { min: 8, message: '至少 8 位' }] : [{ min: 8, message: '至少 8 位' }]}
          >
            <Input.Password placeholder={isCreate ? '至少 8 位' : '留空则不修改'} />
          </Form.Item>
          <Form.Item
            name="permKeys"
            label="宿主机权限(作用于本机,可全部不选)"
            tooltip="全部不选并仅配置下方节点授权时,该用户即为纯子节点管理员:登录后管理目标自动指向其授权节点"
          >
            <Checkbox.Group options={permOptions.map((p) => ({ label: p.label, value: p.key }))} />
          </Form.Item>
          <Form.Item name="certLimit" label="有效证书数量上限(0 = 不限)" rules={[{ required: true }]}>
            <InputNumber min={0} max={1000} style={{ width: 180 }} />
          </Form.Item>
          <Divider orientation="left" plain style={{ fontSize: 13, margin: '8px 0' }}>
            节点授权
          </Divider>
          <Space direction="vertical" size={8} style={{ width: '100%', marginBottom: 8 }}>
            <Select<string | null>
              style={{ width: 280 }}
              placeholder={nodeOpts.length ? '添加节点授权' : '暂无子节点可分配'}
              value={null}
              options={nodeOpts.filter((o) => !grants.some((g) => g.nodeId === o.value))}
              onSelect={(id) => {
                if (typeof id !== 'string') return
                setGrants([...grants, { nodeId: id, full: false, perms: { ...emptyNodePerms(), view: true } }])
              }}
            />
            {grants.map((g, idx) => (
              <Card size="small" key={g.nodeId}>
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                  <Space wrap>
                    <Typography.Text strong>
                      {nodeOpts.find((o) => o.value === g.nodeId)?.label ?? g.nodeId}
                    </Typography.Text>
                    <Dropdown
                      menu={{
                        items: grantTemplates.map((t) => ({ key: t.key, label: t.label })),
                        onClick: ({ key }) => {
                          const t = grantTemplates.find((x) => x.key === key)!
                          const next = [...grants]
                          next[idx] = {
                            nodeId: g.nodeId,
                            full: t.full,
                            perms: { ...emptyNodePerms(), ...t.perms },
                          }
                          setGrants(next)
                        },
                      }}
                    >
                      <Button size="small">套用模板</Button>
                    </Dropdown>
                    <Switch
                      checkedChildren="完整管理"
                      unCheckedChildren="细分权限"
                      checked={g.full}
                      onChange={(full) => {
                        const next = [...grants]
                        next[idx] = { ...g, full }
                        setGrants(next)
                      }}
                    />
                    <Button
                      size="small"
                      danger
                      type="text"
                      icon={<DeleteOutlined />}
                      onClick={() => setGrants(grants.filter((_, i) => i !== idx))}
                    />
                  </Space>
                  {!g.full && (
                    <Checkbox.Group
                      options={nodePermOptions.map((o) => ({ label: o.label, value: o.key }))}
                      value={permKeysOf(g.perms)}
                      onChange={(keys) => {
                        const next = [...grants]
                        next[idx] = { ...g, perms: toNodePerms(keys as (keyof NodePerms)[]) }
                        setGrants(next)
                      }}
                    />
                  )}
                </Space>
              </Card>
            ))}
          </Space>
          {!isCreate && (
            <Form.Item name="disabled" label="禁用账号" valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
        </Form>
      </Modal>
    </Card>
  )
}
