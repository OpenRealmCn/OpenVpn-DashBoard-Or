import { useCallback, useEffect, useState } from 'react'
import {
  App as AntApp,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import type { Perms, UserRow } from '../types'

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
  const [form] = Form.useForm<FormValues>()

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
    setEditing({ username: '', perms: toPerms([]), certLimit: 0, certsUsed: 0, disabled: false, createdAt: '' })
  }

  const openEdit = (u: UserRow) => {
    form.setFieldsValue({
      username: u.username,
      password: '',
      permKeys: permOptions.filter((p) => u.perms[p.key]).map((p) => p.key),
      certLimit: u.certLimit,
      disabled: u.disabled,
    })
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
            perms: toPerms(v.permKeys), certLimit: v.certLimit,
          }),
        })
        message.success('子用户已创建')
      } else {
        await api(`/api/users/${encodeURIComponent(editing!.username)}`, {
          method: 'PUT',
          body: JSON.stringify({
            perms: toPerms(v.permKeys), certLimit: v.certLimit,
            disabled: v.disabled, password: v.password || '',
          }),
        })
        message.success('已保存(权限即时生效)')
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
        子用户按勾选的权限使用面板;下载/分享/吊销仅限自己创建的证书。权限与配额修改即时生效。
      </Typography.Paragraph>
      <Table<UserRow>
        rowKey="username"
        columns={columns}
        dataSource={list}
        loading={loading}
        size="middle"
        pagination={false}
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
          <Form.Item name="permKeys" label="权限">
            <Checkbox.Group options={permOptions.map((p) => ({ label: p.label, value: p.key }))} />
          </Form.Item>
          <Form.Item name="certLimit" label="有效证书数量上限(0 = 不限)" rules={[{ required: true }]}>
            <InputNumber min={0} max={1000} style={{ width: 180 }} />
          </Form.Item>
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
