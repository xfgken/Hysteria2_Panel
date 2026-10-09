import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { IconCheck, IconClose, IconError, IconInfo } from './icons'

type ToastKind = 'ok' | 'err' | 'info'

type ToastItem = {
  id: number
  kind: ToastKind
  message: string
}

type ToastCtxValue = {
  toast: (message: string, kind?: ToastKind) => void
}

const ToastCtx = createContext<ToastCtxValue | null>(null)

/** Toast 提供者：所有操作反馈的统一出口（开发文档 64 节）。 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const seq = useRef(0)

  const toast = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = ++seq.current
    setItems((prev) => [...prev, { id, kind, message }])
    const ttl = 2000
    window.setTimeout(() => {
      setItems((prev) => prev.filter((t) => t.id !== id))
    }, ttl)
  }, [])

  const value = useMemo(() => ({ toast }), [toast])

  return (
    <ToastCtx.Provider value={value}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast--${t.kind}`}>
            <span className="toast__icon">
              {t.kind === 'ok' ? (
                <IconCheck size={15} />
              ) : t.kind === 'err' ? (
                <IconError size={15} />
              ) : (
                <IconInfo size={15} />
              )}
            </span>
            <span className="toast__msg">{t.message}</span>
            <button
              type="button"
              className="toast__close"
              onClick={() => setItems((prev) => prev.filter((x) => x.id !== t.id))}
              aria-label="关闭提示"
            >
              <IconClose size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}

export function useToast(): ToastCtxValue {
  const v = useContext(ToastCtx)
  if (!v) throw new Error('useToast 必须在 ToastProvider 内使用')
  return v
}