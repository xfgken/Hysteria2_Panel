import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { API, ApiError, reasonLabel, formatTime } from '../api'
import type { BackupItem, CertificateView, Issue } from '../api'
import { Page } from '../components/Layout'
import {
  Badge,
  Card,
  ConfirmDialog,
  Empty,
  Field,
  IssueList,
  Loading,
  Select,
  Switch,
} from '../components/ui'
import { IconRefresh, IconSave, IconTrash, IconUpload } from '../icons'
import { useToast } from '../toast'

type Cfg = Record<string, any>

/* --------------------------- 对象路径读写工具 --------------------------- */

function getIn(obj: any, path: string[]): any {
  let cur = obj
  for (const k of path) {
    if (cur == null || typeof cur !== 'object') return undefined
    cur = cur[k]
  }
  return cur
}

function setIn(obj: Cfg, path: string[], value: any): Cfg {
  const clone: any = structuredClone(obj)
  let cur = clone
  for (let i = 0; i < path.length - 1; i++) {
    const k = path[i]
    if (cur[k] == null || typeof cur[k] !== 'object') cur[k] = {}
    cur = cur[k]
  }
  cur[path[path.length - 1]] = value
  return clone
}

function delIn(obj: Cfg, path: string[]): Cfg {
  const clone: any = structuredClone(obj)
  let cur = clone
  for (let i = 0; i < path.length - 1; i++) {
    const k = path[i]
    if (cur[k] == null || typeof cur[k] !== 'object') return clone
    cur = cur[k]
  }
  delete cur[path[path.length - 1]]
  return clone
}

/* ------------------------------- 字段组件 ------------------------------- */

function TextField({
  cfg,
  path,
  onChange,
  label,
  hint,
  placeholder,
  mono,
  action,
  type = 'text',
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
  hint?: string
  placeholder?: string
  mono?: boolean
  /** 输入框右侧的附加按钮（例如「随机」） */
  action?: ReactNode
  type?: string
}) {
  const value = getIn(cfg, path)
  const input = (
    <input
      className={mono ? 'input input--mono' : 'input'}
      type={type}
      value={value === undefined || value === null ? '' : String(value)}
      placeholder={placeholder}
      onChange={(e) => {
        const v = e.target.value
        onChange(v === '' ? delIn(cfg, path) : setIn(cfg, path, v))
      }}
    />
  )
  return (
    <Field label={label} hint={hint}>
      {action ? (
        <div className="input-row">
          {input}
          <span className="input-row__action">{action}</span>
        </div>
      ) : (
        input
      )}
    </Field>
  )
}

/** 生成随机密码：大小写字母 + 数字（去掉 I/l/0/O 等易混字符）。 */
function randomPassword(len = 16) {
  const alphabet = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  const buf = new Uint32Array(len)
  crypto.getRandomValues(buf)
  return Array.from(buf, (n) => alphabet[n % alphabet.length]).join('')
}

function NumberField({
  cfg,
  path,
  onChange,
  label,
  hint,
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
  hint?: string
}) {
  const value = getIn(cfg, path)
  return (
    <Field label={label} hint={hint}>
      <input
        className="input"
        type="number"
        value={value === undefined || value === null ? '' : String(value)}
        onChange={(e) => {
          const raw = e.target.value
          onChange(raw === '' ? delIn(cfg, path) : setIn(cfg, path, Number(raw)))
        }}
      />
    </Field>
  )
}

function SelectField({
  cfg,
  path,
  onChange,
  label,
  hint,
  options,
  allowEmpty,
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
  hint?: string
  options: { value: string; label: string }[]
  allowEmpty?: boolean
}) {
  const value = getIn(cfg, path)
  return (
    <Field label={label} hint={hint}>
      {/* 使用应用内自绘下拉，而非浏览器原生 <select> */}
      <Select
        value={value === undefined || value === null ? '' : String(value)}
        options={options}
        allowEmpty={allowEmpty}
        placeholder="（未设置）"
        onChange={(v) => onChange(v === '' ? delIn(cfg, path) : setIn(cfg, path, v))}
      />
    </Field>
  )
}

function SwitchField({
  cfg,
  path,
  onChange,
  label,
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
}) {
  const value = !!getIn(cfg, path)
  return (
    <Field label={label}>
      <Switch on={value} onChange={(v) => onChange(v ? setIn(cfg, path, true) : delIn(cfg, path))} />
    </Field>
  )
}

function TextareaField({
  cfg,
  path,
  onChange,
  label,
  hint,
  splitLines,
  placeholder,
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
  hint?: string
  /** true 表示按行拆分为字符串数组（如 acme.domains、acl.inline） */
  splitLines?: boolean
  placeholder?: string
}) {
  const raw = getIn(cfg, path)
  const value = Array.isArray(raw) ? raw.join('\n') : raw === undefined || raw === null ? '' : String(raw)
  return (
    <Field label={label} hint={hint} full>
      <textarea
        className="textarea"
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          const text = e.target.value
          if (text === '') {
            onChange(delIn(cfg, path))
            return
          }
          onChange(setIn(cfg, path, splitLines ? text.split('\n').filter((l) => l.trim() !== '') : text))
        }}
      />
    </Field>
  )
}

