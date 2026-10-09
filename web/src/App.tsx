import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { Layout } from './components/Layout'
import { useSession } from './session'
import Account from './pages/Account'
import Login from './pages/Login'
import Logs from './pages/Logs'
import Server from './pages/Server'
import System from './pages/System'

/** 受保护路由：未登录时跳转到登录页。 */
function Protected({ children }: { children: ReactNode }) {
  const { authenticated, loading } = useSession()
  const location = useLocation()

  if (loading) {
    return (
      <div className="login">
        <div className="login__card" style={{ alignItems: 'center' }}>
          <span className="spinner" />
          <span className="muted">正在恢复会话…</span>
        </div>
      </div>
    )
  }

  if (!authenticated) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return <Layout>{children}</Layout>
}

/**
 * 路由表。
 *
 * 面板只服务一个账号，首页直接就是「账号名称 + 订阅信息」，
 * 没有任何统计仪表盘；其余只保留服务器配置、日志、设置：
 *   账号与订阅 / 服务器 / 日志 / 设置
 */
export default function App() {
  const { authenticated } = useSession()

  return (
    <Routes>
      <Route path="/login" element={authenticated ? <Navigate to="/" replace /> : <Login />} />

      <Route
        path="/"
        element={
          <Protected>
            <Account />
          </Protected>
        }
      />
      {/* 兼容旧链接：账号页已合并到首页 */}
      <Route path="/users" element={<Navigate to="/" replace />} />
      <Route
        path="/server"
        element={
          <Protected>
            <Server />
          </Protected>
        }
      />
      <Route
        path="/logs"
        element={
          <Protected>
            <Logs />
          </Protected>
        }
      />
      <Route
        path="/settings"
        element={
          <Protected>
            <System />
          </Protected>
        }
      />

      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}