import { useCallback, useEffect, useRef, useState } from 'react'
import { API, ApiError, formatBytes, formatDuration } from '../api'
import type { HostStats, Settings, SystemInfo, SystemStatus, UpdateCheck } from '../api'
import { Page } from '../components/Layout'
import { Badge, Card, ConfirmDialog, Field, IssueList, Loading } from '../components/ui'
import { IconPlay, IconRefresh, IconRestart, IconStop } from '../icons'
import { useToast } from '../toast'

type ServiceName = 'hysteria'


/** VPS 主机资源：实时刷新（1 秒轮询 /api/system/host）。 */
function HostCard() {
  const [host, setHost] = useState<HostStats | null>(null)
  const { toast } = useToast()
  const warned = useRef(false)

  useEffect(() => {
    let stop = false
    const tick = async () => {
      try {
        const h = await API.system.host()
        if (!stop) setHost(h)
      } catch (err) {
        if (!warned.current) {
          warned.current = true
          toast(err instanceof ApiError ? err.message : '读取主机资源失败', 'err')
        }
      }
    }
    void tick()
    const timer = window.setInterval(() => void tick(), 1000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [toast])

  /** 占用越高颜色越警示。 */
  const fillClass = (pct: number) =>
    pct >= 85 ? 'bar__fill bar__fill--danger' : pct >= 65 ? 'bar__fill bar__fill--warn' : 'bar__fill'

  return (
    <Card
      title="VPS 主机"
      extra={
        <span className="muted">
          <span className="live" />
          实时刷新{host?.updatedAt ? ` · ${host.updatedAt}` : ''}
        </span>
      }
    >
      {!host ? (
        <Loading />
      ) : (
        <div className="hostgrid">
          <div className="hostcard">
            <div className="hostcard__top">
              <span className="hostcard__label">CPU 使用率</span>
              <span className="hostcard__pct">
                {host.cpuPercent.toFixed(1)}<i>%</i>
              </span>
            </div>
            <div className="bar">
              <span className={fillClass(host.cpuPercent)} style={{ width: `${host.cpuPercent}%` }} />
            </div>
            <span className="hostcard__sub">
              {host.cpuCores} 核 · 负载 {host.load1.toFixed(2)} / {host.load5.toFixed(2)} / {host.load15.toFixed(2)}
            </span>
          </div>

          <div className="hostcard">
            <div className="hostcard__top">
              <span className="hostcard__label">运行内存</span>
              <span className="hostcard__pct">
                {host.memPercent.toFixed(1)}<i>%</i>
              </span>
            </div>
            <div className="bar">
              <span className={fillClass(host.memPercent)} style={{ width: `${host.memPercent}%` }} />
            </div>
            <span className="hostcard__sub">
              {formatBytes(host.memUsed)} / {formatBytes(host.memTotal)}
              {host.swapTotal > 0
                ? ` · 交换 ${formatBytes(host.swapUsed)} / ${formatBytes(host.swapTotal)}`
                : ''}
            </span>
          </div>

          <div className="hostcard">
            <div className="hostcard__top">
              <span className="hostcard__label">磁盘存储</span>
              <span className="hostcard__pct">
                {host.diskPercent.toFixed(1)}<i>%</i>
              </span>
            </div>
            <div className="bar">
              <span className={fillClass(host.diskPercent)} style={{ width: `${host.diskPercent}%` }} />
            </div>
            <span className="hostcard__sub">
              {formatBytes(host.diskUsed)} / {formatBytes(host.diskTotal)} · 可用 {formatBytes(host.diskFree)}
            </span>
          </div>

          <div className="hostcard">
            <div className="hostcard__top">
              <span className="hostcard__label">主机</span>
              <span className="hostcard__pct hostcard__pct--sm">{host.hostname || '—'}</span>
            </div>
            <span className="hostcard__sub">内核 {host.kernel || '—'}</span>
            <span className="hostcard__sub">已运行 {formatDuration(host.uptime)}</span>
          </div>
        </div>
      )}
    </Card>
  )
}

/** 设置页：面板设置、服务控制、主机信息、内核更新（开发文档 48 节）。 */
export default function System() {
  const [status, setStatus] = useState<SystemStatus | null>(null)
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [settings, setSettings] = useState<Settings | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [pending, setPending] = useState<{ name: ServiceName; action: 'stop' | 'restart' } | null>(null)
  const [pwdOpen, setPwdOpen] = useState(false)
  const { toast } = useToast()

  const load = useCallback(async () => {
    try {
      const [s, i, cfg] = await Promise.all([API.system.status(), API.system.info(), API.settings.get()])
      setStatus(s)
      setInfo(i)
      setSettings(cfg)
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '读取系统信息失败', 'err')
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => {
    void load()
    // 自动刷新，无需手动点刷新按钮
    const timer = window.setInterval(() => void load(), 2000)
    return () => window.clearInterval(timer)
  }, [load])

  const doAction = async (name: ServiceName, action: 'start' | 'stop' | 'restart') => {
    setBusy(true)
    try {
      const res = await API.system.service(name, action)
      toast(res.output || '操作已完成', 'ok')
      await load()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '操作失败', 'err')
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  const saveSettings = async () => {
    if (!settings) return
    setBusy(true)
    try {
      await API.settings.update(settings)
      toast('设置已保存', 'ok')
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '保存失败', 'err')
    } finally {
      setBusy(false)
    }
  }

  const systemd = status?.panel.systemd ?? false

  /** 服务行的操作按钮组。 */
  const serviceActions = (name: ServiceName) => (
    <div className="svc-row__actions">
      <button
        type="button"
        className="btn btn--sm"
        disabled={busy || !systemd}
        onClick={() => void doAction(name, 'start')}
      >
        <IconPlay size={14} />
        启动
      </button>
      <button
        type="button"
        className="btn btn--sm"
        disabled={busy || !systemd}
        onClick={() => setPending({ name, action: 'restart' })}
      >
        <IconRestart size={14} />
        重启
      </button>
      <button
        type="button"
        className="btn btn--sm btn--danger"
        disabled={busy || !systemd}
        onClick={() => setPending({ name, action: 'stop' })}
      >
        <IconStop size={14} />
        停止
      </button>
    </div>
  )

  return (
    <Page
      title="服务状态"
      subtitle="面板设置 · 服务控制 · 主机信息 · 内核更新（这一切都是本机与面板自身，不是代理协议）"
    >
      {!systemd && status && (
        <IssueList
          issues={[
            {
              severity: 'warning',
              field: 'systemd',
              message: '当前环境未由 systemd 托管，无法在面板内启停服务；配置修改会写入文件，但需要手动重启服务。',
            },
          ]}
        />
      )}

      {loading && !status ? (
        <Card>
          <Loading />
        </Card>
      ) : (
        <>
          <HostCard />

          <Card title="服务" extra={<span className="muted">实时刷新</span>} flush>
            <div className="svc-list">
              <div className="svc-row">
                  <div className="svc-row__head">
                <span className="svc-row__name">Hysteria2 服务</span>
                {status?.hysteria.active ? <Badge kind="ok">运行中</Badge> : <Badge kind="warn">未运行</Badge>}
                  </div>
                  <div className="svc-row__metas">
                <span className="svc-row__meta mono">{status?.hysteria.version || '版本未知'}</span>
                <span className="svc-row__meta mono">{status?.panel.serviceUnit}</span>
                  </div>
                {serviceActions('hysteria')}
              </div>

              <div className="svc-row">
                  <div className="svc-row__head">
                <span className="svc-row__name">Panel 服务</span>
                <Badge kind="ok">运行中</Badge>
                  </div>
                  <div className="svc-row__metas">
                <span className="svc-row__meta mono">
                  {status?.panel.version} · 已运行 {formatDuration(status?.panel.uptime)}
                </span>
                <span className="svc-row__meta">由 systemd 托管</span>
                  </div>
              </div>
            </div>
          </Card>

          <Card title="面板信息">
            {!info ? (
              <Loading />
            ) : (
              <div className="metric-row metric-row--pair">
                <div className="metric">
                  <span className="metric__label">Go 版本</span>
                  <span className="metric__value metric__value--sm">{String(info.goVersion || '—')}</span>
                </div>
                <div className="metric">
                  <span className="metric__label">配置文件</span>
                  <span className="metric__value metric__value--sm mono">{String(info.configPath || '—')}</span>
                </div>
                <div className="metric">
                  <span className="metric__label">备份目录</span>
                  <span className="metric__value metric__value--sm mono">{String(info.backupDir || '—')}</span>
                </div>
                <div className="metric">
                  <span className="metric__label">数据目录</span>
                  <span className="metric__value metric__value--sm mono">{String(info.dataDir || '—')}</span>
                </div>
              </div>
            )}
          </Card>

          <Card
            title="面板设置"
            extra={
              <button
                type="button"
                className="btn btn--sm btn--primary"
                onClick={saveSettings}
                disabled={busy || !settings}
              >
                保存
              </button>
            }
          >
            {!settings ? (
              <Loading />
            ) : (
              <div className="form-grid">
                <Field label="服务器地址" hint="用于生成 HY2 URI 与订阅；留空时使用浏览器访问地址">
                  <input
                    className="input input--mono"
                    value={settings.serverHost}
                    onChange={(e) => setSettings({ ...settings, serverHost: e.target.value })}
                  />
                </Field>
                <Field label="订阅基础地址" hint="如 https://example.com；留空时使用当前访问地址">
                  <input
                    className="input input--mono"
                    value={settings.subBaseURL}
                    onChange={(e) => setSettings({ ...settings, subBaseURL: e.target.value })}
                  />
                </Field>
                <Field label="Clash mixed-port">
                  <input
                    className="input"
                    type="number"
                    value={settings.mixedPort}
                    onChange={(e) => setSettings({ ...settings, mixedPort: Number(e.target.value) })}
                  />
                </Field>
                <Field label="HY2 日志来源" hint="journal:单元名 或文件路径">
                  <input
                    className="input input--mono"
                    value={settings.logHysteria}
                    onChange={(e) => setSettings({ ...settings, logHysteria: e.target.value })}
                  />
                </Field>
                <Field label="Panel 日志路径">
                  <input
                    className="input input--mono"
                    value={settings.logPanel}
                    onChange={(e) => setSettings({ ...settings, logPanel: e.target.value })}
                  />
                </Field>
              </div>
            )}
          </Card>

          <Card title="账号安全">
            <div className="row">
              <button type="button" className="btn btn--sm" onClick={() => setPwdOpen(true)}>
                修改管理员密码
              </button>
              <span className="muted">修改后当前会话会失效，需要重新登录</span>
            </div>
          </Card>

          <UpdateCard />
        </>
      )}

      {pending && (
        <ConfirmDialog
          title={pending.action === 'stop' ? '停止服务' : '重启服务'}
          danger={pending.action === 'stop'}
          message={
            pending.action === 'stop'
              ? '确认停止 Hysteria 2 服务？所有在线连接会立即中断。'
              : '确认重启 Hysteria 2 服务？连接会短暂中断。'
          }
          confirmText={pending.action === 'stop' ? '停止' : '重启'}
          busy={busy}
          onCancel={() => setPending(null)}
          onConfirm={() => void doAction(pending.name, pending.action)}
        />
      )}

      {pwdOpen && <ChangePasswordDialog onClose={() => setPwdOpen(false)} />}
    </Page>
  )
}

/** 官方 Core 更新（开发文档 48 节）。 */
function UpdateCard() {
  const [check, setCheck] = useState<UpdateCheck | null>(null)
  const [lastChecked, setLastChecked] = useState('')
  const [checking, setChecking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [confirmApply, setConfirmApply] = useState(false)
  const { toast } = useToast()

  const doCheck = useCallback(async (silent = false) => {
    setChecking(true)
    try {
      const res = await API.system.updateCheck()
      setCheck(res)
      setLastChecked(new Date().toLocaleTimeString('zh-CN', { hour12: false }))
      if (!silent) {
        toast(
          res.upToDate ? `已是最新版本（${res.current}）` : `发现新版本：${res.latest}`,
          res.upToDate ? 'ok' : 'info',
        )
      }
    } catch (err) {
      setCheck(null)
      toast(err instanceof ApiError ? err.message : '查询最新版本失败', 'err')
    } finally {
      setChecking(false)
    }
  }, [toast])

  useEffect(() => {
    void doCheck(true)
  }, [doCheck])

  const apply = async () => {
    setBusy(true)
    try {
      const res = await API.system.updateApply()
      toast(`已更新到 ${res.result.to}`, 'ok')
      setConfirmApply(false)
      await doCheck()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '更新失败', 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card
      title="官方 Core 更新"
      extra={
        check && (check.upToDate ? <Badge kind="ok">已是最新</Badge> : <Badge kind="warn">有新版本</Badge>)
      }
    >
      <div className="metric-row">
        <div className="metric">
          <span className="metric__label">当前版本</span>
          <span className="metric__value metric__value--sm mono">{check?.current || '—'}</span>
        </div>
        <div className="metric">
          <span className="metric__label">最新版本</span>
          <span className="metric__value metric__value--sm mono">
            {check?.latest || '—'}
            {check?.publishedAt && <span className="metric__unit">{check.publishedAt.slice(0, 10)}</span>}
          </span>
        </div>
        <div className="metric">
          <span className="metric__label">匹配的发布文件</span>
          <span className="metric__value metric__value--sm mono">{check?.assetName || '—'}</span>
        </div>
      </div>

      <div className="row" style={{ marginTop: 14 }}>
        <button
          type="button"
          className="btn btn--sm"
          onClick={() => void doCheck()}
          disabled={checking}
        >
          {checking ? <span className="spinner" /> : <IconRefresh size={14} />}
          {checking ? '正在查询…' : '检查更新'}
        </button>
        <button
          type="button"
          className="btn btn--sm btn--primary"
          disabled={busy || !check || check.upToDate}
          onClick={() => setConfirmApply(true)}
        >
          更新到最新版
        </button>
      </div>

      {lastChecked && (
        <p className="muted" style={{ margin: '10px 0 0' }}>
          最后一次检查：{lastChecked}
        </p>
      )}

      <p className="muted" style={{ margin: '10px 0 0' }}>
        更新会下载官方发布的最新二进制，先自检再原子替换；替换前保留上一版本，启动失败会自动回滚。
      </p>

      {confirmApply && (
        <ConfirmDialog
          title="更新官方 Core"
          message="确认下载并安装官方最新版本？更新过程中服务会重启，连接会短暂中断。"
          confirmText="更新"
          busy={busy}
          onCancel={() => setConfirmApply(false)}
          onConfirm={() => void apply()}
        />
      )}
    </Card>
  )
}

function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const { toast } = useToast()

  const submit = async () => {
    if (next.length < 8) {
      setError('新密码长度至少 8 位')
      return
    }
    setBusy(true)
    setError('')
    try {
      await API.changePassword(current, next)
      toast('密码已修改，请重新登录', 'ok')
      onClose()
      window.setTimeout(() => window.location.reload(), 800)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '修改失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog" role="dialog" aria-modal="true">
        <header className="dialog__head">
          <h3>修改管理员密码</h3>
        </header>
        <div className="dialog__body">
          <Field label="当前密码" error={error}>
            <input
              className="input"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoFocus
            />
          </Field>
          <Field label="新密码" hint="至少 8 位">
            <input className="input" type="password" value={next} onChange={(e) => setNext(e.target.value)} />
          </Field>
        </div>
        <footer className="dialog__foot">
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button type="button" className="btn btn--primary" onClick={submit} disabled={busy}>
            {busy && <span className="spinner" />}
            保存
          </button>
        </footer>
      </div>
    </div>
  )
}