import { useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { App as AntApp, Button, Dropdown, Form, Input, Layout, Menu, Modal, Space, Tag } from 'antd'
import {
  DashboardOutlined,
  KeyOutlined,
  LogoutOutlined,
  RocketOutlined,
  SettingOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
  ToolOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import { hasPerm, useSession } from '../session'
import type { Perms } from '../types'

interface MenuDef {
  key: string
  icon: React.ReactNode
  label: string
  perm?: keyof Perms
  adminOnly?: boolean
}

const allMenus: MenuDef[] = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘', perm: 'view' },
  { key: '/install', icon: <RocketOutlined />, label: '安装向导', perm: 'install' },
  { key: '/clients', icon: <SafetyCertificateOutlined />, label: '客户端证书', perm: 'view' },
  { key: '/maintenance', icon: <ToolOutlined />, label: '系统维护', perm: 'maintain' },
  { key: '/users', icon: <TeamOutlined />, label: '用户管理', adminOnly: true },
  { key: '/settings', icon: <SettingOutlined />, label: '面板设置', adminOnly: true },
]

function ChangePasswordModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { message } = AntApp.useApp()
  const [form] = Form.useForm<{ oldPassword: string; newPassword: string; confirm: string }>()
  const [loading, setLoading] = useState(false)

  const submit = async () => {
    const v = await form.validateFields()
    setLoading(true)
    try {
      await api('/api/auth/password', {
        method: 'POST',
        body: JSON.stringify({ oldPassword: v.oldPassword, newPassword: v.newPassword }),
      })
      message.success('密码已修改')
      form.resetFields()
      onClose()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '修改失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Modal title="修改密码" open={open} onCancel={onClose} onOk={submit} confirmLoading={loading} okText="修改">
      <Form form={form} layout="vertical">
        <Form.Item name="oldPassword" label="旧密码" rules={[{ required: true, message: '请输入旧密码' }]}>
          <Input.Password />
        </Form.Item>
        <Form.Item
          name="newPassword"
          label="新密码"
          rules={[
            { required: true, message: '请输入新密码' },
            { min: 8, message: '至少 8 位' },
          ]}
        >
          <Input.Password />
        </Form.Item>
        <Form.Item
          name="confirm"
          label="确认新密码"
          dependencies={['newPassword']}
          rules={[
            { required: true, message: '请再次输入' },
            ({ getFieldValue }) => ({
              validator(_, value) {
                if (!value || getFieldValue('newPassword') === value) return Promise.resolve()
                return Promise.reject(new Error('两次输入不一致'))
              },
            }),
          ]}
        >
          <Input.Password />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export default function AppLayout() {
  const nav = useNavigate()
  const loc = useLocation()
  const { session, refresh } = useSession()
  const { message } = AntApp.useApp()
  const [pwOpen, setPwOpen] = useState(false)

  const menus = useMemo(
    () =>
      allMenus.filter((m) => {
        if (m.adminOnly) return session.user?.isAdmin
        if (m.perm) return hasPerm(session, m.perm)
        return true
      }),
    [session],
  )

  const selectedKey = useMemo(() => {
    const hit = menus.filter((m) => m.key !== '/').find((m) => loc.pathname.startsWith(m.key))
    return hit ? hit.key : '/'
  }, [loc.pathname, menus])

  const logout = async () => {
    try {
      await api('/api/logout', { method: 'POST' })
      await refresh()
    } catch {
      message.error('注销失败')
    }
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Sider breakpoint="lg" collapsedWidth={64} theme="dark">
        <div style={{ padding: '18px 16px', color: '#fff', fontWeight: 600, fontSize: 16 }}>
          OpenVpnTools
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menus.map(({ key, icon, label }) => ({ key, icon, label }))}
          onClick={(e) => nav(e.key)}
        />
      </Layout.Sider>
      <Layout>
        <Layout.Header
          style={{
            background: '#fff',
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <Space>
            <span style={{ fontWeight: 600 }}>OpenVPN 服务器管理</span>
            {session.mode === 'mock' && <Tag color="orange">mock 演示模式</Tag>}
          </Space>
          <Dropdown
            menu={{
              items: [
                { key: 'pw', icon: <KeyOutlined />, label: '修改密码', onClick: () => setPwOpen(true) },
                { key: 'out', icon: <LogoutOutlined />, label: '退出登录', onClick: logout },
              ],
            }}
          >
            <Button icon={<UserOutlined />}>
              {session.user?.username ?? '-'}
              {session.user?.isAdmin ? '(管理员)' : ''}
            </Button>
          </Dropdown>
        </Layout.Header>
        <Layout.Content style={{ padding: 24, maxWidth: 1200, width: '100%', margin: '0 auto' }}>
          <Outlet />
        </Layout.Content>
      </Layout>
      <ChangePasswordModal open={pwOpen} onClose={() => setPwOpen(false)} />
    </Layout>
  )
}
