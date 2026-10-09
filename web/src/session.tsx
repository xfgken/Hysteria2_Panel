import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { API, setCSRF, setUnauthorizedHandler } from './api'

type SessionCtxValue = {
  loading: boolean
  authenticated: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const SessionCtx = createContext<SessionCtxValue | null>(null)

/**
 * 会话提供者。
 *
 * Panel 使用 HttpOnly Cookie 保存会话，前端仅持有 CSRF Token：
 *  · 刷新页面时通过 GET /api/session 恢复登录态；
 *  · 任何 401 都会自动把登录态置为未登录。
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [authenticated, setAuthenticated] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const info = await API.session()
      setCSRF(info.csrfToken)
      setAuthenticated(true)
    } catch {
      setCSRF('')
      setAuthenticated(false)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setCSRF('')
      setAuthenticated(false)
    })
    return () => setUnauthorizedHandler(null)
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const login = useCallback(async (username: string, password: string) => {
    const res = await API.login(username, password)
    setCSRF(res.csrfToken)
    setAuthenticated(true)
  }, [])

  const logout = useCallback(async () => {
    try {
      await API.logout()
    } finally {
      setCSRF('')
      setAuthenticated(false)
    }
  }, [])

  const value = useMemo(
    () => ({ loading, authenticated, login, logout, refresh }),
    [loading, authenticated, login, logout, refresh],
  )

  return <SessionCtx.Provider value={value}>{children}</SessionCtx.Provider>
}

export function useSession(): SessionCtxValue {
  const v = useContext(SessionCtx)
  if (!v) throw new Error('useSession 必须在 SessionProvider 内使用')
  return v
}