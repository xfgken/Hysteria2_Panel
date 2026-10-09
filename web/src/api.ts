/**
 * Panel API 客户端。
 *
 * 统一处理：会话 Cookie（同源自动携带）、CSRF 请求头、错误结构解析。
 */

export type Issue = {
  severity: 'error' | 'warning'
  field: string
  message: string
}

export class ApiError extends Error {
  status: number
  issues?: Issue[]

  constructor(message: string, status: number, issues?: Issue[]) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.issues = issues
  }
}

let csrfToken = ''
let onUnauthorized: (() => void) | null = null

export function setCSRF(token: string) {
  csrfToken = token
}

export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (csrfToken && method !== 'GET' && method !== 'HEAD') {
    headers['X-CSRF-Token'] = csrfToken
  }

  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  const text = await res.text()
  let data: any = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }

  if (!res.ok) {
    if (res.status === 401 && onUnauthorized && !path.endsWith('/login')) {
      onUnauthorized()
    }
    const message = (data && (data.error || data.message)) || `请求失败（HTTP ${res.status}）`
    throw new ApiError(message, res.status, data?.issues)
  }

  return data as T
}

const get = <T,>(p: string) => request<T>('GET', p)
const post = <T,>(p: string, b?: unknown) => request<T>('POST', p, b)
const put = <T,>(p: string, b?: unknown) => request<T>('PUT', p, b)
const del = <T,>(p: string) => request<T>('DELETE', p)

/* ------------------------------- 类型 ------------------------------- */

export type SessionInfo = { csrfToken: string; username?: string; expiresAt?: string }

export type Dashboard = {
  totalUsers: number
  onlineUsers: number
  connections: number
  sessionTx: number
  sessionRx: number
  todayTx: number
  todayRx: number
  totalTx: number
  totalRx: number
  coreVersion: string
  serviceActive: boolean
  realtimeAvailable: boolean
  realtimeError?: string
  trafficSeries: { ts: number; tx: number; rx: number }[] | null
}

export type UserView = {
  id: number
  username: string
  enabled: boolean
  note: string
  createdAt: string
  online: number
  sessionTx: number
  sessionRx: number
  historical: number
  historicalTx: number
  historicalRx: number
}

/** 一次接入 / 断开记录。 */
export type UserEvent = {
  id: number
  userId: number
  kind: "connect" | "disconnect"
  ts: number
  online: number
}

export type UserList = {
  items: UserView[]
  total: number
  page: number
  pageSize: number
  realtime: boolean
  realtimeError?: string
}

export type UserDetail = {
  user: { id: number; username: string; enabled: boolean; note: string; createdAt: string; updatedAt: string }
  historical: { tx: number; rx: number }
  online?: number
  session?: { tx: number; rx: number }
  streams?: Stream[]
  realtimeError?: string
}

export type Stream = {
  state: string
  auth: string
  connection: number
  stream: number
  req_addr: string
  hooked_req_addr: string
  tx: number
  rx: number
  initial_at: string
  last_active_at: string
}

export type ServerConfigView = {
  config: Record<string, any>
  unknownKeys: string[] | null
  issues: Issue[]
  configPath: string
  serviceUnit: string
}

export type BackupItem = {
  id: number
  createdAt: string
  hy2Version: string
  panelVersion: string
  reason: string
  isCurrent: boolean
}

export type Settings = {
  serverHost: string
  subBaseURL: string
  mixedPort: number
  logHysteria: string
  logPanel: string
  logNginx: string
}

export type SystemStatus = {
  panel: { state: string; version: string; uptime: number; systemd: boolean; serviceUnit: string }
  hysteria: { active: boolean; version: string; error: string }
  nginx: { active: boolean; error: string }
}

export type UpdateCheck = {
  current: string
  latest: string
  upToDate: boolean
  assetName: string
  downloadUrl?: string
  publishedAt?: string
  notes?: string
}