/* ------------------------------- 键值编辑器 ------------------------------- */

/** 以「每行 key=value」的形式编辑一个 map（如 acme.dns.config）。 */
function KvField({
  cfg,
  path,
  onChange,
  label,
  placeholder,
}: {
  cfg: Cfg
  path: string[]
  onChange: (c: Cfg) => void
  label: string
  placeholder?: string
}) {
  const raw = getIn(cfg, path)
  const value =
    raw && typeof raw === 'object' && !Array.isArray(raw)
      ? Object.entries(raw)
          .map(([k, v]) => `${k}=${String(v)}`)
          .join('\n')
      : ''

  return (
    <Field label={label} full>
      <textarea
        className="textarea"
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          const text = e.target.value
          if (text.trim() === '') {
            onChange(delIn(cfg, path))
            return
          }
          const map: Record<string, string> = {}
          for (const line of text.split('\n')) {
            const idx = line.indexOf('=')
            if (idx <= 0) continue
            const k = line.slice(0, idx).trim()
            const v = line.slice(idx + 1).trim()
            if (k) map[k] = v
          }
          onChange(Object.keys(map).length ? setIn(cfg, path, map) : delIn(cfg, path))
        }}
      />
    </Field>
  )
}

/* ------------------------------- 主页面 ------------------------------- */

type TabKey = 'basic' | 'tls' | 'transport' | 'routing' | 'advanced'

