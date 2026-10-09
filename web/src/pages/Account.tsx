import { useCallback, useEffect, useState } from 'react'
import { API, ApiError, formatBytes } from '../api'
import type { UserEvent, UserView } from '../api'
import { Page } from '../components/Layout'
import { SharePanel, prefetchShare } from '../components/SharePanel'
import {
  Card,
  ConfirmDialog,
  CopyButton,
  Dialog,
  Empty,
  Field,
  Loading,
} from '../components/ui'
import { IconChevronDown, IconCopy, IconEdit, IconLogs, IconPlus, IconRefresh, IconTrash } from '../icons'
import { useToast } from '../toast'

/**
 * 首页：账号列表（折叠式）。
 *
 * 服务器配置是**全局共用**的（监听、证书、混淆、QUIC…都在「服务器」页统一改），
 * 只有账号本身是各自独立的：名称、密码、订阅 Token、流量与在线状态。
 *
 * 列表用「一行一个账号 + 右侧箭头」的折叠形态：
 * 收起时只看得到名称与在线状态，展开后就是这个账号的全部信息
 * （累计流量、Hysteria2 链接、Clash 订阅，连同二维码与重置 Token）。
 */
export default function Account() {
  const [users, setUsers] = useState<UserView[]>([])
  const [openId, setOpenId] = useState<number | null>(null)
  /** 展开过（内容已挂载）的账号：避免每次点开都重新拉一次订阅链接 */
  const [mountedIds, setMountedIds] = useState<number[]>([])
  /** 账号被改过（改密码/改名）后 +1，让展开区的订阅链接重新拉取 */
  const [subReload, setSubReload] = useState(0)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [editUser, setEditUser] = useState<UserView | null>(null)
  const [delUser, setDelUser] = useState<UserView | null>(null)
  const [logUser, setLogUser] = useState<UserView | null>(null)
  const [notice, setNotice] = useState<{ title: string; username: string; password: string } | null>(
    null,
  )
  const { toast } = useToast()

  /** 展开 / 收起某个账号；首次展开时把内容挂载进来。 */
  const toggle = (id: number) => {
    setOpenId((cur) => (cur === id ? null : id))
    setMountedIds((cur) => (cur.includes(id) ? cur : [...cur, id]))
  }

  /** 直接展开某个账号（新建、补建后自动定位用）。 */
  const expand = (id: number) => {
    setOpenId(id)
    setMountedIds((cur) => (cur.includes(id) ? cur : [...cur, id]))
  }

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const list = await API.users.list('')
        const items = list.items || []
        setUsers(items)
        // 预取每个账号的订阅链接：点展开时两个链接区域已经是现成的
        items.forEach((u) => prefetchShare(u.id))
        // 默认全部收起；只有当前展开的账号被删掉时才收起
        setOpenId((cur) => (cur != null && items.some((u) => u.id === cur) ? cur : null))
      } catch (err) {
        toast(err instanceof ApiError ? err.message : '加载用户失败', 'err')
      } finally {
        setLoading(false)
      }
    },
    [toast],
  )

  // 列表里已带在线数与累计流量，定时刷新即可（不必再单独拉详情）
  useEffect(() => {
    void load()
    const timer = window.setInterval(() => void load(true), 10000)
    return () => window.clearInterval(timer)
  }, [load])

  /** 删除用户；若删空则面板会补建一个，这里把新凭据展示出来。 */
  const removeAccount = async () => {
    if (!delUser) return
    setBusy(true)
    try {
      const res = await API.users.remove(delUser.id)
      const removed = delUser.username
      setDelUser(null)
      if (res.replacement) {
        prefetchShare(res.replacement.id)
        expand(res.replacement.id)
        setNotice({
          title: '已补建新用户',
          username: res.replacement.username,
          password: res.replacementPassword || '',
        })
      } else {
        toast(
          res.syncError ? `已删除，但同步失败：${res.syncError}` : `已删除「${removed}」`,
          res.syncError ? 'err' : 'ok',
        )
      }
      await load()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '删除失败', 'err')
    } finally {
      setBusy(false)
    }
  }



  /** 列表顺序与序号：按创建先后自上而下 1、2、3（新账号排在底部，序号更大）。 */
  const list = [...users].sort((a, b) => a.id - b.id)
  const seqOf = new Map<number, number>()
  list.forEach((x, i) => seqOf.set(x.id, i + 1))
  return (
    <Page title="账号与订阅" subtitle="服务器配置全局共用 · 每个账号独立订阅">
      <Card
        title="用户列表"
        flush
        extra={
          <span className="uhead">
            <button
              type="button"
              className="btn btn--icon btn--primary addbtn"
              onClick={() => setAddOpen(true)}
              title="添加用户"
              aria-label="添加用户"
            >
              <IconPlus size={20} />
            </button>
          </span>
        }
      >
        {loading && users.length === 0 ? (
          <Loading />
        ) : users.length === 0 ? (
          <Empty
            title="还没有用户"
            hint="点右上角的＋创建一个，名称会自动生成"
            action={
              <button
                type="button"
                className="btn btn--icon btn--primary"
                onClick={() => setAddOpen(true)}
                title="添加用户"
                aria-label="添加用户"
              >
                <IconPlus size={20} />
              </button>
            }
          />
        ) : (
          <ul className="ulist">
            {list.map((u) => {
              const open = u.id === openId
              const seq = seqOf.get(u.id) ?? 0
              return (
                <li key={u.id} className={open ? 'uacc uacc--open' : 'uacc'}>
                  {/* 行头：名称（点名称也能展开）+ 修改/删除 + 右侧下拉箭头 */}
                  <div className="uacc__head">
                    <span className="uacc__index" title={`第 ${seq} 个账号`}>
                      {seq}
                    </span>
                    <button
                      type="button"
                      className="uacc__toggle"
                      onClick={() => toggle(u.id)}
                      aria-expanded={open}
                    >
                      <span className="uacc__name">{u.username}</span>
                      <span className="uacc__meta">
                        <span
                          className={
                            u.online > 0 ? 'uacc__state uacc__state--on' : 'uacc__state uacc__state--off'
                          }
                        >
                          {u.online > 0 ? `在线 · ${u.online} 个连接` : '离线'}
                        </span>
                        {' · 累计 '}
                        ↓ {formatBytes(u.historicalRx)} ↑ {formatBytes(u.historicalTx)}
                      </span>
                    </button>
                    <button
                      type="button"
                      className={open ? 'uacc__expand uacc__expand--open' : 'uacc__expand'}
                      onClick={() => toggle(u.id)}
                      aria-expanded={open}
                      aria-label={open ? '收起' : '展开'}
                      title={open ? '收起' : '展开'}
                    >
                      <IconChevronDown
                        size={15}
                        className={open ? 'uacc__expand-svg uacc__expand-svg--open' : 'uacc__expand-svg'}
                      />
                      {open ? '收起' : '展开'}
                    </button>
                  </div>

                  {/* 展开区：累计流量 + 订阅（链接 / 复制 / 二维码 / 重置 Token） */}
                  <div className={open ? 'uacc__body uacc__body--open' : 'uacc__body'}>
                    {mountedIds.includes(u.id) && (
                      <div className="uacc__clip">
                        <div className="uacc__inner">
                          <SharePanel key={u.id} userId={u.id} reloadKey={subReload} />
                          {/* 账号操作放展开区最下方 */}
                          <div className="uacc__foot">
                            <button
                              type="button"
                              className="btn btn--sm"
                              onClick={() => setLogUser(u)}
                            >
                              <IconLogs size={14} />
                              日志
                            </button>
                            <button
                              type="button"
                              className="btn btn--sm"
                              onClick={() => setEditUser(u)}
                            >
                              <IconEdit size={14} />
                              修改
                            </button>
                            <button
                              type="button"
                              className="btn btn--sm btn--danger"
                              onClick={() => setDelUser(u)}
                            >
                              <IconTrash size={14} />
                              删除
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </Card>

      {addOpen && (
        <CreateAccountDialog
          onClose={() => setAddOpen(false)}
          onCreated={async (info) => {
            setAddOpen(false)
            setNotice({ title: '用户已添加', username: info.username, password: info.password })
            await load()
            try {
              const list = await API.users.list('')
              const fresh = (list.items || []).find((u) => u.username === info.username)
              if (fresh) expand(fresh.id)
            } catch {
              // 选中失败不影响使用
            }
          }}
        />
      )}

      {editUser && (
        <EditUserDialog
          user={editUser}
          onClose={() => setEditUser(null)}
          onSaved={async () => {
            setEditUser(null)
            // 改名/改密码都会换掉这个账号的订阅 Token，让展开区重新拉一次
            setSubReload((n) => n + 1)
            await load()
          }}
        />
      )}

      {logUser && <UserLogDialog user={logUser} onClose={() => setLogUser(null)} />}
      {delUser && (
        <ConfirmDialog
          title="删除用户"
          danger
          message={
            users.length <= 1
              ? `「${delUser.username}」是最后一个用户。删除后官方 Core 会因为 userpass 为空而无法启动，因此面板会立即补建一个随机用户并把新密码告诉你。确认删除？`
              : `确认删除「${delUser.username}」？该用户的订阅链接会立即失效，已连接的客户端会被断开。`
          }
          confirmText="删除"
          busy={busy}
          onCancel={() => setDelUser(null)}
          onConfirm={() => void removeAccount()}
        />
      )}

      {notice && (
        <Dialog
          title={notice.title}
          onClose={() => setNotice(null)}
          footer={
            <button type="button" className="btn btn--primary" onClick={() => setNotice(null)}>
              完成
            </button>
          }
        >
          <p className="muted">密码仅在本次显示，请立即保存。</p>
          <Field label="名称">
            <input className="input input--mono" readOnly value={notice.username} />
          </Field>
          <Field label="密码">
            <input className="input input--mono" readOnly value={notice.password} />
          </Field>
          <div>
            <CopyButton text={`${notice.username}\n${notice.password}`} label="复制账号密码" />
          </div>
        </Dialog>
      )}
    </Page>
  )
}

/** 生成随机密码/后缀：大小写字母 + 数字（去掉 I/l/0/O 等易混字符）。 */
function randomPassword(len = 16) {
  const alphabet = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  const buf = new Uint32Array(len)
  crypto.getRandomValues(buf)
  return Array.from(buf, (n) => alphabet[n % alphabet.length]).join('')
}

function CreateAccountDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (info: { username: string; password: string }) => void
}) {
  const [suffix, setSuffix] = useState('')
  const [password, setPassword] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const { toast } = useToast()


  const submit = async () => {
    if (suffix.length > 32) {
      setError('名称后缀最长 32 位')
      return
    }
    setBusy(true)
    setError('')
    try {
      const res = await API.users.create(password, note, suffix)
      if (res.syncError) {
        toast(`用户已添加，但同步到官方配置失败：${res.syncError}`, 'err')
      } else {
        toast('用户已添加并同步到官方配置', 'ok')
      }
      onCreated({ username: res.user.username, password: res.password })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '添加失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      title="添加用户"
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button type="button" className="btn btn--primary" onClick={submit} disabled={busy}>
            {busy && <span className="spinner" />}
            添加
          </button>
        </>
      }
    >
      <Field
        label="名称"
        error={error}
      >
        <div className="name-builder">
          <span className="name-builder__prefix">hysteria2-</span>
          <input
            className="input input--mono"
            value={suffix}
            maxLength={32}
            placeholder="留空则随机生成"
            onChange={(e) => {
              setSuffix(e.target.value.replace(/[^a-zA-Z0-9]/g, '').slice(0, 32))
              setError('')
            }}
          />
        </div>
      </Field>

      <Field label="密码" hint="留空将自动生成高强度随机密码">
        <div className="input-row">
          <input
            className="input input--mono"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <span className="input-row__action">
            <button
              type="button"
              className="btn"
              onClick={() => setPassword(randomPassword())}
            >
              <IconRefresh size={14} />
              随机生成
            </button>
          </span>
        </div>
      </Field>

      <Field label="备注">
        <input className="input" value={note} onChange={(e) => setNote(e.target.value)} />
      </Field>
    </Dialog>
  )
}

/** 取出名称中 hysteria2- 之后的部分（前缀固定，只允许改这一段）。 */
function suffixOf(name: string) {
  const i = name.indexOf('-')
  return i >= 0 && i + 1 < name.length ? name.slice(i + 1) : name
}

/**
 * 修改用户：名称 + 密码，一次提交。
 *
 * 名称只允许改 hysteria2- 之后的部分；密码留空表示不修改。
 */
function EditUserDialog({
  user,
  onClose,
  onSaved,
}: {
  user: UserView
  onClose: () => void
  onSaved: () => void
}) {
  const [suffix, setSuffix] = useState(suffixOf(user.username))
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const { toast } = useToast()

  const nameChanged = suffix !== suffixOf(user.username)

  const submit = async () => {
    if (!suffix) {
      setError('名称不能为空')
      return
    }
    if (suffix.length > 32) {
      setError('名称最长 32 位')
      return
    }
    if (!nameChanged && !password) {
      onClose()
      return
    }
    setBusy(true)
    setError('')
    try {
      const patch: { suffix?: string; password?: string } = {}
      if (nameChanged) patch.suffix = suffix
      if (password) patch.password = password
      const res = await API.users.update(user.id, patch)
      if (res.syncError) {
        toast(`已保存，但同步到官方配置失败：${res.syncError}`, 'err')
      } else if (res.rotatedToken) {
        toast('已保存，该账号的订阅地址已重新生成', 'ok')
      } else {
        toast('已保存并同步到官方配置', 'ok')
      }
      onSaved()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      title={`修改用户 · ${user.username}`}
      onClose={onClose}
      footer={
        <>
          <button type="button" className="btn" onClick={onClose} disabled={busy}>
            取消
          </button>
          <button type="button" className="btn btn--primary" onClick={submit} disabled={busy}>
            {busy && <span className="spinner" />}
            保存
          </button>
        </>
      }
    >
      <Field label="名称" error={error}>
        <div className="name-builder">
          <span className="name-builder__prefix">hysteria2-</span>
          <input
            className="input input--mono"
            value={suffix}
            maxLength={32}
            placeholder="只填后面的部分"
            onChange={(e) => {
              setSuffix(e.target.value.replace(/[^a-zA-Z0-9]/g, '').slice(0, 32))
              setError('')
            }}
          />
        </div>
      </Field>

      <Field label="新密码" hint="留空表示不修改密码">
        <div className="input-row">
          <input
            className="input input--mono"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <span className="input-row__action">
            <button
              type="button"
              className="btn"
              onClick={() => setPassword(randomPassword(18))}
            >
              <IconRefresh size={14} />
              随机生成
            </button>
          </span>
        </div>
      </Field>
    </Dialog>
  )
}
/**
 * 单个用户的接入 / 断开记录。
 *
 * 数据来源：Panel 后台每 15 秒读取一次官方 /online，与前一次快照比对，
 * 0 → N 记为「接入」，N → 0 记为「断开」。官方 Core 本身不提供这份历史。
 */
function UserLogDialog({ user, onClose }: { user: UserView; onClose: () => void }) {
  const [events, setEvents] = useState<UserEvent[] | null>(null)
  const [error, setError] = useState('')
  const { toast } = useToast()

  /** 复制全部连接日志（面板跑在 http 上，需兼容旧式复制） */
  const copyAll = async () => {
    const lines = (events || []).map((ev) =>
      [
        ev.kind === 'connect' ? '接入' : '断开',
        formatDateTime(ev.ts),
        ev.kind === 'connect' && ev.online > 0 ? `${ev.online} 个连接` : '',
      ]
        .filter(Boolean)
        .join('  '),
    )
    const text = lines.length > 0 ? lines.join(String.fromCharCode(10)) : '（暂无连接记录）'
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text)
      } else {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      }
      toast('已复制全部连接日志', 'ok')
    } catch {
      toast('复制失败，请手动选择文本', 'err')
    }
  }

  useEffect(() => {
    let alive = true
    API.users
      .events(user.id)
      .then((res) => {
        if (alive) setEvents(res.items || [])
      })
      .catch((err) => {
        if (alive) setError(err instanceof ApiError ? err.message : '加载失败')
      })
    return () => {
      alive = false
    }
  }, [user.id])

  return (
    <Dialog
      title={`${user.username} 连接日志`}
      noBackdropClose
      headExtra={
        <button
          type="button"
          className="btn btn--ghost btn--icon"
          onClick={copyAll}
          title="复制全部连接日志"
          aria-label="复制全部连接日志"
        >
          <IconCopy size={16} />
        </button>
      }
      onClose={onClose}
    >
      <div className="ulog">
        {error ? (
          <p className="ulog__empty">{error}</p>
        ) : events === null ? (
          <Loading />
        ) : events.length === 0 ? (
          <p className="ulog__empty">
            还没有连接记录。面板每 15 秒检测一次在线状态，客户端接入或断开后就会出现在这里。
          </p>
        ) : (
          <ul className="ulog__list">
            {events.map((ev) => (
              <li key={ev.id} className="ulog__item">
                <span
                  className={
                    ev.kind === 'connect' ? 'ulog__kind ulog__kind--on' : 'ulog__kind ulog__kind--off'
                  }
                >
                  {ev.kind === 'connect' ? '接入' : '断开'}
                </span>
                <span className="ulog__time">{formatDateTime(ev.ts)}</span>
                {ev.kind === 'connect' && ev.online > 0 ? (
                  <span className="ulog__extra">{ev.online} 个连接</span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  )
}

/** 把 Unix 秒格式化成“月-日 时:分:秒”。 */
function formatDateTime(ts: number) {
  const d = new Date(ts * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}