export type UpdateResult = {
  from: string
  to: string
  backupPath: string
  restarted: boolean
  downloadUrl: string
}

export type CertificateInfo = {
  selfSigned: boolean
  subject: string
  issuer: string
  dnsNames: string[] | null
  notBefore: string
  notAfter: string
  daysLeft: number
  pin: string
}

export type CertificateView = {
  mode: 'none' | 'tls' | 'acme'
  path?: string
  domains?: string[]
  info?: CertificateInfo
  message?: string
  error?: string
}

export type SystemInfo = Record<string, any>

/* ------------------------------- 接口 ------------------------------- */

/** VPS 主机资源快照（/api/system/host）。 */
export type HostStats = {
  hostname: string
  kernel: string
  cpuCores: number
  cpuPercent: number
  load1: number
  load5: number
  load15: number
  memTotal: number
  memUsed: number
  memAvailable: number
  memPercent: number
  swapTotal: number
  swapUsed: number
  diskTotal: number
  diskUsed: number
  diskFree: number
  diskPercent: number
  uptime: number
  updatedAt: string
}

export const API = {
  health: () => get<Record<string, string>>('/api/health'),
  version: () => get<Record<string, string>>('/api/version'),

  login: (username: string, password: string) =>
    post<{ username: string; csrfToken: string }>('/api/login', { username, password }),
  logout: () => post<{ status: string }>('/api/logout'),
  session: () => get<SessionInfo>('/api/session'),
  changePassword: (currentPassword: string, newPassword: string) =>
    post<{ status: string }>('/api/session/password', { currentPassword, newPassword }),

  dashboard: () => get<Dashboard>('/api/dashboard'),

  users: {
    list: (q = '', page = 1, pageSize = 50) =>
      get<UserList>(`/api/users?q=${encodeURIComponent(q)}&page=${page}&pageSize=${pageSize}`),
    // 单用户模式：账号名由面板生成，只需提交密码与备注
    create: (password: string, note: string, suffix = '') =>
      post<{ user: UserView; password: string; syncError?: string }>('/api/users', {
        password,
        note,
        suffix,
      }),
    get: (id: number) => get<UserDetail>(`/api/users/${id}`),
    update: (id: number, patch: { password?: string; note?: string; enabled?: boolean; suffix?: string }) =>
      put<{ user: UserView; syncError?: string; rotatedToken?: boolean }>(`/api/users/${id}`, patch),
    remove: (id: number) =>
      del<{
        status: string
        syncError?: string
        /** 删除最后一个账号时，面板会自动补建一个，这里返回新账号与密码 */
        replacement?: UserView
        replacementPassword?: string
      }>(`/api/users/${id}`),
    kick: (id: number) => post<{ status: string }>(`/api/users/${id}/kick`),
    // uri 为官方多端口写法；uriSingle 为单端口兼容版（部分客户端不支持端口跳跃）
    uri: (id: number) => get<{ uri: string; uriSingle: string }>(`/api/users/${id}/uri`),
    subscription: (id: number) =>
      get<{ token: string; url: string; createdAt: string }>(`/api/users/${id}/subscription`),
    resetSubscription: (id: number) =>
      post<{ token: string; url: string; createdAt: string }>(`/api/users/${id}/subscription/reset`),
    regenerateName: (id: number) =>
      post<{ user: UserView; syncError: string }>(`/api/users/${id}/regenerate-name`),
    events: (id: number, limit = 200) =>
      get<{ userId: number; items: UserEvent[] }>(
        `/api/users/${id}/events?limit=${limit}`,
      ),
  },

  server: {
    config: () => get<ServerConfigView>('/api/server/config'),
    validate: (config: Record<string, any>) =>
      post<{ ok: boolean; issues: Issue[] }>('/api/server/config/validate', { config }),
    apply: (config: Record<string, any>) =>
      put<{ status: string; message: string; rotatedTokens?: number }>('/api/server/config', {
        config,
      }),
    raw: () => get<{ content: string; configPath: string }>('/api/server/config/raw'),
    certificate: () => get<CertificateView>('/api/server/certificate'),
    applyRaw: (content: string) =>
      put<{ status: string; message: string }>('/api/server/config/raw', { content }),
    syncUsers: () => post<{ status: string; message: string }>('/api/server/users/sync'),
    backups: () => get<{ items: BackupItem[] | null }>('/api/server/backups'),
    restore: (id: number) => post<{ status: string; message: string }>(`/api/server/backups/${id}/restore`),
    removeBackup: (id: number) => del<{ status: string }>(`/api/server/backups/${id}`),
  },

  settings: {
    get: () => get<Settings>('/api/settings'),
    update: (s: Settings) => put<{ status: string }>('/api/settings', s),
  },

  network: {
    online: () =>
      get<{ items: { username: string; userId: number; connections: number; known: boolean }[] | null }>(
        '/api/network/online',
      ),
    streams: () => get<{ items: Stream[]; total: number }>('/api/network/streams'),
    traffic: () =>
      get<{
        items: {
          username: string
          userId: number
          sessionTx: number
          sessionRx: number
          todayTx: number
          todayRx: number
          totalTx: number
          totalRx: number
        }[]
        series: { ts: number; tx: number; rx: number }[] | null
        realtimeError?: string
      }>('/api/network/traffic'),
  },

  logs: {
    fetch: (source: 'hysteria' | 'panel' | 'nginx', lines = 300, q = '', level = '') =>
      get<{ source: string; target: string; lines: number; content: string }>(
        `/api/logs/${source}?lines=${lines}&q=${encodeURIComponent(q)}&level=${encodeURIComponent(level)}`,
      ),
    clear: (source: 'hysteria' | 'panel' | 'nginx') =>
      del<{ status: string; message: string; target: string }>(`/api/logs/${source}`),
  },

  system: {
    status: () => get<SystemStatus>('/api/system/status'),
    info: () => get<SystemInfo>('/api/system/info'),
    host: () => get<HostStats>('/api/system/host'),
    service: (name: 'hysteria' | 'nginx', action: 'start' | 'stop' | 'restart' | 'status') =>
      post<{ output: string; active: boolean }>(`/api/system/service/${name}/${action}`),
    updateCheck: () => get<UpdateCheck>('/api/system/update/check'),
    updateApply: () => post<{ status: string; result: UpdateResult }>('/api/system/update/apply'),
    updateRollback: () => post<{ status: string; message: string }>('/api/system/update/rollback'),
  },
}