/** 服务器配置（开发文档 24~51 节）。 */
export default function Server() {
  const [cfg, setCfg] = useState<Cfg | null>(null)
  const [original, setOriginal] = useState<Cfg>({})
  const [issues, setIssues] = useState<Issue[]>([])
  const [unknownKeys, setUnknownKeys] = useState<string[]>([])
  const [configPath, setConfigPath] = useState('')
  const [tab, setTab] = useState<TabKey>('basic')
  const [busy, setBusy] = useState(false)
  const [rawOpen, setRawOpen] = useState(false)
  const [restoreId, setRestoreId] = useState<number | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const { toast } = useToast()

  const load = useCallback(async () => {
    setBusy(true)
    try {
      const res = await API.server.config()
      setCfg(res.config || {})
      setOriginal(res.config || {})
      setIssues(res.issues || [])
      setUnknownKeys(res.unknownKeys || [])
      setConfigPath(res.configPath)
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '读取配置失败', 'err')
    } finally {
      setBusy(false)
    }
  }, [toast])

  useEffect(() => {
    void load()
  }, [load])

  const dirty = cfg && JSON.stringify(cfg) !== JSON.stringify(original)

  const validate = async () => {
    if (!cfg) return
    setBusy(true)
    try {
      const res = await API.server.validate(cfg)
      setIssues(res.issues || [])
      toast(res.ok ? '检查通过' : '检查未通过，请查看下方提示', res.ok ? 'ok' : 'err')
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '检查失败', 'err')
    } finally {
      setBusy(false)
    }
  }

  const apply = async () => {
    if (!cfg) return
    setBusy(true)
    try {
      const res = await API.server.apply(cfg)
      toast(
        res.rotatedTokens
          ? `配置已应用，已为 ${res.rotatedTokens} 个账号重新生成订阅地址`
          : '配置已应用',
        'ok',
      )
      await load()
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.issues) setIssues(err.issues)
        toast(err.message, 'err')
      } else {
        toast('应用失败', 'err')
      }
    } finally {
      setBusy(false)
    }
  }

  const tlsMode = cfg?.tls ? 'tls' : cfg?.acme ? 'acme' : 'none'

  const setTlsMode = (mode: string) => {
    if (!cfg) return
    let next = delIn(delIn(cfg, ['tls']), ['acme'])
    if (mode === 'tls') next = setIn(next, ['tls'], getIn(cfg, ['tls']) || { cert: '', key: '' })
    if (mode === 'acme')
      next = setIn(next, ['acme'], getIn(cfg, ['acme']) || { domains: [], email: '', type: 'http', http: { altPort: 80 } })
    setCfg(next)
  }

  return (
    <Page
      title="Hysteria2 配置"
      subtitle={configPath ? `配置文件：${configPath}` : '官方 Hysteria 2 服务端配置'}
    >
      {/* 操作按钮放在内容区顶部（顶栏下方） */}
      <div className="fab-actions">
        <button
          type="button"
          className="fab fab--icon"
          title="重新载入配置"
          onClick={() => void load()}
          disabled={busy}
        >
          <IconRefresh size={18} />
        </button>
        <button type="button" className="fab" onClick={validate} disabled={busy || !cfg}>
          检查配置
        </button>
        <button type="button" className="fab fab--primary" onClick={apply} disabled={busy || !cfg}>
          {busy ? <span className="spinner" /> : <IconSave size={17} />}
          保存并应用
        </button>
      </div>

      {unknownKeys.length > 0 && (
        <IssueList
          issues={[
            {
              severity: 'warning',
              field: '未知字段',
              message: `检测到 Panel 未建模的官方字段：${unknownKeys.join('、')}。这些字段不会被界面覆盖，请通过「原始配置」维护。`,
            },
          ]}
        />
      )}

      {issues.length > 0 && <IssueList issues={issues} />}

      <div className="tabs">
        {(
          [
            ['basic', '基础'],
            ['tls', 'TLS 与认证'],
            ['transport', '传输与混淆'],
            ['routing', '路由与出站'],
            ['advanced', '高级与备份'],
          ] as [TabKey, string][]
        ).map(([k, label]) => (
          <button
            key={k}
            type="button"
            className={tab === k ? 'tab tab--active' : 'tab'}
            onClick={() => setTab(k)}
          >
            {label}
          </button>
        ))}
      </div>

      {!cfg ? (
        <Card>
          <Loading />
        </Card>
      ) : (
        <>
          {tab === 'basic' && (
            <Card title="基础">
              <div className="form-grid">
                <TextField
                  cfg={cfg}
                  onChange={setCfg}
                  path={['listen']}
                  label="监听地址"
                  hint="如 :443；端口跳跃可写 :20000-50000；Realms 模式可写 realm://…"
                  mono
                />
                <TextField
                  cfg={cfg}
                  onChange={setCfg}
                  path={['udpIdleTimeout']}
                  label="UDP 空闲超时"
                  hint="例如 60s"
                />
                <SwitchField cfg={cfg} onChange={setCfg} path={['disableUDP']} label="禁用 UDP 转发" />
                <SwitchField cfg={cfg} onChange={setCfg} path={['speedTest']} label="启用内置测速服务" />
                <SwitchField
                  cfg={cfg}
                  onChange={setCfg}
                  path={['ignoreClientBandwidth']}
                  label="忽略客户端带宽提示"
                />
              </div>
              <p className="field__hint" style={{ marginTop: 12 }}>
                面板依赖下面的全局流量统计 API 读取实时数据，请在「高级与备份」中确认已启用。
              </p>
            </Card>
          )}

          {tab === 'tls' && (
            <>
              <Card title="TLS / ACME">
                <div className="form-grid">
                  <SelectField
                    cfg={{ mode: tlsMode }}
                    onChange={(c) => setTlsMode((c as any).mode)}
                    path={['mode']}
                    label="证书来源"
                    hint="tls 与 acme 二选一，不能同时配置"
                    options={[
                      { value: 'none', label: '未配置' },
                      { value: 'tls', label: '静态证书（tls）' },
                      { value: 'acme', label: '自动申请（acme）' },
                    ]}
                  />
                </div>

                {tlsMode === 'tls' && (
                  <div className="form-grid" style={{ marginTop: 14 }}>
                    <TextField cfg={cfg} onChange={setCfg} path={['tls', 'cert']} label="证书文件" mono />
                    <TextField cfg={cfg} onChange={setCfg} path={['tls', 'key']} label="私钥文件" mono />
                    <SelectField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['tls', 'sniGuard']}
                      label="SNI 校验"
                      allowEmpty
                      options={[
                        { value: 'dns-san', label: 'dns-san（默认）' },
                        { value: 'strict', label: 'strict' },
                        { value: 'disable', label: 'disable' },
                      ]}
                    />
                    <TextField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['tls', 'clientCA']}
                      label="客户端 CA（mTLS）"
                      mono
                    />
                  </div>
                )}

                {tlsMode === 'acme' && (
                  <div className="form-grid" style={{ marginTop: 14 }}>
                    <TextareaField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['acme', 'domains']}
                      label="域名（每行一个）"
                      splitLines
                      placeholder={'example.com\n*.example.com'}
                    />
                    <TextField cfg={cfg} onChange={setCfg} path={['acme', 'email']} label="邮箱" />
                    <SelectField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['acme', 'ca']}
                      label="CA"
                      allowEmpty
                      options={[
                        { value: 'letsencrypt', label: "Let's Encrypt（默认）" },
                        { value: 'zerossl', label: 'ZeroSSL' },
                      ]}
                    />
                    <SelectField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['acme', 'type']}
                      label="挑战方式"
                      options={[
                        { value: 'http', label: 'HTTP-01' },
                        { value: 'tls', label: 'TLS-ALPN-01' },
                        { value: 'dns', label: 'DNS-01' },
                      ]}
                    />
                    {getIn(cfg, ['acme', 'type']) === 'http' && (
                      <NumberField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['acme', 'http', 'altPort']}
                        label="HTTP 备用端口"
                      />
                    )}
                    {getIn(cfg, ['acme', 'type']) === 'tls' && (
                      <NumberField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['acme', 'tls', 'altPort']}
                        label="TLS 备用端口"
                      />
                    )}
                    {getIn(cfg, ['acme', 'type']) === 'dns' && (
                      <>
                        <TextField
                          cfg={cfg}
                          onChange={setCfg}
                          path={['acme', 'dns', 'name']}
                          label="DNS 服务商"
                          hint="cloudflare / duckdns / gandi / godaddy / namecheap / njalla / porkbun / vultr"
                          mono
                        />
                        <KvField
                          cfg={cfg}
                          onChange={setCfg}
                          path={['acme', 'dns', 'config']}
                          label="服务商配置（每行 key=value）"
                          placeholder="cloudflare_api_token=xxx"
                        />
                      </>
                    )}
                    <TextField cfg={cfg} onChange={setCfg} path={['acme', 'listenHost']} label="挑战监听地址" />
                  </div>
                )}

                <CertificateStatus reloadKey={tlsMode} />
              </Card>

              <Card title="认证">
                <div className="form-grid">
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['auth', 'type']}
                    label="认证方式"
                    options={[
                      { value: 'userpass', label: 'userpass（推荐 · 账号由面板管理）' },
                      { value: 'password', label: 'password（单一固定密码）' },
                      { value: 'http', label: 'http（外部后端）' },
                      { value: 'command', label: 'command（外部命令）' },
                    ]}
                  />
                  {getIn(cfg, ['auth', 'type']) === 'password' && (
                    <TextField cfg={cfg} onChange={setCfg} path={['auth', 'password']} label="密码" mono />
                  )}
                  {getIn(cfg, ['auth', 'type']) === 'http' && (
                    <>
                      <TextField cfg={cfg} onChange={setCfg} path={['auth', 'http', 'url']} label="认证后端 URL" mono />
                      <SwitchField cfg={cfg} onChange={setCfg} path={['auth', 'http', 'insecure']} label="跳过 TLS 校验" />
                    </>
                  )}
                  {getIn(cfg, ['auth', 'type']) === 'command' && (
                    <TextField cfg={cfg} onChange={setCfg} path={['auth', 'command']} label="命令路径" mono />
                  )}
                </div>
                {getIn(cfg, ['auth', 'type']) === 'userpass' && (
                  <p className="field__hint" style={{ marginTop: 12 }}>
                    账号由面板管理并自动同步为这里的 userpass 列表，无需手动填写。
                    新增、改密码、删除都在「账号订阅」页里操作。
                  </p>
                )}
              </Card>

              <Card title="ECH 加密客户端问候">
                <div className="form-grid">
                  <TextField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['ech', 'keyPath']}
                    label="ECH 密钥文件"
                    hint="留空表示不启用"
                    mono
                  />
                </div>
              </Card>
            </>
          )}

          {tab === 'transport' && (
            <>
              <Card title="混淆">
                <div className="form-grid">
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['obfs', 'type']}
                    label="混淆类型"
                    allowEmpty
                    options={[
                      { value: 'salamander', label: 'Salamander' },
                      { value: 'gecko', label: 'Gecko（Experimental）' },
                    ]}
                  />
                  {getIn(cfg, ['obfs', 'type']) === 'salamander' && (
                    <TextField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['obfs', 'salamander', 'password']}
                      label="混淆密码"
                      mono
                      action={
                        <button
                          type="button"
                          className="btn btn--sm"
                          onClick={() =>
                            setCfg(setIn(cfg, ['obfs', 'salamander', 'password'], randomPassword(16)))
                          }
                        >
                          <IconRefresh size={14} />
                          随机
                        </button>
                      }
                    />
                  )}
                  {getIn(cfg, ['obfs', 'type']) === 'gecko' && (
                    <>
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['obfs', 'gecko', 'password']}
                        label="混淆密码"
                        mono
                        action={
                          <button
                            type="button"
                            className="btn btn--sm"
                            onClick={() =>
                              setCfg(setIn(cfg, ['obfs', 'gecko', 'password'], randomPassword(16)))
                            }
                          >
                            <IconRefresh size={14} />
                            随机
                          </button>
                        }
                      />
                      <NumberField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['obfs', 'gecko', 'minPacketSize']}
                        label="最小分片大小"
                      />
                      <NumberField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['obfs', 'gecko', 'maxPacketSize']}
                        label="最大分片大小"
                      />
                    </>
                  )}
                </div>
                <p className="field__hint" style={{ marginTop: 12 }}>
                  启用混淆会使服务端不再兼容标准 QUIC / HTTP/3 连接。
                </p>
              </Card>

              <Card title="拥塞与带宽">
                <div className="form-grid">
                  <TextField cfg={cfg} onChange={setCfg} path={['bandwidth', 'up']} label="上行限速" hint="如 1 gbps，留空不限速" />
                  <TextField cfg={cfg} onChange={setCfg} path={['bandwidth', 'down']} label="下行限速" hint="如 1 gbps，留空不限速" />
                  <SwitchField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['bandwidth', 'disableLossCompensation']}
                    label="关闭丢包补偿"
                  />
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['congestion', 'type']}
                    label="拥塞控制"
                    options={[
                      { value: 'bbr', label: 'BBR（默认）' },
                      { value: 'reno', label: 'Reno' },
                    ]}
                  />
                  {getIn(cfg, ['congestion', 'type']) === 'bbr' && (
                    <SelectField
                      cfg={cfg}
                      onChange={setCfg}
                      path={['congestion', 'bbrProfile']}
                      label="BBR 模式"
                      options={[
                        { value: 'standard', label: 'standard' },
                        { value: 'conservative', label: 'conservative' },
                        { value: 'aggressive', label: 'aggressive' },
                      ]}
                    />
                  )}
                </div>
                <p className="field__hint" style={{ marginTop: 12 }}>
                  注意：Brutal 不是拥塞控制类型，而是由客户端上报带宽触发；服务端仅可通过带宽字段限速。
                </p>
              </Card>

              <Card title="QUIC 参数">
                <div className="form-grid">
                  <NumberField cfg={cfg} onChange={setCfg} path={['quic', 'initStreamReceiveWindow']} label="初始流接收窗口" hint="默认 8388608" />
                  <NumberField cfg={cfg} onChange={setCfg} path={['quic', 'maxStreamReceiveWindow']} label="最大流接收窗口" hint="默认 8388608" />
                  <NumberField cfg={cfg} onChange={setCfg} path={['quic', 'initConnReceiveWindow']} label="初始连接接收窗口" hint="默认 20971520" />
                  <NumberField cfg={cfg} onChange={setCfg} path={['quic', 'maxConnReceiveWindow']} label="最大连接接收窗口" hint="默认 20971520" />
                  <TextField cfg={cfg} onChange={setCfg} path={['quic', 'maxIdleTimeout']} label="最大空闲超时" hint="默认 30s" />
                  <NumberField cfg={cfg} onChange={setCfg} path={['quic', 'maxIncomingStreams']} label="最大并发入站流" hint="默认 1024" />
                  <SwitchField cfg={cfg} onChange={setCfg} path={['quic', 'disablePathMTUDiscovery']} label="禁用路径 MTU 探测" />
                  <SwitchField cfg={cfg} onChange={setCfg} path={['quic', 'disableStatelessReset']} label="禁用无状态重置" />
                </div>
              </Card>
            </>
          )}

          {tab === 'routing' && (
            <>
              <Card title="DNS 解析器（resolver）">
                <div className="form-grid">
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['resolver', 'type']}
                    label="类型"
                    allowEmpty
                    options={[
                      { value: 'udp', label: 'udp' },
                      { value: 'tcp', label: 'tcp' },
                      { value: 'tls', label: 'tls' },
                      { value: 'https', label: 'https' },
                    ]}
                  />
                  {['udp', 'tcp'].includes(getIn(cfg, ['resolver', 'type'])) && (
                    <>
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'addr']}
                        label="地址"
                        hint="如 8.8.8.8:53"
                        mono
                      />
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'timeout']}
                        label="超时"
                        hint="如 4s"
                      />
                    </>
                  )}
                  {['tls', 'https'].includes(getIn(cfg, ['resolver', 'type'])) && (
                    <>
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'addr']}
                        label="地址"
                        mono
                      />
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'timeout']}
                        label="超时"
                      />
                      <TextField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'sni']}
                        label="SNI"
                        mono
                      />
                      <SwitchField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['resolver', getIn(cfg, ['resolver', 'type']), 'insecure']}
                        label="跳过 TLS 校验"
                      />
                    </>
                  )}
                </div>
              </Card>

              <Card title="协议嗅探（sniff）">
                <div className="form-grid">
                  <SwitchField cfg={cfg} onChange={setCfg} path={['sniff', 'enable']} label="启用嗅探" />
                  <TextField cfg={cfg} onChange={setCfg} path={['sniff', 'timeout']} label="超时" hint="默认 2s" />
                  <SwitchField cfg={cfg} onChange={setCfg} path={['sniff', 'rewriteDomain']} label="重写为目标域名" />
                  <TextField cfg={cfg} onChange={setCfg} path={['sniff', 'tcpPorts']} label="TCP 端口" hint="如 80,443,8000-9000" mono />
                  <TextField cfg={cfg} onChange={setCfg} path={['sniff', 'udpPorts']} label="UDP 端口" hint="留空表示全部" mono />
                </div>
              </Card>

              <Card title="访问控制（acl）">
                <div className="form-grid">
                  <TextField cfg={cfg} onChange={setCfg} path={['acl', 'file']} label="ACL 文件路径" hint="与内联规则二选一" mono />
                  <TextField cfg={cfg} onChange={setCfg} path={['acl', 'geoip']} label="GeoIP 数据库" mono />
                  <TextField cfg={cfg} onChange={setCfg} path={['acl', 'geosite']} label="GeoSite 数据库" mono />
                  <TextField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['acl', 'geoUpdateInterval']}
                    label="数据库刷新间隔"
                    hint="默认 168h"
                  />
                </div>
                <div style={{ marginTop: 14 }}>
                  <TextareaField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['acl', 'inline']}
                    label="内联规则（每行一条）"
                    splitLines
                    placeholder={'direct(all, geoip:cn)\nproxy(all, geosite:netflix)'}
                  />
                </div>
              </Card>

              <OutboundsEditor cfg={cfg} onChange={setCfg} />

              <Card title="伪装（masquerade）">
                <div className="form-grid">
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['masquerade', 'type']}
                    label="类型"
                    allowEmpty
                    options={[
                      { value: 'file', label: 'file（静态文件）' },
                      { value: 'proxy', label: 'proxy（反向代理）' },
                      { value: 'string', label: 'string（固定内容）' },
                    ]}
                  />
                  {getIn(cfg, ['masquerade', 'type']) === 'file' && (
                    <TextField cfg={cfg} onChange={setCfg} path={['masquerade', 'file', 'dir']} label="目录" mono />
                  )}
                  {getIn(cfg, ['masquerade', 'type']) === 'proxy' && (
                    <>
                      <TextField cfg={cfg} onChange={setCfg} path={['masquerade', 'proxy', 'url']} label="目标 URL" mono />
                      <SwitchField cfg={cfg} onChange={setCfg} path={['masquerade', 'proxy', 'rewriteHost']} label="重写 Host" />
                      <SwitchField cfg={cfg} onChange={setCfg} path={['masquerade', 'proxy', 'insecure']} label="跳过 TLS 校验" />
                      <SwitchField cfg={cfg} onChange={setCfg} path={['masquerade', 'proxy', 'xForwarded']} label="附加 X-Forwarded" />
                    </>
                  )}
                  {getIn(cfg, ['masquerade', 'type']) === 'string' && (
                    <>
                      <TextField cfg={cfg} onChange={setCfg} path={['masquerade', 'string', 'content']} label="返回内容" />
                      <NumberField
                        cfg={cfg}
                        onChange={setCfg}
                        path={['masquerade', 'string', 'statusCode']}
                        label="状态码"
                      />
                    </>
                  )}
                  <TextField cfg={cfg} onChange={setCfg} path={['masquerade', 'listenHTTP']} label="HTTP 伪装监听" hint="如 :80" mono />
                  <TextField cfg={cfg} onChange={setCfg} path={['masquerade', 'listenHTTPS']} label="HTTPS 伪装监听" hint="如 :443" mono />
                  <SwitchField cfg={cfg} onChange={setCfg} path={['masquerade', 'forceHTTPS']} label="强制跳转 HTTPS" />
                </div>
              </Card>
            </>
          )}

          {tab === 'advanced' && (
            <>
              <Card
                title="流量统计 API（trafficStats）"
                extra={<Badge kind="ok">面板依赖</Badge>}
              >
                <div className="form-grid">
                  <TextField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['trafficStats', 'listen']}
                    label="监听地址"
                    hint="建议仅监听 127.0.0.1"
                    mono
                  />
                  <TextField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['trafficStats', 'secret']}
                    label="访问密钥"
                    hint="强烈建议设置，否则同主机进程可读取或踢人"
                    mono
                  />
                </div>
              </Card>

              <Card title="伪 TCP（mimic，仅 Linux）">
                <div className="form-grid">
                  <SwitchField cfg={cfg} onChange={setCfg} path={['mimic', 'enabled']} label="启用" />
                  <TextField cfg={cfg} onChange={setCfg} path={['mimic', 'interface']} label="网卡" mono />
                  <SelectField
                    cfg={cfg}
                    onChange={setCfg}
                    path={['mimic', 'xdpMode']}
                    label="XDP 模式"
                    allowEmpty
                    options={[
                      { value: 'skb', label: 'skb' },
                      { value: 'native', label: 'native' },
                    ]}
                  />
                  <TextField cfg={cfg} onChange={setCfg} path={['mimic', 'path']} label="mimic 可执行文件" mono />
                </div>
                <p className="field__hint" style={{ marginTop: 12 }}>
                  需要系统安装 mimic 及其内核模块，且 Hysteria 以 root 运行；开启后所有客户端必须使用相同设置。
                </p>
              </Card>

              <Card
                title="原始配置"
                extra={
                  <button type="button" className="btn btn--sm" onClick={() => setRawOpen(true)}>
                    编辑原始配置
                  </button>
                }
              >
                <p className="muted">
                  当官方新增参数而界面尚未跟进时，可通过原始配置直接编辑。原始配置同样要等官方 Core 试跑通过后才会写入。
                </p>
              </Card>

              <BackupsCard
                onRestore={(id) => setRestoreId(id)}
                onDelete={(id) => setDeleteId(id)}
                refreshKey={busy ? 0 : 1}
              />
            </>
          )}
        </>
      )}

      {rawOpen && <RawConfigDialog onClose={() => setRawOpen(false)} onApplied={() => void load()} />}

      {restoreId !== null && (
        <ConfirmDialog
          title="恢复备份"
          danger
          message="恢复会用该备份覆盖当前正式配置并重启服务，且当前配置会先被自动备份。确认继续？"
          confirmText="恢复"
          busy={busy}
          onCancel={() => setRestoreId(null)}
          onConfirm={async () => {
            setBusy(true)
            try {
              await API.server.restore(restoreId)
              toast('已恢复该备份', 'ok')
              setRestoreId(null)
              await load()
            } catch (err) {
              toast(err instanceof ApiError ? err.message : '恢复失败', 'err')
            } finally {
              setBusy(false)
            }
          }}
        />
      )}

      {deleteId !== null && (
        <ConfirmDialog
          title="删除备份"
          danger
          message="确认删除该配置备份？删除后无法恢复。"
          confirmText="删除"
          busy={busy}
          onCancel={() => setDeleteId(null)}
          onConfirm={async () => {
            setBusy(true)
            try {
              await API.server.removeBackup(deleteId)
              toast('备份已删除', 'ok')
              setDeleteId(null)
              await load()
            } catch (err) {
              toast(err instanceof ApiError ? err.message : '删除失败', 'err')
            } finally {
              setBusy(false)
            }
          }}
        />
      )}
    </Page>
  )
}

