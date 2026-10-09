import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError } from '../api'
import { useSession } from '../session'
import { Field } from '../components/ui'
import { ThemeSwitch } from '../components/Layout'
import { IconArrowForward } from '../icons'

/**
 * 登录页（极简）。
 *
 * 只有一个显示名称 + 账号 / 密码两个输入框，
 * 深浅色切换固定在右上角。
 */
export default function Login() {
  const { login } = useSession()
  const navigate = useNavigate()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
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

      <form className="login__card" onSubmit={submit}>
        <h1 className="login__name">Hysteria2 管理面板</h1>

        <Field label="用户名">
          <input
            className="input"
            value={username}
            autoComplete="username"
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </Field>

        <Field label="密码" error={error}>
          <input
            className="input"
            type="password"
            value={password}
            autoComplete="current-password"
            onChange={(e) => setPassword(e.target.value)}
            required
            autoFocus
          />
        </Field>

        <div className="login__actions">
          <button
            type="submit"
            className="login__go"
            disabled={busy}
            title="登录"
            aria-label="登录"
          >
            {busy ? <span className="spinner" /> : <IconArrowForward size={22} />}
          </button>
        </div>
      </form>
    </div>
  )
}
