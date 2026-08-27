import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Card,
  Descriptions,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import {
  CaretRightOutlined,
  CloudSyncOutlined,
  DownloadOutlined,
  PoweroffOutlined,
  RedoOutlined,
  ReloadOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { api, ApiError } from '../api/client'
import { useSession } from '../session'
import type { AuditEntry, ServiceStatus, StatusResp, VersionInfo } from '../types'

const auditColumns: ColumnsType<AuditEntry> = [
  {
    title: '时间',
    dataIndex: 'time',
    width: 170,
    render: (v: string) => new Date(v).toLocaleString(),
  },
  { title: '用户', dataIndex: 'user', width: 110 },
  { title: '操作', dataIndex: 'action' },
  {
    title: '结果',
    dataIndex: 'status',
    width: 90,
    render: (v: number) => (
      <Tag color={v < 400 ? 'green' : 'red'}>{v}</Tag>
    ),
  },
  { title: '来源 IP', dataIndex: 'ip', width: 140 },
]

export default function Maintenance() {
  const { message, modal } = AntApp.useApp()
  const { session } = useSession()
  const isAdmin = !!session.user?.isAdmin
  const [svc, setSvc] = useState<ServiceStatus | null>(null)
  const [ver, setVer] = useState<VersionInfo | null>(null)
  const [checking, setChecking] = useState(false)
  const [acting, setActing] = useState('')
  const [auditList, setAuditList] = useState<AuditEntry[]>([])
  const [auditLoading, setAuditLoading] = useState(false)

  const loadAudit = useCallback(async () => {
    setAuditLoading(true)
    try {
      const res = await api<{ entries: AuditEntry[] | null }>('/api/audit')
      setAuditList(res.entries ?? [])
    } catch {
      /* 非管理员或加载失败时忽略 */
    } finally {
      setAuditLoading(false)
    }
  }, [])

  const loadSvc = useCallback(async () => {
    try {
      const st = await api<StatusResp>('/api/status')
      setSvc(st.openvpn.service)
    } catch {
      /* 状态页会提示 */
    }
  }, [])

  useEffect(() => {
    void loadSvc()
    api<VersionInfo>('/api/version').then(setVer).catch(() => {})
    if (isAdmin) void loadAudit()
  }, [loadSvc, loadAudit, isAdmin])

  const ctl = async (action: 'start' | 'stop' | 'restart') => {
    setActing(action)
    try {
      const res = await api<{ service: ServiceStatus }>(`/api/service/openvpn/${action}`, {
        method: 'POST',
      })
      setSvc(res.service)
      message.success('操作完成')
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setActing('')
    }
  }

  const checkUpdates = async () => {
    setChecking(true)
    try {
      setVer(await api<VersionInfo>('/api/version?check=1'))
      message.success('检查完成')
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '检查更新失败')
    } finally {
      setChecking(false)
    }
  }

  const restartPanel = useCallback(async () => {
    try {
      await api('/api/panel/restart', { method: 'POST' })
    } catch {
      /* 连接可能随重启中断,属预期 */
    }
    message.loading('面板重启中…', 0)
    const t0 = Date.now()
    const timer = setInterval(() => {
      api('/api/session')
        .then(() => {
          clearInterval(timer)
          window.location.reload()
        })
        .catch(() => {
          if (Date.now() - t0 > 30000) {
            clearInterval(timer)
            message.destroy()
            message.error('面板未在 30 秒内恢复,请到服务器检查 ovpn-web 服务')
          }
        })
    }, 1500)
  }, [message])

  const upgrade = async (what: 'openvpn' | 'easyrsa' | 'panel') => {
    setActing('up-' + what)
    try {
      const res = await api<{ logs: string[]; needRestart?: boolean }>(`/api/update/${what}`, {
        method: 'POST',
      })
      if (res.needRestart) {
        modal.confirm({
          title: '面板已更新,重启后生效',
          content: (
            <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
              {(res.logs ?? []).join('\n')}
            </pre>
          ),
          okText: '立即重启面板',
          cancelText: '稍后手动重启',
          onOk: restartPanel,
        })
      } else {
        modal.success({
          title: '升级完成',
          content: (
            <pre style={{ maxHeight: 300, overflow: 'auto', fontSize: 12 }}>
              {(res.logs ?? []).join('\n')}
            </pre>
          ),
        })
        await checkUpdates()
        await loadSvc()
      }
    } catch (e) {
      message.error(e instanceof ApiError ? e.message : '升级失败')
    } finally {
      setActing('')
    }
  }

  const fileRef = useRef<HTMLInputElement>(null)
  const [restoring, setRestoring] = useState(false)

  const onRestoreFile = (f: File) => {
    modal.confirm({
      title: '确认恢复备份?',
      content: `将用「${f.name}」覆盖当前 PKI、server 配置与面板数据;被替换的文件会留底,OpenVPN 会自动重启。`,
      okText: '开始恢复',
      okButtonProps: { danger: true },
      onOk: async () => {
        setRestoring(true)
        try {
          const fd = new FormData()
          fd.append('file', f)
          const res = await fetch('/api/backup/restore', { method: 'POST', body: fd })
          const data = await res.json()
          if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
          modal.success({
            title: '恢复完成',
            content: `PKI ${data.summary.pkiFiles} 个文件、server 配置 ${data.summary.serverFiles} 个、面板数据 ${data.summary.dataFiles} 个;${data.note}。原文件留底:${data.summary.backupDir}`,
          })
        } catch (e) {
          message.error(e instanceof Error ? e.message : '恢复失败')
        } finally {
          setRestoring(false)
        }
      },
    })
  }

  const openvpnUpgradable = !!ver?.openvpnUpgrade
  const easyrsaUpgradable =
    !!ver?.easyrsaLatest && !!ver.easyrsa && ver.easyrsaLatest !== ver.easyrsa
  const panelUpgradable = !!ver?.panelLatest && ver.panelLatest !== ver.panel

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card title="OpenVPN 服务控制" size="small">
        <Space size="middle" wrap>
          {!svc || !svc.exists ? (
            <Badge status="default" text="未安装" />
          ) : svc.active ? (
            <Badge status="success" text="运行中" />
          ) : (
            <Badge status="error" text="已停止" />
          )}
          <Button
            icon={<CaretRightOutlined />}
            disabled={!svc?.exists || svc.active}
            loading={acting === 'start'}
            onClick={() => ctl('start')}
          >
            启动
          </Button>
          <Popconfirm title="重启会断开所有在线客户端,继续?" onConfirm={() => ctl('restart')}>
            <Button icon={<RedoOutlined />} disabled={!svc?.active} loading={acting === 'restart'}>
              重启
            </Button>
          </Popconfirm>
          <Popconfirm title="停止后所有客户端将离线,继续?" onConfirm={() => ctl('stop')}>
            <Button
              danger
              icon={<PoweroffOutlined />}
              disabled={!svc?.active}
              loading={acting === 'stop'}
            >
              停止
            </Button>
          </Popconfirm>
        </Space>
      </Card>

      <Card
        title="版本与更新"
        size="small"
        extra={
          <Button
            size="small"
            icon={<CloudSyncOutlined />}
            loading={checking}
            onClick={checkUpdates}
          >
            检查更新
          </Button>
        }
      >
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="面板">
              <Space>
                v{ver?.panel ?? '-'}
                {panelUpgradable && <Tag color="orange">可升级 → v{ver?.panelLatest}</Tag>}
                {ver?.checkedRemote && !panelUpgradable && ver.panelLatest && (
                  <Tag color="green">已最新</Tag>
                )}
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="OpenVPN">
              <Space>
                {ver?.openvpn || '未安装'}
                {openvpnUpgradable && <Tag color="orange">可升级 → {ver?.openvpnUpgrade}</Tag>}
                {ver?.checkedRemote && ver.openvpn && !openvpnUpgradable && (
                  <Tag color="green">已最新</Tag>
                )}
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="EasyRSA">
              <Space>
                {ver?.easyrsa || '未安装'}
                {easyrsaUpgradable && <Tag color="orange">可升级 → {ver?.easyrsaLatest}</Tag>}
                {ver?.checkedRemote && ver.easyrsa && !easyrsaUpgradable && (
                  <Tag color="green">已最新</Tag>
                )}
              </Space>
            </Descriptions.Item>
          </Descriptions>
          {!ver?.checkedRemote && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              点「检查更新」联网查询(apt 源 + GitHub API);EasyRSA 升级会校验 GitHub
              官方 SHA256、通过 bash -n 语法检查并完整保留 PKI。
            </Typography.Text>
          )}
          <Space>
            {openvpnUpgradable && (
              <Popconfirm
                title="升级 OpenVPN?若服务在运行,升级后会自动重启(客户端短暂断线)。"
                onConfirm={() => upgrade('openvpn')}
              >
                <Button type="primary" loading={acting === 'up-openvpn'}>
                  升级 OpenVPN
                </Button>
              </Popconfirm>
            )}
            {easyrsaUpgradable && (
              <Popconfirm
                title={`升级 EasyRSA 到 ${ver?.easyrsaLatest}?PKI(CA 与全部证书)会原样保留。`}
                onConfirm={() => upgrade('easyrsa')}
              >
                <Button type="primary" loading={acting === 'up-easyrsa'}>
                  升级 EasyRSA
                </Button>
              </Popconfirm>
            )}
            {panelUpgradable && (
              <Popconfirm
                title={`升级面板到 v${ver?.panelLatest}?下载校验后替换二进制,需重启面板生效。`}
                onConfirm={() => upgrade('panel')}
              >
                <Button type="primary" loading={acting === 'up-panel'}>
                  升级面板
                </Button>
              </Popconfirm>
            )}
          </Space>
          {ver?.checkedRemote && !openvpnUpgradable && !easyrsaUpgradable && ver.openvpn && (
            <Alert type="success" showIcon message="所有组件均为最新版本" />
          )}
        </Space>
      </Card>

      {isAdmin && (
        <Card
          title="备份"
          size="small"
        >
          <Space direction="vertical" size={8}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              导出 PKI(CA/证书/index)、server 配置、安装参数、子用户与证书归属;
              面板自身的 config.yaml(含密钥)请另行备份。恢复时被替换的文件会自动留底。
            </Typography.Text>
            <Space>
              <Button icon={<DownloadOutlined />} href="/api/backup">
                下载备份(tar.gz)
              </Button>
              <Button icon={<UploadOutlined />} loading={restoring} onClick={() => fileRef.current?.click()}>
                恢复备份
              </Button>
              <input
                type="file"
                accept=".gz,.tgz,application/gzip"
                hidden
                ref={fileRef}
                onChange={(e) => {
                  const f = e.target.files?.[0]
                  if (f) onRestoreFile(f)
                  e.target.value = ''
                }}
              />
            </Space>
          </Space>
        </Card>
      )}

      {isAdmin && (
        <Card
          title="审计日志(最近 200 条)"
          size="small"
          extra={
            <Button size="small" icon={<ReloadOutlined />} loading={auditLoading} onClick={loadAudit}>
              刷新
            </Button>
          }
        >
          <Table<AuditEntry>
            rowKey={(e) => e.time + e.action + e.ip}
            columns={auditColumns}
            dataSource={auditList}
            loading={auditLoading}
            size="small"
            pagination={{ pageSize: 15, hideOnSinglePage: true }}
            locale={{ emptyText: '暂无审计记录' }}
          />
        </Card>
      )}
    </Space>
  )
}