/* ----------------------------- 出站编辑器 ----------------------------- */

function OutboundsEditor({ cfg, onChange }: { cfg: Cfg; onChange: (c: Cfg) => void }) {
  const list: any[] = Array.isArray(getIn(cfg, ['outbounds'])) ? getIn(cfg, ['outbounds']) : []

  const remove = (idx: number) => {
    const next = list.filter((_, i) => i !== idx)
    onChange(next.length ? setIn(cfg, ['outbounds'], next) : delIn(cfg, ['outbounds']))
  }

  const add = () => {
    const next = [...list, { name: `outbound_${list.length + 1}`, type: 'direct' }]
    onChange(setIn(cfg, ['outbounds'], next))
  }

  return (
    <Card
      title="出站（outbounds）"
      extra={
        <button type="button" className="btn btn--sm" onClick={add}>
          添加出站
        </button>
      }
    >
      {list.length === 0 ? (
        <Empty title="未配置出站" hint="未使用 ACL 时所有流量都走第一个出站；留空表示使用默认直连" />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          {list.map((ob, i) => (
            <div key={i} style={{ borderTop: i === 0 ? 'none' : '1px solid var(--line-color)', paddingTop: i === 0 ? 0 : 14 }}>
              <div className="form-grid">
                <TextField
                  cfg={cfg}
                  onChange={onChange}
                  path={['outbounds', String(i), 'name']}
                  label="名称"
                  mono
                />
                <SelectField
                  cfg={cfg}
                  onChange={onChange}
                  path={['outbounds', String(i), 'type']}
                  label="类型"
                  options={[
                    { value: 'direct', label: 'direct' },
                    { value: 'socks5', label: 'socks5' },
                    { value: 'http', label: 'http' },
                  ]}
                />
                {ob.type === 'socks5' && (
                  <>
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'socks5', 'addr']} label="地址" mono />
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'socks5', 'username']} label="用户名" />
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'socks5', 'password']} label="密码" mono />
                  </>
                )}
                {ob.type === 'http' && (
                  <>
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'http', 'url']} label="URL" mono />
                    <SwitchField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'http', 'insecure']} label="跳过 TLS 校验" />
                  </>
                )}
                {ob.type === 'direct' && (
                  <>
                    <SelectField
                      cfg={cfg}
                      onChange={onChange}
                      path={['outbounds', String(i), 'direct', 'mode']}
                      label="地址族模式"
                      allowEmpty
                      options={[
                        { value: 'auto', label: 'auto' },
                        { value: '64', label: '64（优先 IPv6）' },
                        { value: '46', label: '46（优先 IPv4）' },
                        { value: '6', label: '6（仅 IPv6）' },
                        { value: '4', label: '4（仅 IPv4）' },
                      ]}
                    />
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'direct', 'bindDevice']} label="绑定网卡" mono />
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'direct', 'bindIPv4']} label="绑定 IPv4" mono />
                    <TextField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'direct', 'bindIPv6']} label="绑定 IPv6" mono />
                    <SwitchField cfg={cfg} onChange={onChange} path={['outbounds', String(i), 'direct', 'fastOpen']} label="TCP Fast Open" />
                  </>
                )}
              </div>
              <div className="row" style={{ marginTop: 10, justifyContent: 'flex-end' }}>
                <button type="button" className="btn btn--sm btn--danger" onClick={() => remove(i)}>
                  <IconTrash size={13} />
                  删除该出站
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

