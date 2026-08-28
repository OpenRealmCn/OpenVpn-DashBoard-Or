import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  List,
  Modal,
  Popconfirm,
  Radio,
  Row,
  Select,
  Space,
  Steps,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import { CheckCircleTwoTone, CloseCircleTwoTone, DownloadOutlined, PlusOutlined, StopOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import type { ClientCert, InstallState, NodeRow, PrecheckReport, StepStatus } from '../types'

export const hostOf = (url: string): string => {
  try {
    return new URL(url).hostname
  } catch {
    return url
  }
}

const installDefaults = {
  port: 1194,
  proto: 'udp',
  subnet: '10.8.0.0/24',
  enableIPv6: false,
  subnet6: 'fd42:42:42:42::/112',
  dnsMode: 'cloudflare',
}

const dnsOptions = [
  { value: 'cloudflare', label: 'Cloudflare(1.1.1.1)' },
  { value: 'google', label: 'Google(8.8.8.8)' },
  { value: 'system', label: '节点当前上游 DNS' },
  { value: 'custom', label: '自定义' },
]

// 安装参数字段组(须置于 Form 内)
function InstallFields() {
  const form = Form.useFormInstance()
  const dnsMode = Form.useWatch('dnsMode', form)
  const enableIPv6 = Form.useWatch('enableIPv6', form)
  return (
    <>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="port" label="监听端口" rules={[{ required: true, message: '请输入端口' }]}>
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="proto" label="协议">
            <Radio.Group
              optionType="button"
              options={[
                { label: 'UDP(推荐)', value: 'udp' },
                { label: 'TCP', value: 'tcp' },
              ]}
            />
          </Form.Item>
        </Col>
      </Row>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="subnet" label="VPN 网段" rules={[{ required: true, message: '请输入网段' }]}>
            <Input placeholder="10.8.0.0/24" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="dnsMode" label="推送给客户端的 DNS">
            <Select options={dnsOptions} />
          </Form.Item>
        </Col>
      </Row>
      <Row gutter={12}>
        <Col span={10}>
          <Form.Item name="enableIPv6" label="启用 IPv6(NAT66)" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Col>
        {enableIPv6 && (
          <Col span={14}>
            <Form.Item name="subnet6" label="IPv6 网段(ULA)" rules={[{ required: true, message: '请输入 IPv6 网段' }]}>
              <Input placeholder="fd42:42:42:42::/112" />
            </Form.Item>
          </Col>
        )}
      </Row>
      {dnsMode === 'custom' && (
        <Row gutter={12}>
          <Col span={12}>
            <Form.Item name="dns1" label="DNS 1" rules={[{ required: true, message: '请输入 DNS' }]}>
              <Input placeholder="223.5.5.5" />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item name="dns2" label="DNS 2(可选)">
              <Input placeholder="119.29.29.29" />
            </Form.Item>
          </Col>
        </Row>
      )}
    </>
  )
}

// 批量远程安装:公网地址自动使用各节点面板地址的主机名
export function RemoteInstallModal({
  open,
  nodes,
  onClose,
  onDone,
}: {
  open: boolean
  nodes: NodeRow[]
  onClose: () => void
  onDone: () => void
}) {
  const { message, modal } = AntApp.useApp()
  const [form] = Form.useForm()
  const [running, setRunning] = useState(false)
  const candidates = nodes.filter((n) => n.health.reachable && !n.health.installed)

  const submit = async () => {
    const v = await form.validateFields()
    const ids: string[] = v.nodeIds
    setRunning(true)
    try {
      const outcomes = await Promise.all(
        ids.map(async (id) => {
          const n = nodes.find((x) => x.id === id)!
          try {
            await api(`/api/nodes/${id}/proxy/install`, {
              method: 'POST',
              body: JSON.stringify({ ...v, nodeIds: undefined, publicAddr: hostOf(n.url) }),
            })
            return { name: n.name, ok: true, msg: '安装任务已启动' }
          } catch (e) {
            return { name: n.name, ok: false, msg: e instanceof ApiError ? e.message : '请求失败' }
          }
        }),
      )
      modal.info({
        title: '远程安装已下发',
        content: (
          <List
            size="small"
            dataSource={outcomes}
            renderItem={(o) => (
              <List.Item>
                <Space>
                  {o.ok ? <Tag color="green">已启动</Tag> : <Tag color="red">失败</Tag>}
                  <Typography.Text strong>{o.name}</Typography.Text>
                  <Typography.Text type="secondary">{o.msg}</Typography.Text>
                </Space>
              </List.Item>
            )}
          />
        ),
      })
      onDone()
      onClose()
    } catch (e) {
      if (e instanceof ApiError) message.error(e.message)
    } finally {
      setRunning(false)
    }
  }

  return (
    <Modal
      title="远程安装 OpenVPN"
      open={open}
      onCancel={onClose}
      onOk={submit}
      confirmLoading={running}
      okText="开始安装"
      width={640}
      destroyOnClose
    >
      <Form form={form} layout="vertical" initialValues={{ ...installDefaults, nodeIds: [] }}>
        <Form.Item
          name="nodeIds"
          label="目标节点(仅列出在线且未安装的节点)"
          rules={[{ required: true, message: '请选择至少一个节点' }]}
        >
          <Select
            mode="multiple"
            placeholder={candidates.length ? '选择要安装的节点' : '暂无可安装的节点'}
            options={candidates.map((n) => ({ label: `${n.name}(${hostOf(n.url)})`, value: n.id }))}
          />
        </Form.Item>
        <InstallFields />
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          各节点的客户端连接地址将自动使用其面板地址的主机名;安装进度可在节点详情的「安装」页查看。
        </Typography.Text>
      </Form>
    </Modal>
  )
}

const stepStatusMap: Record<StepStatus, 'wait' | 'process' | 'finish' | 'error'> = {
  pending: 'wait',
  running: 'process',
  done: 'finish',
  failed: 'error',
  skipped: 'finish',
}

// 单节点安装页:状态展示、预检、启动安装、失败回滚
function InstallPanel({ node, refreshList }: { node: NodeRow; refreshList: () => void }) {
  const { message } = AntApp.useApp()
  const [state, setState] = useState<InstallState | null>(null)
  const [precheck, setPrecheck] = useState<PrecheckReport | null>(null)
  const [busy, setBusy] = useState('')
  const [form] = Form.useForm()

  const load = useCallback(async () => {
    try {
      setState(await api<InstallState>(`/api/nodes/${node.id}/proxy/install/state`))
    } catch {
      /* 节点不可达时由概览页提示 */
    }
  }, [node.id])

  useEffect(() => {
    void load()
  }, [load])

  const running = state?.job?.state === 'running'
  useEffect(() => {
    if (!running) return
    const t = setInterval(() => void load(), 2500)
    return () => clearInterval(t)
  }, [running, load])

  const call = async (key: string, path: string, body?: unknown, okMsg?: string) => {
    setBusy(key)
    try {
      await api(`/api/nodes/${node.id}/proxy/${path}`, {
        method: 'POST',
        body: body ? JSON.stringify(body) : JSON.stringify({}),
      })
      if (okMsg) message.success(okMsg)
      await load()
      refreshList()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '请求失败')
    } finally {
      setBusy('')
    }
  }

  if (!state) return <Card loading size="small" />

  if (state.installed) {
    return <Alert type="success" showIcon message="该节点已完成 OpenVPN 部署" description="可在「客户端证书」页为该节点创建与管理证书。" />
  }

  const job = state.job
  if (job && job.state === 'running') {
    return (
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Alert type="info" showIcon message="安装进行中,进度自动刷新" />
        <Steps
          direction="vertical"
          size="small"
          items={job.steps.map((st) => ({ title: st.name, status: stepStatusMap[st.status] }))}
        />
      </Space>
    )
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {job?.state === 'rolled_back' && (
        <Alert type="error" showIcon message="上次安装失败,已自动完整回滚" description={job.error} />
      )}
      {job?.state === 'rollback_failed' && (
        <Alert
          type="error"
          showIcon
          message="上次安装失败,且部分回滚未复原"
          description={job.error}
          action={
            <Button danger loading={busy === 'rb'} onClick={() => call('rb', 'install/rollback', {}, '回滚完成')}>
              重试回滚
            </Button>
          }
        />
      )}
      {state.pendingJournal && (
        <Alert
          type="warning"
          showIcon
          message="检测到未完成的安装残留,须先回滚才能重新安装"
          action={
            <Button danger loading={busy === 'rb'} onClick={() => call('rb', 'install/rollback', {}, '回滚完成')}>
              立即回滚
            </Button>
          }
        />
      )}
      <Form form={form} layout="vertical" initialValues={{ ...installDefaults, publicAddr: hostOf(node.url) }}>
        <InstallFields />
        <Form.Item
          name="publicAddr"
          label="客户端连接地址"
          tooltip="默认使用节点面板地址的主机名;NAT 环境下请填写该节点的真实公网 IP 或域名"
          rules={[{ required: true, message: '请输入公网 IP 或域名' }]}
        >
          <Input />
        </Form.Item>
      </Form>
      {precheck && (
        <List
          size="small"
          dataSource={precheck.checks}
          renderItem={(chk) => (
            <List.Item>
              <Space align="start">
                {chk.ok ? <CheckCircleTwoTone twoToneColor="#16a34a" /> : <CloseCircleTwoTone twoToneColor="#e5484d" />}
                <span>
                  <Typography.Text strong>{chk.name}</Typography.Text>
                  <br />
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {chk.detail}
                  </Typography.Text>
                </span>
              </Space>
            </List.Item>
          )}
        />
      )}
      <Space>
        <Button
          loading={busy === 'pre'}
          onClick={async () => {
            const v = await form.validateFields()
            setBusy('pre')
            try {
              setPrecheck(
                await api<PrecheckReport>(`/api/nodes/${node.id}/proxy/install/precheck`, {
                  method: 'POST',
                  body: JSON.stringify(v),
                }),
              )
            } catch (e) {
              message.error(e instanceof ApiError ? e.message : '预检失败')
            } finally {
              setBusy('')
            }
          }}
        >
          执行预检
        </Button>
        <Button
          type="primary"
          loading={busy === 'ins'}
          onClick={async () => {
            const v = await form.validateFields()
            await call('ins', 'install', v, '安装任务已启动')
          }}
        >
          开始安装
        </Button>
      </Space>
    </Space>
  )
}

