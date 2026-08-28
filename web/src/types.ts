export interface Perms {
  view: boolean
  certCreate: boolean
  certRevoke: boolean
  install: boolean
  kick: boolean
  maintain: boolean
}

export interface NodePerms {
  view: boolean
  certCreate: boolean
  certRevoke: boolean
  install: boolean
  rollback: boolean
  service: boolean
  kick: boolean
  upgrade: boolean
  panelRestart: boolean
}

export interface NodeGrant {
  nodeId: string
  full: boolean
  perms: NodePerms
}

export interface PanelUser {
  username: string
  isAdmin: boolean
  perms: Perms
  certLimit: number
  certsUsed: number
  nodeGrants: NodeGrant[] | null
}

export interface Session {
  initialized: boolean
  authenticated: boolean
  mode: 'linux' | 'mock'
  user?: PanelUser
}

export interface UserRow {
  username: string
  perms: Perms
  certLimit: number
  certsUsed: number
  nodeGrants: NodeGrant[] | null
  disabled: boolean
  createdAt: string
}

export interface AuditEntry {
  time: string
  user: string
  action: string
  status: number
  ip: string
}

export interface ServiceStatus {
  exists: boolean
  active: boolean
  enabled: boolean
}

export interface PortInfo {
  proto: string
  addr: string
  port: number
  pid: number
  comm: string
  unit: string
}

export type OccupantClass = 'resolved' | 'openvpn' | 'known-dns' | 'unknown'

export interface Occupant extends PortInfo {
  class: OccupantClass
}

export interface Port53Report {
  free: boolean
  occupants: Occupant[] | null
}

export interface DnsState {
  resolvedExists: boolean
  resolvedActive: boolean
  dropInPresent: boolean
  resolvConfIsLink: boolean
  resolvConfTarget: string
  backupPresent: boolean
  port53: Port53Report
}

export interface StatusResp {
  mode: string
  os: { id: string; versionId: string; pretty: string }
  openvpn: { unit: string; service: ServiceStatus; error: string }
  ports: PortInfo[]
  portsError: string
  ipForward: { runtime: boolean; persisted: boolean }
  dns: DnsState
}

export interface Settings {
  listen: string
  panelUrl: string
  githubMirror: string
  tlsMode: 'off' | 'self' | 'le'
  tlsDomain: string
  tlsEmail: string
}

export interface InstallParams {
  port: number
  proto: 'udp' | 'tcp'
  subnet: string
  enableIPv6: boolean
  subnet6?: string
  dnsMode: 'cloudflare' | 'google' | 'system' | 'self' | 'custom'
  dns1?: string
  dns2?: string
  publicAddr: string
}

export interface CheckResult {
  name: string
  ok: boolean
  detail: string
}

export interface PrecheckReport {
  ok: boolean
  checks: CheckResult[]
  suggestedAddr: string
  installed: boolean
}

export type StepStatus = 'pending' | 'running' | 'done' | 'failed' | 'skipped'

export interface StepInfo {
  id: string
  name: string
  status: StepStatus
}

export type JobState = 'running' | 'success' | 'rolled_back' | 'rollback_failed'

export interface JobSnapshot {
  id: string
  state: JobState
  error?: string
  steps: StepInfo[]
  startedAt: string
  finishedAt?: string
}

export interface InstallState {
  installed: boolean
  pendingJournal: boolean
  job?: JobSnapshot
}

export interface LogEvent {
  seq: number
  time: string
  level: string
  step?: string
  msg: string
}

export interface ClientCert {
  cn: string
  status: 'V' | 'R' | 'E'
  expiry: string
  revokedAt?: string
  serial: string
  owner: string
}

export interface ShareResp {
  url: string
  expiresAt: string
  ttlSec: number
}

export interface OnlineClient {
  cn: string
  realAddr: string
  virtualAddr: string
  bytesRecv: number
  bytesSent: number
  since: string
}

export interface NodeHealth {
  reachable: boolean
  version: string
  mode: string
  installed: boolean
  serviceActive: boolean
  online: number
  error?: string
}

export interface NodeRow {
  id: string
  name: string
  url: string
  insecureTLS: boolean
  addedAt: string
  health: NodeHealth
  grant?: NodeGrant | null
}

export interface JoinCodeResp {
  code: string
  expiresAt: string
  command: string
}

export interface BatchResult {
  id: string
  name: string
  ok: boolean
  status: number
  body: string
}

export interface VersionInfo {
  panel: string
  panelLatest: string
  openvpn: string
  openvpnUpgrade: string
  easyrsa: string
  easyrsaLatest: string
  checkedRemote: boolean
}