/* ------------------------------- 备份列表 ------------------------------- */

function BackupsCard({
  onRestore,
  onDelete,
  refreshKey,
}: {
  onRestore: (id: number) => void
  onDelete: (id: number) => void
  refreshKey: number
}) {
  const [items, setItems] = useState<BackupItem[] | null>(null)
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await API.server.backups()
      setItems(res.items || [])
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '读取备份失败', 'err')
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  return (
    <Card
      title="配置备份"
      extra={
        <button type="button" className="btn btn--sm" onClick={() => void load()}>
          <IconRefresh size={13} />
          刷新
        </button>
      }
      flush
    >
      {loading ? (
        <Loading />
      ) : !items || items.length === 0 ? (
        <Empty title="暂无配置备份" hint="每次修改正式配置前都会自动备份" />
      ) : (
        <div className="table-wrap" style={{ border: 'none', borderRadius: 0 }}>
          <table className="table">
            <thead>
              <tr>
                <th>时间</th>
                <th>原因</th>
                <th>HY2 版本</th>
                <th>Panel 版本</th>
                <th className="actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((b) => (
                <tr key={b.id}>
                  <td className="muted">{formatTime(b.createdAt)}</td>
                  <td>
                    {reasonLabel(b.reason)} {b.isCurrent && <Badge kind="ok">最新</Badge>}
                  </td>
                  <td className="mono">{b.hy2Version || '—'}</td>
                  <td className="mono">{b.panelVersion || '—'}</td>
                  <td className="actions">
                    <div className="row row--tight" style={{ justifyContent: 'flex-end' }}>
                      <button type="button" className="btn btn--sm" onClick={() => onRestore(b.id)}>
                        <IconUpload size={13} />
                        恢复
                      </button>
                      <button type="button" className="btn btn--sm btn--ghost" onClick={() => onDelete(b.id)}>
                        <IconTrash size={13} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}
/* ----------------------------- 原始配置弹窗 ----------------------------- */

function RawConfigDialog({ onClose, onApplied }: { onClose: () => void; onApplied: () => void }) {
  const [content, setContent] = useState('')
  const [busy, setBusy] = useState(false)
  const [issues, setIssues] = useState<Issue[]>([])
  const { toast } = useToast()

  useEffect(() => {
    void (async () => {
      try {
        const res = await API.server.raw()
        setContent(res.content)
      } catch (err) {
        toast(err instanceof ApiError ? err.message : '读取原始配置失败', 'err')
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const doApply = async () => {
    setBusy(true)
    setIssues([])
    try {
      await API.server.applyRaw(content)
      toast('原始配置已应用', 'ok')
      onApplied()
      onClose()
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.issues) setIssues(err.issues)
        toast(err.message, 'err')
      } else {
        toast('应用失败', 'err')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog dialog--wide" role="dialog" aria-modal="true">
        <header className="dialog__head">
          <h3>原始配置（config.yaml）</h3>
          <button type="button" className="btn btn--ghost btn--icon dialog__close" onClick={onClose} aria-label="关闭">
            关闭
          </button>
        </header>
        <div className="dialog__body">
          <IssueList issues={issues} />
          <textarea
            className="textarea"
            style={{ minHeight: '46vh' }}
            value={content}
            spellCheck={false}
            onChange={(e) => setContent(e.target.value)}
          />
          <p className="field__hint">
            提交后 Panel 会先让官方 Core 试跑一遍；不通过不会修改正式配置，也不会重启服务。
            应用成功后会自动为所有账号生成新的订阅地址。
          </p>
        </div>
        <footer className="dialog__foot">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button type="button" className="btn btn--primary" onClick={doApply} disabled={busy}>
            {busy && <span className="spinner" />}
            检查并应用
          </button>
        </footer>
      </div>
    </div>
  )
}

/**
 * 证书状态：展示当前配置实际使用的证书信息。
 *
 * 面板只服务一个账号，证书是链路上最容易出问题的一环
 * （自签名证书若没告知客户端跳过校验会直接连不上），所以直接显示真实状态。
 */
function CertificateStatus({ reloadKey }: { reloadKey: string }) {
  const [data, setData] = useState<CertificateView | null>(null)

  const load = useCallback(async () => {
    try {
      setData(await API.server.certificate())
    } catch {
      setData(null)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load, reloadKey])

  if (!data) return null

  if (data.mode === 'none') {
    return (
      <div className="issue" style={{ marginTop: 14 }}>
        <span>{data.message}</span>
      </div>
    )
  }

  return (
    <div className="cert-box">
      <div className="row row--tight" style={{ marginBottom: 8 }}>
        <span className="muted">当前证书</span>
        {data.mode === 'acme' ? (
          <Badge kind="ok">ACME 自动签发</Badge>
        ) : data.info ? (
          <>
            <Badge kind={data.info.selfSigned ? 'warn' : 'ok'}>
              {data.info.selfSigned ? '自签名' : '公信证书'}
            </Badge>
            <Badge kind={data.info.daysLeft < 0 ? 'danger' : data.info.daysLeft < 15 ? 'warn' : 'ok'}>
              {data.info.daysLeft < 0 ? '已过期' : `${data.info.daysLeft} 天后过期`}
            </Badge>
          </>
        ) : null}
      </div>

      {data.mode === 'acme' && <p className="field__hint">{data.message}</p>}

      {data.mode === 'tls' && data.error && (
        <div className="issue issue--warning" style={{ marginTop: 4 }}>
          <span>{data.error}</span>
        </div>
      )}

      {data.mode === 'tls' && data.info && (
        <>
          <dl className="kv">
            <dt>证书文件</dt>
            <dd className="mono">{data.path}</dd>
            <dt>主体</dt>
            <dd className="mono">{data.info.subject}</dd>
            <dt>域名（SAN）</dt>
            <dd className="mono">{(data.info.dnsNames || []).join(', ') || '—'}</dd>
            <dt>有效期至</dt>
            <dd className="mono">{formatTime(data.info.notAfter)}</dd>
            <dt>指纹</dt>
            <dd className="mono cert-pin">{data.info.pin}</dd>
          </dl>
          {data.info.selfSigned && (
            <p className="field__hint" style={{ marginTop: 8 }}>
              自签名证书：生成的节点链接与订阅已自动附带 insecure 与 pinSHA256，
              客户端可直接导入；改用 ACME 正式证书后这些参数会自动消失。
            </p>
          )}
        </>
      )}
    </div>
  )
}