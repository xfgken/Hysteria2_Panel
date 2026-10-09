import { createPortal } from 'react-dom'
import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import type { Issue } from '../api'
import { IconCheck, IconChevronDown, IconClose, IconCopy, IconInfo, IconWarning } from '../icons'

/* ------------------------------- 布局块 ------------------------------- */

export function Card({
  title,
  extra,
  children,
  flush,
}: {
  title?: ReactNode
  extra?: ReactNode
  children: ReactNode
  flush?: boolean
}) {
  return (
    <section className="card">
      {title !== undefined && (
        <header className="card__head">
          <h3>{title}</h3>
          {extra}
        </header>
      )}
      <div className={flush ? 'card__body card__body--flush' : 'card__body'}>{children}</div>
    </section>
  )
}

export function StatCard({
  label,
  value,
  unit,
  foot,
  icon,
  onClick,
}: {
  label: string
  value: ReactNode
  unit?: string
  foot?: ReactNode
  icon?: ReactNode
  onClick?: () => void
}) {
  const body = (
    <>
      <div className="stat__top">
        {icon && <span className="stat__icon">{icon}</span>}
        <span>{label}</span>
      </div>
      <div className="stat__value">
        {value}
        {unit && <span className="stat__unit">{unit}</span>}
      </div>
      {foot && <div className="stat__foot">{foot}</div>}
    </>
  )

  if (onClick) {
    return (
      <button type="button" className="stat" onClick={onClick}>
        {body}
      </button>
    )
  }
  return <div className="stat">{body}</div>
}

export function Field({
  label,
  hint,
  error,
  children,
  full,
}: {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  children: ReactNode
  full?: boolean
}) {
  return (
    <label className={full ? 'field form-grid--full' : 'field'}>
      <span className="field__label">{label}</span>
      {children}
      {hint && <span className="field__hint">{hint}</span>}
      {error && <span className="field__error">{error}</span>}
    </label>
  )
}

export function Badge({
  kind = 'plain',
  children,
}: {
  kind?: 'plain' | 'ok' | 'warn' | 'danger'
  children: ReactNode
}) {
  return <span className={`badge badge--${kind}`}>{children}</span>
}

export function Switch({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label?: string }) {
  return (
    <div className="switch-row">
      <button
        type="button"
        className="switch"
        data-on={on ? 'true' : 'false'}
        role="switch"
        aria-checked={on}
        aria-label={label}
        onClick={() => onChange(!on)}
      >
        <span className="switch__knob" />
      </button>
      {label && <span className="muted">{label}</span>}
    </div>
  )
}

/* ------------------------------ 状态反馈 ------------------------------ */

export function Empty({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="empty">
      <span className="empty__icon">
        <IconInfo size={22} />
      </span>
      <strong>{title}</strong>
      {hint && <span className="muted">{hint}</span>}
      {action && <div style={{ marginTop: 6 }}>{action}</div>}
    </div>
  )
}

export function Loading({ text = '加载中' }: { text?: string }) {
  return (
    <div className="loading-row">
      <span className="spinner" />
      <span>{text}</span>
    </div>
  )
}

export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: 16 }}>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton" style={{ height: 18, width: `${100 - i * 9}%` }} />
      ))}
    </div>
  )
}

export function IssueList({ issues }: { issues: Issue[] }) {
  if (!issues || issues.length === 0) return null
  return (
    <div className="issues">
      {issues.map((it, i) => (
        <div key={`${it.field}-${i}`} className={`issue issue--${it.severity}`}>
          {it.severity === 'error' ? <IconWarning size={15} /> : <IconInfo size={15} />}
          <span>
            <b>{it.field}</b> · {it.message}
          </span>
        </div>
      ))}
    </div>
  )
}

/* ------------------------------ 自绘下拉选择 ------------------------------ */

export type SelectOption = { value: string; label: string }

/**
 * Select 是应用内自绘的下拉选择器。
 *
 * 刻意不使用浏览器的 <select>：原生控件外观无法与整体设计统一，
 * 且在移动端会弹出系统选择器。这里用按钮 + 浮层实现，行为一致、样式可控。
 */
