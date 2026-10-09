import { useCallback, useEffect, useState } from 'react'
import QRCode from 'qrcode'
import { API, ApiError } from '../api'
import { CopyButton, Dialog, Loading } from './ui'
import { IconQr } from '../icons'
import { useToast } from '../toast'

/** 生成并展示二维码。 */
export function QrBox({ text, caption }: { text: string; caption: string }) {
  const [src, setSrc] = useState('')

  useEffect(() => {
    let cancelled = false
    if (!text) {
      setSrc('')
      return
    }
    // margin: 0：不加自带白边，白边改由 CSS 的固定内边距提供，
      // 这样两个二维码的白边一样厚、图案区一样大（数据长的那个不会显小）
      QRCode.toDataURL(text, { margin: 0, width: 640, errorCorrectionLevel: 'M' })
      .then((d) => {
        if (!cancelled) setSrc(d)
      })
      .catch(() => setSrc(''))
    return () => {
      cancelled = true
    }
  }, [text])

  return (
    <div className="share-qr">
      {src ? (
        <div className="qr">
          <img src={src} alt={caption} />
        </div>
      ) : (
        <div className="qr" style={{ background: 'var(--surface-3)' }}>
          <div className="skeleton" style={{ width: 176, height: 176 }} />
        </div>
      )}
      <span className="faint" style={{ fontSize: 11.5 }}>
        {caption}
      </span>
    </div>
  )
}

/**
 * 单条订阅链接。
 *
 * 交互固定为：链接（可点选 / 长按全选）+ 旁边的复制按钮，
 * 下方一个「二维码」按钮，点开才以弹窗显示二维码。
 */
function LinkEntry({
  title,
  url,
}: {
  title: string
  url: string
}) {
  const [qrOpen, setQrOpen] = useState(false)

  return (
    <div className="link-entry">
      {/* 二维码 / 复制按钮在左，类型名跟在后面 */}
      <div className="link-entry__head">
        <span className="link-entry__name">{title}</span>
        <span className="link-entry__actions">
          <button
            type="button"
            className="btn btn--sm"
            onClick={() => setQrOpen(true)}
            disabled={!url}
          >
            <IconQr size={14} />
            二维码
          </button>
          <CopyButton text={url} label="复制" />
        </span>
      </div>


      {/* 完整显示链接（自动换行 + 下划线），点一下可整条选中 */}
      <div className="link-entry__url" title={url}>
        {url || '—'}
      </div>

      {qrOpen && (
        <Dialog
          title={`${title}二维码`}
          noBackdropClose
          onClose={() => setQrOpen(false)}
        >
          <div className="qr-dialog">
            <QrBox text={url} caption={title} />
          </div>
        </Dialog>
      )}
    </div>
  )
}

/**
 * 订阅分享面板（单账号）。
 *
 * 两种分享形式放在同一个区域里，顺序固定：
 *   · Hysteria 2 URI（官方 scheme，客户端可直接导入）
 *   · Clash Meta 订阅地址（Panel 扩展能力，Marzban 风格订阅）
 */
/**
 * 单个账号的订阅链接缓存。
 *
 * 目的：让“展开”的那一瞬间，两个链接区域已经是现成的，
 * 而不是“先展开、再加载”（先看到骨架再填内容）。
 */
type ShareData = { sub: { url: string; token: string }; uri: string; at: number }

/** 预取缓存的保鲜期：超过就重新取一次（防止 token 轮换后一直用旧链接） */
const SHARE_CACHE_TTL = 60_000

const shareCache = new Map<number, ShareData>()
const shareInflight = new Map<number, Promise<ShareData>>()

/** 拉取某账号的两个链接（带去重：预取与展开共用同一次请求）。 */
function fetchShare(userId: number): Promise<ShareData> {
  const running = shareInflight.get(userId)
  if (running) return running
  const p = Promise.all([API.users.subscription(userId), API.users.uri(userId)])
    .then(([s, u]) => {
      const data: ShareData = { sub: { url: s.url, token: s.token }, uri: u.uri, at: Date.now() }
      shareCache.set(userId, data)
      return data
    })
    .finally(() => {
      shareInflight.delete(userId)
    })
  shareInflight.set(userId, p)
  return p
}

/**
 * 预取：账号列表加载完就调用一次，
 * 之后点展开直接读缓存，不会出现“展开后再加载”。
 */
export function prefetchShare(userId: number): void {
  const hit = shareCache.get(userId)
  if (hit && Date.now() - hit.at < SHARE_CACHE_TTL) return
  if (shareInflight.has(userId)) return
  void fetchShare(userId).catch(() => {
    /* 预取失败不提示，展开时会自己再试一次 */
  })
}

export function SharePanel({
  userId,
  mode = 'all',
  reloadKey = 0,
}: {
  userId: number
  /** all：两种都显示；uri：只显示 Hysteria2 节点；clash：只显示订阅 */
  mode?: 'all' | 'uri' | 'clash'
  /** 变化时强制重新拉取链接（例如名称被重新生成后） */
  reloadKey?: number
}) {
  // 预取过就用缓存初始化：展开瞬间就是“已经加载好的两个链接区域”
  const cached = shareCache.get(userId)
  const [sub, setSub] = useState<{ url: string; token: string } | null>(cached?.sub ?? null)
  const [uri, setUri] = useState(cached?.uri ?? '')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(!cached)
  const { toast } = useToast()

  const load = useCallback(async () => {
    // 已有缓存时静默刷新，不再闪一下“正在生成订阅信息”
    if (!shareCache.has(userId)) setLoading(true)
    try {
      const data = await fetchShare(userId)
      setSub(data.sub)
      setUri(data.uri)
      setError('')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '读取订阅信息失败')
    } finally {
      setLoading(false)
    }
  }, [userId])

  useEffect(() => {
    // 始终后台刷新一次：
    //   · 有预取缓存 → 先把两个链接区域显示出来，再静默更新（不闪“正在生成订阅信息”）
    //   · 没缓存 → 正常走加载态
    // 这样既满足“展开前就加载好”，又不会因为缓存而一直用旧的 Token 链接
    void load()
  }, [load, reloadKey, userId])

  if (loading) return <Loading text="正在生成订阅信息" />

  // 自签名证书时，URI 会自带 insecure=1 与 pinSHA256
  const selfSigned = uri.includes('insecure=1')
  const showURI = mode === 'all' || mode === 'uri'
  const showClash = mode === 'all' || mode === 'clash'

  return (
    <>
      {error && (
        <div className="issue issue--warning">
          <span>{error}</span>
        </div>
      )}

      {selfSigned && (
        <div className="issue issue--warning">
          <span>
            服务端当前使用<strong>自签名证书</strong>，因此链接中已自动带上
            <code> insecure=1</code> 与 <code>pinSHA256</code>（证书指纹）。
            客户端可直接导入，无需手动配置；换成正式证书（ACME）后该参数会自动消失。
          </span>
        </div>
      )}

      <div className="links">
        {showURI && (
          <LinkEntry title="Hysteria2" url={uri} />
        )}

        {showClash && (
          <LinkEntry title="Clash" url={sub?.url || ''} />
        )}
      </div>
    </>
  )
}