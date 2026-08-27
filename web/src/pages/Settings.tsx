import { useEffect, useState } from 'react'
import { App as AntApp, Button, Card, Form, Input, Select, Typography } from 'antd'
import { api, ApiError } from '../api/client'
import type { Settings as SettingsData } from '../types'

export default function Settings() {
  const [form] = Form.useForm<SettingsData>()
  const [loading, setLoading] = useState(false)
  const { message } = AntApp.useApp()
  const tlsMode = Form.useWatch('tlsMode', form)

  useEffect(() => {
    api<SettingsData>('/api/settings')
      .then((data) => form.setFieldsValue(data))
      .catch(() => message.error('加载设置失败'))
  }, [form, message])

  const onFinish = async (v: SettingsData) => {
    setLoading(true)
    try {
      const res = await api<{ ok: boolean; note?: string }>('/api/settings', {
        method: 'PUT',
        body: JSON.stringify(v),
      })
      message.success('设置已保存')
      if (res.note) message.info(res.note, 5)
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Card title="面板设置" style={{ maxWidth: 640 }}>
      <Form<SettingsData> form={form} layout="vertical" onFinish={onFinish}>
        <Form.Item
          name="listen"
          label="面板监听地址"
          tooltip="host:port 形式,修改后需重启面板"
          rules={[{ required: true, message: '请输入监听地址' }]}
        >
          <Input placeholder="0.0.0.0:8686" />
        </Form.Item>
        <Form.Item
          name="panelUrl"
          label="面板外部地址"
          tooltip="生成二维码时使用,手机需要能访问该地址;留空则使用当前浏览器地址"
        >
          <Input placeholder="http://服务器IP:8686" allowClear />
        </Form.Item>
        <Form.Item
          name="githubMirror"
          label="GitHub 下载镜像"
          tooltip="EasyRSA 下载镜像前缀,留空直连 GitHub;SHA256 校验保证镜像不可信也安全"
        >
          <Input placeholder="https://mirror.example.com/https://github.com" allowClear />
        </Form.Item>
        <Form.Item name="tlsMode" label="HTTPS 模式">
          <Select
            options={[
              { value: 'off', label: '关闭(HTTP)' },
              { value: 'self', label: '自签名证书(浏览器有一次告警)' },
              { value: 'le', label: "Let's Encrypt 受信证书(需域名)" },
            ]}
          />
        </Form.Item>
        {tlsMode === 'le' && (
          <>
            <Form.Item
              name="tlsDomain"
              label="面板域名"
              tooltip="必须已解析到本机;签发与续期需要 80/443 端口可从公网访问"
              rules={[{ required: true, message: '请输入域名' }]}
            >
              <Input placeholder="panel.example.com" />
            </Form.Item>
            <Form.Item name="tlsEmail" label="邮箱(可选,证书到期提醒)">
              <Input placeholder="admin@example.com" allowClear />
            </Form.Item>
          </>
        )}
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
          监听地址与 HTTPS 变更需要重启面板进程后生效;Let's Encrypt 模式会强制监听
          443 并占用 80 端口做证书验证。
        </Typography.Paragraph>
        <Button type="primary" htmlType="submit" loading={loading}>
          保存
        </Button>
      </Form>
    </Card>
  )
}
