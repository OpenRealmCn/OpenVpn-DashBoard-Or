import { useEffect, useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  App as AntApp,
  Button,
  Drawer,
  Dropdown,
  Form,
  Input,
  Layout,
  Menu,
  Modal,
  Select,
  Space,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import {
  ClusterOutlined,
  DashboardOutlined,
  KeyOutlined,
  LogoutOutlined,
  MenuOutlined,
  MoonOutlined,
  RocketOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  SunOutlined,
  TeamOutlined,
  ToolOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import { hasPerm, useSession } from '../session'
import { TargetProvider, useTarget } from '../target'
import { useTheme } from '../theme'
import type { Perms } from '../types'

interface MenuDef {
  key: string
  icon: React.ReactNode
  label: string
  perm?: keyof Perms
  adminOnly?: boolean
}

const allMenus: MenuDef[] = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/install', icon: <RocketOutlined />, label: '安装向导', perm: 'install' },
  { key: '/clients', icon: <SafetyCertificateOutlined />, label: '客户端证书' },
  { key: '/maintenance', icon: <ToolOutlined />, label: '系统维护', perm: 'maintain' },
  { key: '/nodes', icon: <ClusterOutlined />, label: '节点管理' },
  { key: '/users', icon: <TeamOutlined />, label: '用户管理', adminOnly: true },
  { key: '/settings', icon: <SettingOutlined />, label: '面板设置', adminOnly: true },
]

// 管理目标选择器只在与目标相关的页面展示
const targetAwarePages = ['/', '/clients']

// useIsMobile 以 matchMedia 同步初始化,避免桌面端首帧闪烁
function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(() => window.matchMedia('(max-width: 767px)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 767px)')
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return isMobile
}

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

function LayoutInner() {
  const nav = useNavigate()
  const loc = useLocation()
  const { session, refresh } = useSession()
  const { message } = AntApp.useApp()
  const { dark, toggle } = useTheme()
  const { target, setTarget, options } = useTarget()
  const [pwOpen, setPwOpen] = useState(false)
  const isMobile = useIsMobile()
  const [navOpen, setNavOpen] = useState(false)

  const menus = useMemo(() => {
    const u = session.user
    const nodeViewable = !!u?.isAdmin || (u?.nodeGrants ?? []).some((g) => g.full || g.perms.view)
    return allMenus.filter((m) => {
      // 节点管理:管理员或持有任意节点授权
      if (m.key === '/nodes') return !!u?.isAdmin || (u?.nodeGrants?.length ?? 0) > 0
      // 仪表盘/客户端证书:宿主机查看权限,或任一节点的查看授权(纯子节点管理员)
      if (m.key === '/' || m.key === '/clients') return hasPerm(session, 'view') || nodeViewable
      if (m.adminOnly) return u?.isAdmin
      if (m.perm) return hasPerm(session, m.perm)
      return true
    })
  }, [session])

  const selectedKey = useMemo(() => {
    const hit = menus.filter((m) => m.key !== '/').find((m) => loc.pathname.startsWith(m.key))
    return hit ? hit.key : '/'
  }, [loc.pathname, menus])

  const pageTitle = menus.find((m) => m.key === selectedKey)?.label ?? ''
  const showTarget = targetAwarePages.includes(selectedKey) && options.length > 1

  const logout = async () => {
    try {
      await api('/api/logout', { method: 'POST' })
      await refresh()
    } catch {
      message.error('注销失败')
    }
  }

  const brand = (
    <div className="brand">
      <div className="brand-mark">
        <SafetyCertificateOutlined />
      </div>
      <span className="brand-name">OpenVpnTools</span>
    </div>
  )
  const menuEl = (
    <Menu
      theme="light"
      mode="inline"
      selectedKeys={[selectedKey]}
      items={menus.map(({ key, icon, label }) => ({ key, icon, label }))}
      onClick={(e) => {
        nav(e.key)
        setNavOpen(false)
      }}
    />
  )

  return (
    <Layout style={{ minHeight: '100vh' }}>
      {!isMobile && (
        <Layout.Sider breakpoint="lg" collapsedWidth={64} className="app-sider">
          {brand}
          {menuEl}
        </Layout.Sider>
      )}
      <Layout>
        <Layout.Header
          className="app-header"
          style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}
        >
          <Space size={isMobile ? 4 : 12}>
            {isMobile && (
              <Button type="text" aria-label="打开菜单" icon={<MenuOutlined />} onClick={() => setNavOpen(true)} />
            )}
            <Typography.Text strong style={{ fontSize: 15, whiteSpace: 'nowrap' }}>
              {pageTitle}
            </Typography.Text>
            {showTarget && (
              <Tooltip title="选择本页作用的管理目标">
                <Select
                  size="small"
                  style={{ minWidth: isMobile ? 130 : 170 }}
                  value={target}
                  onChange={setTarget}
                  options={options}
                />
              </Tooltip>
            )}
            {session.mode === 'mock' && !isMobile && <Tag color="orange">mock 演示模式</Tag>}
          </Space>
          <Space size={4}>
            <Tooltip title={dark ? '切换为亮色模式' : '切换为暗色模式'}>
              <Button
                type="text"
                shape="circle"
                aria-label="切换主题"
                icon={dark ? <SunOutlined /> : <MoonOutlined />}
                onClick={toggle}
              />
            </Tooltip>
            <Dropdown
              menu={{
                items: [
                  { key: 'pw', icon: <KeyOutlined />, label: '修改密码', onClick: () => setPwOpen(true) },
                  { key: 'out', icon: <LogoutOutlined />, label: '退出登录', onClick: logout },
                ],
              }}
            >
              <Button type="text" icon={<UserOutlined />}>
                {!isMobile && (
                  <>
                    {session.user?.username ?? '-'}
                    {session.user?.isAdmin ? '(管理员)' : ''}
                  </>
                )}
              </Button>
            </Dropdown>
          </Space>
        </Layout.Header>
        <Layout.Content className="app-content">
          <div key={loc.pathname + target} className="page-enter">
            <Outlet />
          </div>
        </Layout.Content>
      </Layout>
      {isMobile && (
        <Drawer
          placement="left"
          width={248}
          open={navOpen}
          onClose={() => setNavOpen(false)}
          closable={false}
          className="app-nav-drawer"
          styles={{ body: { padding: 0, background: 'var(--sider-bg)' } }}
        >
          {brand}
          {menuEl}
        </Drawer>
      )}
      <ChangePasswordModal open={pwOpen} onClose={() => setPwOpen(false)} />
    </Layout>
  )
}

export default function AppLayout() {
  return (
    <TargetProvider>
      <LayoutInner />
    </TargetProvider>
  )
}
