import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  List,
  Radio,
  Result,
  Row,
  Select,
  Space,
  Steps,
  Switch,
  Tag,
  Typography,
} from 'antd'
import { CheckCircleTwoTone, CloseCircleTwoTone, PlayCircleOutlined } from '@ant-design/icons'
import { api, ApiError } from '../api/client'
import type { InstallParams, InstallState, LogEvent, PrecheckReport, StepStatus } from '../types'

const defaultParams: InstallParams = {
  port: 1194,
  proto: 'udp',
  subnet: '10.8.0.0/24',
  enableIPv6: false,
  subnet6: 'fd42:42:42:42::/112',
  dnsMode: 'cloudflare',
  publicAddr: '',
}

const stepStatusMap: Record<StepStatus, 'wait' | 'process' | 'finish' | 'error'> = {
  pending: 'wait',
  running: 'process',
  done: 'finish',
  failed: 'error',
  skipped: 'finish',
}

function logColor(level: string): string {
  if (level === 'error') return '#ff7875'
  if (level === 'step') return '#69b1ff'
  if (level === 'state') return '#95de64'
  return '#d9d9d9'
}

export default function InstallWizard() {
  const [form] = Form.useForm<InstallParams>()
  const { message } = AntApp.useApp()
  const [state, setState] = useState<InstallState | null>(null)
  const [precheck, setPrecheck] = useState<PrecheckReport | null>(null)
  const [checking, setChecking] = useState(false)
  const [starting, setStarting] = useState(false)
  const [rollbacking, setRollbacking] = useState(false)
  const [logs, setLogs] = useState<LogEvent[]>([])
  const logBoxRef = useRef<HTMLDivElement>(null)
  const esRef = useRef<EventSource | null>(null)
  const dnsMode = Form.useWatch('dnsMode', form)
  const enableIPv6 = Form.useWatch('enableIPv6', form)

  const loadState = useCallback(async () => {
    const st = await api<InstallState>('/api/install/state')
    setState(st)
    return st
  }, [])

  const runPrecheck = useCallback(
    async (values?: Partial<InstallParams>) => {
      setChecking(true)
      try {
        const body = { ...defaultParams, ...values }
        const rep = await api<PrecheckReport>('/api/install/precheck', {
          method: 'POST',
          body: JSON.stringify(body),
        })
        setPrecheck(rep)
        if (rep.suggestedAddr && !form.getFieldValue('publicAddr')) {
          form.setFieldValue('publicAddr', rep.suggestedAddr)
        }
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : '预检失败')
      } finally {
        setChecking(false)
      }
    },
    [form, message],
  )

  // 初始加载:状态 + 预检
  useEffect(() => {
    loadState()
      .then((st) => {
        if (!st.installed && !st.job) void runPrecheck()
      })
      .catch(() => message.error('加载安装状态失败'))
  }, [loadState, runPrecheck, message])

  // 任务运行中:轮询状态 + SSE 日志
  const running = state?.job?.state === 'running'
  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => void loadState().catch(() => {}), 2000)
    return () => clearInterval(timer)
  }, [running, loadState])

  useEffect(() => {
    if (!state?.job) return
    if (esRef.current) return
    const es = new EventSource('/api/install/events')
    esRef.current = es
    es.addEventListener('log', (ev) => {
      const e = JSON.parse((ev as MessageEvent).data) as LogEvent
      setLogs((prev) => (prev.length && prev[prev.length - 1].seq >= e.seq ? prev : [...prev, e]))
    })
    es.addEventListener('state', () => {
      void loadState()
      es.close()
      esRef.current = null
    })
    es.onerror = () => {
      if (state.job?.state !== 'running') {
        es.close()
        esRef.current = null
      }
    }
    return () => {
      es.close()
      esRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state?.job?.id])

  useEffect(() => {
    logBoxRef.current?.scrollTo({ top: logBoxRef.current.scrollHeight })
  }, [logs])

  const start = async () => {
    const values = await form.validateFields()
    setStarting(true)
    try {
      setLogs([])
      await api('/api/install', { method: 'POST', body: JSON.stringify(values) })
      message.success('安装任务已启动')
      await loadState()
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '启动失败')
    } finally {
      setStarting(false)
    }
  }

  const rollback = async () => {
    setRollbacking(true)
    try {
      const res = await api<{ ok: boolean; failures: string[] }>('/api/install/rollback', {
        method: 'POST',
      })
      if (res.ok) message.success('已完整回滚')
      else message.warning('回滚完成但有未复原项: ' + res.failures.join('; '))
      await loadState()
      setLogs([])
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '回滚失败')
    } finally {
      setRollbacking(false)
    }
  }

  if (!state) return <Card loading />

  if (state.installed) {
    return (
      <Card>
        <Result
          status="success"
          title="OpenVPN 已安装并由本面板管理"
          subTitle="可以前往客户端证书页创建、吊销证书并生成二维码分享"
          extra={
            <Link to="/clients">
              <Button type="primary">管理客户端证书</Button>
            </Link>
          }
        />
      </Card>
    )
  }

  const job = state.job

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      {state.pendingJournal && (
        <Alert
          type="warning"
          showIcon
          message="检测到上次安装失败的残留"
          description="必须先按回滚日志恢复系统原状(包括 DNS drop-in、sysctl、防火墙规则等),才能重新安装。"
          action={
            <Button danger loading={rollbacking} onClick={rollback}>
              立即回滚
            </Button>
          }
        />
      )}

      {!job && !state.pendingJournal && (
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={13}>
            <Card title="安装参数">
              <Form<InstallParams> form={form} layout="vertical" initialValues={defaultParams}>
                <Row gutter={12}>
                  <Col span={12}>
                    <Form.Item
                      name="port"
                      label="监听端口"
                      rules={[{ required: true, message: '请输入端口' }]}
                    >
                      <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="proto" label="协议">
                      <Radio.Group
                        options={[
                          { label: 'UDP(推荐)', value: 'udp' },
                          { label: 'TCP', value: 'tcp' },
                        ]}
                        optionType="button"
                      />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item
                  name="subnet"
                  label="VPN 网段"
                  rules={[{ required: true, message: '请输入网段' }]}
                >
                  <Input placeholder="10.8.0.0/24" />
                </Form.Item>
                <Row gutter={12}>
                  <Col span={10}>
                    <Form.Item
                      name="enableIPv6"
                      label="启用 IPv6(NAT66)"
                      valuePropName="checked"
                      tooltip="为 VPN 客户端提供 IPv6 出口:server-ipv6 + ip6tables MASQUERADE,需宿主机具备 IPv6"
                    >
                      <Switch />
                    </Form.Item>
                  </Col>
                  {enableIPv6 && (
                    <Col span={14}>
                      <Form.Item
                        name="subnet6"
                        label="IPv6 网段(ULA)"
                        rules={[{ required: true, message: '请输入 IPv6 网段' }]}
                      >
                        <Input placeholder="fd42:42:42:42::/112" />
                      </Form.Item>
                    </Col>
                  )}
                </Row>
                <Form.Item name="dnsMode" label="推送给客户端的 DNS">
                  <Select
                    options={[
                      { value: 'cloudflare', label: 'Cloudflare(1.1.1.1)' },
                      { value: 'google', label: 'Google(8.8.8.8)' },
                      { value: 'system', label: '本机当前上游 DNS' },
                      { value: 'self', label: '本机 resolved 服务客户端(drop-in 方式)' },
                      { value: 'custom', label: '自定义' },
                    ]}
                  />
                </Form.Item>
                {dnsMode === 'custom' && (
                  <Row gutter={12}>
                    <Col span={12}>
                      <Form.Item
                        name="dns1"
                        label="DNS 1"
                        rules={[{ required: true, message: '请输入 DNS' }]}
                      >
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
                <Form.Item
                  name="publicAddr"
                  label="服务器公网地址"
                  tooltip="客户端 remote 使用;自动探测的是本机路由源地址,NAT 环境请改成真实公网 IP 或域名"
                  rules={[{ required: true, message: '请输入公网 IP 或域名' }]}
                >
                  <Input placeholder="203.0.113.10 或 vpn.example.com" />
                </Form.Item>
                <Space>
                  <Button
                    loading={checking}
                    onClick={() => runPrecheck(form.getFieldsValue())}
                  >
                    重新预检
                  </Button>
                  <Button
                    type="primary"
                    icon={<PlayCircleOutlined />}
                    loading={starting}
                    disabled={!precheck?.ok}
                    onClick={start}
                  >
                    开始安装
                  </Button>
                </Space>
              </Form>
            </Card>
          </Col>
          <Col xs={24} lg={11}>
            <Card title="环境预检" loading={checking && !precheck}>
              {precheck && (
                <List
                  size="small"
                  dataSource={precheck.checks}
                  renderItem={(chk) => (
                    <List.Item>
                      <Space align="start">
                        {chk.ok ? (
                          <CheckCircleTwoTone twoToneColor="#52c41a" />
                        ) : (
                          <CloseCircleTwoTone twoToneColor="#ff4d4f" />
                        )}
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
              {precheck && !precheck.ok && (
                <Alert type="error" showIcon message="预检未通过,修正后请点击「重新预检」" />
              )}
            </Card>
          </Col>
        </Row>
      )}

      {job && (
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={8}>
            <Card title="安装进度" size="small">
              <Steps
                direction="vertical"
                size="small"
                items={job.steps.map((st) => ({
                  title: st.name,
                  status: stepStatusMap[st.status],
                  description: st.status === 'skipped' ? '已跳过' : undefined,
                }))}
              />
            </Card>
          </Col>
          <Col xs={24} lg={16}>
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              {job.state === 'success' && (
                <Alert
                  type="success"
                  showIcon
                  message="安装完成"
                  description={
                    <Space>
                      OpenVPN 已启动并设置开机自启。
                      <Link to="/clients">去创建客户端证书</Link>
                    </Space>
                  }
                />
              )}
              {job.state === 'rolled_back' && (
                <Alert
                  type="error"
                  showIcon
                  message="安装失败,已自动完整回滚"
                  description={job.error}
                  action={
                    <Button onClick={() => { setState({ ...state, job: undefined }); setLogs([]); void runPrecheck(form.getFieldsValue()) }}>
                      调整参数重试
                    </Button>
                  }
                />
              )}
              {job.state === 'rollback_failed' && (
                <Alert
                  type="error"
                  showIcon
                  message="安装失败,且部分回滚未复原"
                  description={job.error}
                  action={
                    <Button danger loading={rollbacking} onClick={rollback}>
                      重试回滚
                    </Button>
                  }
                />
              )}
              <Card
                size="small"
                title={
                  <Space>
                    安装日志
                    {job.state === 'running' && <Tag color="processing">进行中</Tag>}
                  </Space>
                }
              >
                <div
                  ref={logBoxRef}
                  style={{
                    background: '#141414',
                    borderRadius: 6,
                    padding: 12,
                    height: 420,
                    overflow: 'auto',
                    fontFamily: 'Consolas, Menlo, monospace',
                    fontSize: 12.5,
                    lineHeight: 1.7,
                  }}
                >
                  {logs.map((l) => (
                    <div key={l.seq} style={{ color: logColor(l.level) }}>
                      {l.step ? `[${l.step}] ` : ''}
                      {l.msg}
                    </div>
                  ))}
                </div>
              </Card>
            </Space>
          </Col>
        </Row>
      )}
    </Space>
  )
}