// 节点证书管理:经主节点代理直达子节点
function CertsPanel({ node }: { node: NodeRow }) {
  const { message } = AntApp.useApp()
  const [list, setList] = useState<ClientCert[]>([])
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [form] = Form.useForm()
  const withPass = Form.useWatch('withPass', form)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api<{ clients: ClientCert[] | null }>(`/api/nodes/${node.id}/proxy/clients`)
      setList(res.clients ?? [])
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setList([])
      else message.error(e instanceof ApiError ? e.message : '加载证书列表失败')
    } finally {
      setLoading(false)
    }
  }, [node.id, message])

  useEffect(() => {
    void load()
  }, [load])

  const create = async () => {
    const v = await form.validateFields()
    setCreating(true)
    try {
      await api(`/api/nodes/${node.id}/proxy/clients`, {
        method: 'POST',
        body: JSON.stringify({
          name: v.name,
          passphrase: v.withPass ? v.passphrase : '',
          expireDays: v.expireDays,
        }),
      })
      message.success(`证书 ${v.name} 已创建`)
      setCreateOpen(false)
      form.resetFields()
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  const revoke = async (cn: string) => {
    try {
      await api(`/api/nodes/${node.id}/proxy/clients/${encodeURIComponent(cn)}/revoke`, { method: 'POST' })
      message.success(`已吊销 ${cn}`)
      await load()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '吊销失败')
    }
  }

  const columns: ColumnsType<ClientCert> = [
    { title: '证书', dataIndex: 'cn', render: (v: string) => <Typography.Text strong>{v}</Typography.Text> },
    {
      title: '状态',
      key: 's',
      width: 90,
      render: (_, c) =>
        c.status === 'V' ? <Tag color="green">有效</Tag> : c.status === 'R' ? <Tag color="red">已吊销</Tag> : <Tag color="orange">已过期</Tag>,
    },
    { title: '到期时间', dataIndex: 'expiry', width: 170, render: (v: string) => new Date(v).toLocaleString() },
    {
      title: '操作',
      key: 'a',
      width: 190,
      render: (_, c) =>
        c.status === 'V' ? (
          <Space>
            <Button size="small" icon={<DownloadOutlined />} href={`/api/nodes/${node.id}/proxy/clients/${encodeURIComponent(c.cn)}/config`}>
              下载
            </Button>
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
          </Space>
        ) : null,
    },
  ]

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)} disabled={!node.health.installed}>
          新建证书
        </Button>
        <Button onClick={load} loading={loading}>
          刷新
        </Button>
      </Space>
      <Table<ClientCert>
        rowKey="serial"
        columns={columns}
        dataSource={list}
        loading={loading}
        size="small"
        pagination={{ pageSize: 8, hideOnSinglePage: true }}
        locale={{ emptyText: node.health.installed ? '该节点暂无客户端证书' : '该节点尚未安装 OpenVPN' }}
      />
      <Modal
        title="新建客户端证书"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={create}
        confirmLoading={creating}
        okText="创建"
        destroyOnClose
      >
        <Form form={form} layout="vertical" initialValues={{ withPass: false, expireDays: 825 }}>
          <Form.Item
            name="name"
            label="证书名称"
            rules={[
              { required: true, message: '请输入名称' },
              { pattern: /^[A-Za-z0-9_-]{1,64}$/, message: '仅支持字母、数字、下划线与连字符(1-64 位)' },
            ]}
          >
            <Input placeholder="例如 laptop-01" autoFocus />
          </Form.Item>
          <Form.Item name="expireDays" label="有效期(天)" rules={[{ required: true, message: '请输入有效期' }]}>
            <InputNumber min={1} max={3650} style={{ width: 180 }} />
          </Form.Item>
          <Form.Item name="withPass" label="私钥加密(导入时需输入密码)" valuePropName="checked">
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
              <Input.Password />
            </Form.Item>
          )}
        </Form>
      </Modal>
    </Space>
  )
}