export function Select({
  value,
  options,
  onChange,
  allowEmpty,
  placeholder = '（未设置）',
}: {
  value: string
  options: SelectOption[]
  onChange: (v: string) => void
  allowEmpty?: boolean
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)

  // 点击外部 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDocDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDocDown)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDocDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  const current = options.find((o) => o.value === value)
  const display = current ? current.label : placeholder
  const isPlaceholder = !current

  const items: SelectOption[] = allowEmpty
    ? [{ value: '', label: placeholder }, ...options]
    : options

  return (
    <div className="select-box" ref={boxRef}>
      <button
        type="button"
        className="select-box__trigger"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen((v) => !v)}
      >
        <span className={isPlaceholder ? 'select-box__value is-placeholder' : 'select-box__value'}>{display}</span>
        <IconChevronDown size={16} className="select-box__caret" />
      </button>

      {open && (
        <div className="select-menu" role="listbox">
          {items.map((o) => (
            <button
              key={o.value || '__empty__'}
              type="button"
              role="option"
              aria-selected={o.value === value}
              className={o.value === value ? 'select-menu__item is-active' : 'select-menu__item'}
              onClick={() => {
                onChange(o.value)
                setOpen(false)
              }}
            >
              <span>{o.label}</span>
              {o.value === value && <IconCheck size={14} />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/* -------------------------------- 弹层 -------------------------------- */

export function Dialog({
  title,
  children,
  footer,
  onClose,
  wide,
  noBackdropClose,
  headExtra,
}: {
  title: ReactNode
  children: ReactNode
  footer?: ReactNode
  onClose: () => void
  wide?: boolean
  noBackdropClose?: boolean
  /** 标题右侧、关闭按钮左侧的附加操作（如复制） */
  headExtra?: ReactNode
}) {
  const id = useId()
  const [closing, setClosing] = useState(false)
  const closingRef = useRef(false)
  const closeTimer = useRef<number | undefined>(undefined)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const requestClose = () => {
    if (closingRef.current) return
    closingRef.current = true
    setClosing(true)
    closeTimer.current = window.setTimeout(
      () => onCloseRef.current(), 260)
  }
  useEffect(() => () => {
    if (closeTimer.current !== undefined) {
      window.clearTimeout(closeTimer.current)
    }
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') requestClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })


  // 用 Portal 挂到 body：弹窗不会被展开区的 overflow / 动画裁到
  // （否则在某些浏览器里 fixed 会被限制在展开行内部）
  return createPortal(
    <div
      className={closing ? 'overlay overlay--closing' : 'overlay'}
      onMouseDown={(e) => {
        if (noBackdropClose) return
        if (e.target === e.currentTarget) requestClose()
      }}
    >
      <div
        className={
          (wide ? 'dialog dialog--wide' : 'dialog') +
          (closing ? ' dialog--closing' : '')
        }
        role="dialog"
        aria-modal="true"
        aria-labelledby={id}
      >
        <header className="dialog__head">
          <h3 id={id}>{title}</h3>
          <div className="dialog__actions">
            {headExtra}
            <button
              type="button"
              className="btn btn--ghost btn--icon dialog__close"
              onClick={requestClose}
              aria-label="关闭"
            >
              <IconClose size={16} />
            </button>
          </div>
        </header>
        <div className="dialog__body">{children}</div>
        {footer && <footer className="dialog__foot">{footer}</footer>}
      </div>
    </div>,
    document.body,
  )
}

/** 通用二次确认（开发文档 64 节要求危险操作必须二次确认）。 */
export function ConfirmDialog({
  title,
  message,
  confirmText = '确认',
  danger,
  onConfirm,
  onCancel,
  busy,
}: {
  title: string
  message: ReactNode
  confirmText?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
  busy?: boolean
}) {
  return (
    <Dialog
      title={title}
      onClose={onCancel}
      footer={
        <>
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>
            取消
          </button>
          <button
            type="button"
            className={danger ? 'btn btn--danger' : 'btn btn--primary'}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy && <span className="spinner" />}
            {confirmText}
          </button>
        </>
      }
    >
      <p style={{ fontSize: 13.5 }}>{message}</p>
    </Dialog>
  )
}

/* ------------------------------ 小组件 ------------------------------ */

export function CopyButton({ text, label = '复制' }: { text: string; label?: string }) {
  const [done, setDone] = useState(false)

  const copy = async () => {
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
      setDone(true)
      window.setTimeout(() => setDone(false), 1600)
    } catch {
      setDone(false)
    }
  }

  return (
    <button type="button" className="btn btn--sm" onClick={copy}>
      {done ? <IconCheck size={14} /> : <IconCopy size={14} />}
      {done ? '已复制' : label}
    </button>
  )
}

/** 流量折线图：极细描边、无填充、无装饰（线条风）。 */
export function Sparkline({ points }: { points: { ts: number; tx: number; rx: number }[] }) {
  if (!points || points.length < 2) {
    return (
      <div className="chart" style={{ display: 'grid', placeItems: 'center' }}>
        <span className="muted">数据不足，等待采样</span>
      </div>
    )
  }

  const max = Math.max(1, ...points.map((p) => Math.max(p.tx, p.rx)))
  const n = points.length
  const line = (key: 'tx' | 'rx') =>
    points
      .map((p, i) => `${(i / (n - 1)) * 100},${100 - (p[key] / max) * 100}`)
      .join(' ')

  return (
    <div className="chart">
      <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-label="流量趋势">
        <line x1="0" y1="99.4" x2="100" y2="99.4" stroke="var(--line-color)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        <polyline
          points={line('rx')}
          fill="none"
          stroke="var(--primary)"
          strokeWidth="1.6"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
        <polyline
          points={line('tx')}
          fill="none"
          stroke="var(--text-faint)"
          strokeWidth="1.4"
          strokeDasharray="4 3"
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    </div>
  )
}

export function Legend() {
  return (
    <div className="row row--tight" style={{ fontSize: 11.5 }}>
      <span className="row row--tight" style={{ gap: 5 }}>
        <span style={{ width: 14, height: 2, background: 'var(--primary)', borderRadius: 2 }} />
        <span className="faint">下行</span>
      </span>
      <span className="row row--tight" style={{ gap: 5, marginLeft: 10 }}>
        <span
          style={{
            width: 14,
            height: 0,
            borderTop: '2px dashed var(--text-faint)',
          }}
        />
        <span className="faint">上行</span>
      </span>
    </div>
  )
}