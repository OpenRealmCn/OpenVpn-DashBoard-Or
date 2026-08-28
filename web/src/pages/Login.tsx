import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { App as AntApp, Button, Card, Form, Input, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import { useSession } from '../session'

interface FormValues {
  username?: string
  password: string
  confirm?: string
}

export default function Login() {
  const { session, refresh } = useSession()
  const nav = useNavigate()
  const { message } = AntApp.useApp()
  const [loading, setLoading] = useState(false)
  const setupMode = !session.initialized

  useEffect(() => {
    if (session.authenticated) nav('/', { replace: true })
  }, [session.authenticated, nav])

  const onFinish = async (v: FormValues) => {
    setLoading(true)
    try {
      await api(setupMode ? '/api/setup' : '/api/login', {
        method: 'POST',
        body: JSON.stringify({ username: setupMode ? 'admin' : v.username || 'admin', password: v.password }),
      })
      await refresh()
      message.success(setupMode ? '管理员密码已设置' : '登录成功')
      nav('/', { replace: true })
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '网络错误')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-bg">
      <Card className="login-card" styles={{ body: { padding: 32 } }}>
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <div className="brand-mark" style={{ width: 52, height: 52, fontSize: 26, margin: '0 auto 14px' }}>
            <SafetyCertificateOutlined />
          </div>
          <Typography.Title level={3} style={{ margin: 0 }}>
            OpenVpnTools
          </Typography.Title>
          <Typography.Text type="secondary">
            {setupMode ? '首次使用,请设置管理员密码' : 'OpenVPN 服务器管理面板'}
          </Typography.Text>
        </div>
        <Form<FormValues> onFinish={onFinish} layout="vertical" initialValues={{ username: 'admin' }} size="large">
          {!setupMode && (
            <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
              <Input prefix={<UserOutlined />} placeholder="admin 或子用户名" />
            </Form.Item>
          )}
          <Form.Item
            name="password"
            label={setupMode ? '管理员密码' : '密码'}
            rules={[
              { required: true, message: '请输入密码' },
              ...(setupMode ? [{ min: 8, message: '密码长度至少 8 位' }] : []),
            ]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder="密码" autoFocus />
          </Form.Item>
          {setupMode && (
            <Form.Item
              name="confirm"
              label="确认密码"
              dependencies={['password']}
              rules={[
                { required: true, message: '请再次输入密码' },
                ({ getFieldValue }) => ({
                  validator(_, value) {
                    if (!value || getFieldValue('password') === value) return Promise.resolve()
                    return Promise.reject(new Error('两次输入的密码不一致'))
                  },
                }),
              ]}
            >
              <Input.Password prefix={<LockOutlined />} placeholder="再次输入密码" />
            </Form.Item>
          )}
          <Button type="primary" htmlType="submit" block loading={loading} style={{ marginTop: 4 }}>
            {setupMode ? '设置并登录' : '登录'}
          </Button>
        </Form>
      </Card>
    </div>
  )
}