/* ---------------------------- 工具函数 ---------------------------- */

/** 人类可读的字节数。 */
export function formatBytes(n: number | undefined | null): string {
  if (!n || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

/**
 * 把 formatBytes 的结果拆成「数值 + 单位」两段。
 *
 * 用于大字排版：数值用主字号，单位用小一号的灰色字。
 */
export function splitBytes(n: number | undefined | null): [string, string] {
  const s = formatBytes(n)
  const i = s.lastIndexOf(' ')
  return i < 0 ? [s, ''] : [s.slice(0, i), s.slice(i + 1)]
}

/** 人类可读的时长（秒）。 */
export function formatDuration(seconds: number | undefined | null): string {
  if (!seconds || seconds <= 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  return `${m} 分`
}

/** 本地时间字符串。 */
export function formatTime(iso: string | undefined): string {
  if (!iso) return '—'
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${t.getFullYear()}-${p(t.getMonth() + 1)}-${p(t.getDate())} ${p(t.getHours())}:${p(t.getMinutes())}`
}

/** 备份原因的中文说明。 */
export function reasonLabel(reason: string): string {
  switch (reason) {
    case 'gui':
      return '界面修改'
    case 'apply':
      return '应用配置'
    case 'raw':
      return '原始配置修改'
    case 'sync-users':
      return '用户同步'
    case 'restore':
      return '恢复备份'
    case 'backup':
      return '手动备份'
    default:
      return reason || '—'
  }
}