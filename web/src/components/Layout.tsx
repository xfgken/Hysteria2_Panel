import { createContext, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useSession } from '../session'
import { useTheme } from '../theme'
import {
  IconClose,
  IconMonitorHeart,
  IconMoon,
  IconSun,
  IconLink,
  IconLogout,
  IconLogs,
  IconMenu,
  IconServer,
  IconSettings,
} from '../icons'
import { ConfirmDialog } from './ui'

type NavItem = {
  to: string
  label: string
  icon: (p: { size?: number }) => JSX.Element
}

export const NAV_ITEMS: NavItem[] = [
  { to: '/', label: '账号与订阅', icon: IconLink },
  { to: '/server', label: 'Hysteria2 配置', icon: IconServer },
  { to: '/logs', label: '服务日志', icon: IconLogs },
  { to: '/settings', label: '服务状态', icon: IconMonitorHeart },
]

/** 侧边栏的展开状态（供顶栏的切换按钮使用）。 */
type DrawerCtxValue = {
  open: boolean
  setOpen: (v: boolean) => void
  toggle: () => void
}

const DrawerCtx = createContext<DrawerCtxValue>({
  open: false,
  setOpen: () => {},
  toggle: () => {},
})

/** 窄屏判定（与 CSS 断点保持一致）。 */
const isNarrow = () => typeof window !== 'undefined' && window.innerWidth < 768

/**
 * 应用外壳。
 *
 * 侧边栏位于**左侧**，通过顶栏左端的按钮展开/收起：
 *   · 宽屏默认展开，收起后内容区自动占满；
 *   · 窄屏默认收起，展开时为覆盖式抽屉 + 遮罩。
 *   · 同一个按钮：收起时是菜单图标，展开时变成叉号。
 */
export function Layout({ children }: { children: ReactNode }) {
  const narrow = useNarrow()
  const [open, setOpen] = useState(() => !isNarrow())
  const location = useLocation()

  // 切换页面后，窄屏自动收起抽屉
  useEffect(() => {
    if (narrow) setOpen(false)
  }, [narrow, location.pathname])

  // Esc 收起；窄屏展开时锁定背景滚动
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', onKey)
    if (narrow) {
      document.body.style.overflow = open ? 'hidden' : ''
    }
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [open, narrow])

  return (
    <DrawerCtx.Provider value={{ open, setOpen, toggle: () => setOpen(!open) }}>
      <div className={open ? 'app app--open' : 'app'}>
        {/* 遮罩只在「窄屏 + 抽屉展开」时存在：
            否则它会盖住内容区，把输入框等元素的点击全部吃掉。 */}
        {narrow && open && (
          <div className="drawer-backdrop" onClick={() => setOpen(false)} aria-hidden="true" />
        )}
        <div className="main">{children}</div>
        <Rail />
      </div>
    </DrawerCtx.Provider>
  )
}

/**
 * 窄屏判定（与 CSS 断点 767px 保持一致）。
 *
 * 必须响应窗口尺寸变化：手机上横竖屏切换时，若沿用旧的 open 状态，
 * 会出现「CSS 已切到抽屉模式、但状态仍是展开」→ 全屏遮罩挡住所有点击。
 */
function useNarrow() {
  const [narrow, setNarrow] = useState(() => isNarrow())

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 767px)')
    const onChange = () => setNarrow(mq.matches)
    onChange()
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  return narrow
}

/** 侧边栏（位于窗口右侧）。 */
function Rail() {
  const { logout } = useSession()
  const { setOpen } = useContext(DrawerCtx)
  const navigate = useNavigate()
  const [confirmOut, setConfirmOut] = useState(false)

  return (
    <>
      <aside className="rail">
        <div className="rail__head">
          <div className="rail__brand">
            <span className="rail__brand-text">
              <strong>Hysteria2 Panel</strong>
              <span>Hysteria2 管理面板</span>
            </span>
          </div>
          <button
            type="button"
            className="btn btn--icon rail__close"
            onClick={() => setOpen(false)}
            title="收起侧边栏"
            aria-label="收起侧边栏"
          >
            <IconClose size={18} />
          </button>
        </div>

        <nav className="rail__nav">
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              title={item.label}
              className={({ isActive }) => (isActive ? 'navitem navitem--active' : 'navitem')}
            >
              <item.icon size={19} />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </nav>

        <div className="rail__spacer" />

        <div className="rail__foot">
          <div className="rail__foot-row">
            <ThemeSwitch compact />
            <button
              type="button"
              className="railbtn-round railbtn-round--danger"
              title="退出登录"
              aria-label="退出登录"
              onClick={() => setConfirmOut(true)}
            >
              <IconLogout size={19} />
            </button>
          </div>
        </div>
      </aside>

      {confirmOut && (
        <ConfirmDialog
          title="退出登录"
          message="确认退出当前登录？退出后需要重新输入密码。"
          confirmText="退出登录"
          danger
          busy={false}
          onCancel={() => setConfirmOut(false)}
          onConfirm={async () => {
            setConfirmOut(false)
            await logout()
            navigate('/login', { replace: true })
          }}
        />
      )}
    </>
  )
}

/**
 * 主题切换：单个按钮，图标随当前状态变化。
 *
 * 浅色时显示月亮（点一下变深色），深色时显示太阳（点一下变浅色）。
 */
export function ThemeSwitch({ compact }: { compact?: boolean }) {
  const { resolved, setMode } = useTheme()
  const dark = resolved === "dark"
  const label = dark ? "切换到浅色" : "切换到深色"
  return (
    <button
      type="button"
      className="railbtn-round themebtn"
      onClick={() => setMode(dark ? "light" : "dark")}
      title={label}
      aria-label={label}
    >
      {dark ? <IconSun size={19} /> : <IconMoon size={19} />}
    </button>
  )
}

/**
 * 页面容器：顶部栏 + 内容区。
 *
 * 顶栏左端始终有一个侧栏切换按钮：收起时显示菜单图标，展开时变成叉号。
 */
export function Page({
  title,
  subtitle,
  actions,
  children,
}: {
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
  children: ReactNode
}) {
  const { open, toggle } = useContext(DrawerCtx)

  return (
    <>
      <header className="topbar">
        <div className="topbar__title">
          <h1>{title}</h1>
          {subtitle && <div className="topbar__sub">{subtitle}</div>}
        </div>
        <div className="topbar__actions">{actions}</div>
        <button
          type="button"
          className="btn btn--icon rail-toggle"
          onClick={toggle}
          title={open ? '收起侧边栏' : '展开侧边栏'}
          aria-label={open ? '收起侧边栏' : '展开侧边栏'}
          aria-expanded={open}
          >
          {open ? <IconClose size={18} /> : <IconMenu size={18} />}
        </button>
      </header>
      <div className="page">{children}</div>
    </>
  )
}