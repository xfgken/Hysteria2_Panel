import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError } from '../api'
import { useSession } from '../session'
import { ThemeSwitch } from '../components/Layout'
import {
  IconArrowForward,
  IconError,
  IconKey,
  IconUser,
  IconVisibility,
  IconVisibilityOff,
} from '../icons'

/**
 * 登录页。
 *
 * 整页只有一个背景色，没有卡片外框：
 *  · 顶部一枚圆形徽标 + 面板名称 + 一行说明
 *  · 两个胶囊输入框（左侧图标；密码可切换明文）
 *  · 一颗整行饱满的主按钮
 * 深浅色切换固定在右上角。
 */
export default function Login() {
  const { login } = useSession()
  const navigate = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await login(username.trim(), password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败，请稍后重试')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login">
      <div className="login__theme">
        <ThemeSwitch compact />
      </div>

      <div className="login__wrap">
        <div className="login__brand">
          <h1 className="login__name">Hysteria2 管理面板</h1>
          <p className="login__sub">轻量级 Hysteria2 控制面板</p>
        </div>

        <form className="login__form" onSubmit={submit}>
          <label className="lgf">
            <span className="lgf__icon">
              <IconUser size={18} />
            </span>
            <input
              className="lgf__input"
              value={username}
              placeholder="用户名"
              autoComplete="username"
              onChange={(e) => setUsername(e.target.value)}
              required
            />
          </label>

          <label className="lgf">
            <span className="lgf__icon">
              <IconKey size={18} />
            </span>
            <input
              className="lgf__input"
              type={show ? 'text' : 'password'}
              value={password}
              placeholder="密码"
              autoComplete="current-password"
              onChange={(e) => setPassword(e.target.value)}
              required
              autoFocus
            />
            <button
              type="button"
              className="lgf__eye"
              onClick={() => setShow((v) => !v)}
              title={show ? '隐藏密码' : '显示密码'}
              aria-label={show ? '隐藏密码' : '显示密码'}
              tabIndex={-1}
            >
              {show ? <IconVisibilityOff size={18} /> : <IconVisibility size={18} />}
            </button>
          </label>

          <p className="login__err">
            {error ? (
              <>
                <IconError size={15} />
                {error}
              </>
            ) : null}
          </p>

          <button type="submit" className="login__go" disabled={busy} title="登录" aria-label="登录">
            {busy ? (
              <span className="spinner" />
            ) : (
              <>
                登录
                <IconArrowForward size={18} />
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