// 节点详情抽屉:概览 / 安装 / 证书
export default function NodeDrawer({
  node,
  onClose,
  refreshList,
}: {
  node: NodeRow | null
  onClose: () => void
  refreshList: () => void
}) {
  const { message } = AntApp.useApp()
  const [acting, setActing] = useState('')

  const ctl = async (action: 'start' | 'restart' | 'stop') => {
    if (!node) return
    setActing(action)
    try {
      await api(`/api/nodes/${node.id}/proxy/service/openvpn/${action}`, { method: 'POST' })
      message.success('操作完成')
      refreshList()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setActing('')
    }
  }

  if (!node) return null
  const h = node.health

  return (
    <Drawer title={`节点:${node.name}`} width={760} open onClose={onClose} destroyOnClose>
      <Tabs
        items={[
          {
            key: 'overview',
            label: '概览',
            children: (
              <Space direction="vertical" size={16} style={{ width: '100%' }}>
                {!h.reachable && <Alert type="error" showIcon message="节点不可达" description={h.error} />}
                <Descriptions column={2} size="small" bordered>
                  <Descriptions.Item label="节点地址" span={2}>
                    <Typography.Link href={node.url} target="_blank">
                      {node.url}
                    </Typography.Link>
                  </Descriptions.Item>
                  <Descriptions.Item label="面板版本">{h.version ? `v${h.version}` : '-'}</Descriptions.Item>
                  <Descriptions.Item label="运行模式">{h.mode || '-'}</Descriptions.Item>
                  <Descriptions.Item label="OpenVPN">
                    {!h.reachable ? '-' : !h.installed ? <Tag>未安装</Tag> : h.serviceActive ? (
                      <Badge status="processing" color="#16a34a" text="运行中" />
                    ) : (
                      <Badge status="error" text="已停止" />
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label="在线客户端">{h.reachable ? h.online : '-'}</Descriptions.Item>
                  <Descriptions.Item label="接入时间" span={2}>
                    {new Date(node.addedAt).toLocaleString()}
                  </Descriptions.Item>
                </Descriptions>
                {h.installed && (
                  <Card size="small" title="服务控制">
                    <Space wrap>
                      <Button loading={acting === 'start'} disabled={h.serviceActive} onClick={() => ctl('start')}>
                        启动
                      </Button>
                      <Popconfirm title="重启将断开该节点全部在线客户端,是否继续?" onConfirm={() => ctl('restart')}>
                        <Button loading={acting === 'restart'} disabled={!h.serviceActive}>
                          重启
                        </Button>
                      </Popconfirm>
                      <Popconfirm title="停止后该节点全部客户端将离线,是否继续?" onConfirm={() => ctl('stop')}>
                        <Button danger loading={acting === 'stop'} disabled={!h.serviceActive}>
                          停止
                        </Button>
                      </Popconfirm>
                    </Space>
                  </Card>
                )}
              </Space>
            ),
          },
          { key: 'install', label: '安装', children: <InstallPanel node={node} refreshList={refreshList} /> },
          { key: 'certs', label: '客户端证书', children: <CertsPanel node={node} /> },
        ]}
      />
    </Drawer>
  )
}
