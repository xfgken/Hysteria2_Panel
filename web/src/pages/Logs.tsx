import { useCallback, useEffect, useRef, useState } from 'react'
import { API, ApiError } from '../api'
import { Page } from '../components/Layout'
import { Card, ConfirmDialog, CopyButton, IssueList, Loading } from '../components/ui'
import { IconRefresh, IconTrash } from '../icons'
import { useToast } from '../toast'

type Source = 'hysteria' | 'panel'

const SOURCES: [Source, string][] = [
  ['hysteria', 'hysteria2 服务'],
  ['panel', 'Panel'],
]

/** 日志级别过滤。 */
const LEVELS: [string, string][] = [
  ['', '全部'],
  ['error', 'error'],
  ['warn', 'warn'],
  ['info', 'info'],
  ['debug', 'debug'],
]

/** 每次读取的日志行数。 */
const LINE_OPTIONS = [100, 300, 1000, 3000]

/** 自动刷新间隔（毫秒）。固定开启，无需手动切换。 */
const REFRESH_INTERVAL = 5000

/** 日志：hysteria2 / Panel / Nginx（开发文档 47 节）。 */
export default function Logs() {
  const [source, setSource] = useState<Source>('hysteria')
  const [content, setContent] = useState('')
  const [target, setTarget] = useState('')
  const [lines, setLines] = useState(300)
  const [level, setLevel] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirmClear, setConfirmClear] = useState(false)
  const [busy, setBusy] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)
  const { toast } = useToast()

  const load = useCallback(
    async (silent = false) => {
      if (!silent) setLoading(true)
      try {
        const res = await API.logs.fetch(source, lines, '', level)
        setContent(res.content)
        setTarget(res.target)
        setError('')
      } catch (err) {
        const msg = err instanceof ApiError ? err.message : '读取日志失败'
        setError(msg)
        if (!silent) toast(msg, 'err')
      } finally {
        setLoading(false)
      }
    },
    [source, lines, level, toast],
  )

  useEffect(() => {
    void load()
  }, [load])

  // 自动刷新：始终开启
  useEffect(() => {
    const timer = window.setInterval(() => void load(true), REFRESH_INTERVAL)
    return () => window.clearInterval(timer)
  }, [load])

  // 自动滚动到底部：始终开启
  useEffect(() => {
    if (boxRef.current) {
      boxRef.current.scrollTop = boxRef.current.scrollHeight
    }
  }, [content])

  const clearLogs = async () => {
    setBusy(true)
    try {
      const res = await API.logs.clear(source)
      toast(res.message || '日志已清空', 'ok')
      setConfirmClear(false)
      await load()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : '清空失败', 'err')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Page title="服务日志" subtitle={target ? `来源：${target}` : '实时查看服务日志'}>
      {error && <IssueList issues={[{ severity: 'warning', field: '日志', message: error }]} />}

      <div className="tabs">
        {SOURCES.map(([k, label]) => (
          <button
            key={k}
            type="button"
            className={source === k ? 'tab tab--active' : 'tab'}
            onClick={() => setSource(k)}
          >
            {label}
          </button>
        ))}
      </div>

      <Card>
        {/* 工具栏：级别 / 显示行数 / 刷新 / 复制 / 清空 */}
        <div className="log-toolbar">
          <div className="log-group">
            <span className="log-group__label">级别</span>
            <div className="tabs" role="group" aria-label="按日志级别过滤">
              {LEVELS.map(([v, label]) => (
                <button
                  key={v || 'all'}
                  type="button"
                  className={level === v ? 'tab tab--active' : 'tab'}
                  onClick={() => setLevel(v)}
                  title={v === '' ? '不过滤，显示全部级别' : `只看 ${v} 级别`}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>

          <div className="log-group">
            <span className="log-group__label">显示行数</span>
            <div className="tabs" role="group" aria-label="每次读取的日志行数">
              {LINE_OPTIONS.map((n) => (
                <button
                  key={n}
                  type="button"
                  className={lines === n ? 'tab tab--active' : 'tab'}
                  onClick={() => setLines(n)}
                  title={`读取最近 ${n} 行`}
                >
                  {n}
                </button>
              ))}
            </div>
          </div>

          <span className="log-toolbar__spacer" />

          <button type="button" className="btn btn--sm" onClick={() => void load()}>
            <IconRefresh size={13} />
            刷新
          </button>
          <CopyButton text={content} label="复制全部" />
          <button type="button" className="btn btn--sm btn--danger" onClick={() => setConfirmClear(true)}>
            <IconTrash size={13} />
            清空日志
          </button>
        </div>

        <div style={{ marginTop: 12 }}>
          {loading ? (
            <Loading text="正在读取日志" />
          ) : (
            <div className="logbox" ref={boxRef}>
              {content.trim() || '（没有匹配的日志）'}
            </div>
          )}
        </div>

        <p className="field__hint" style={{ marginTop: 10 }}>
          自动刷新（每 {REFRESH_INTERVAL / 1000} 秒）与自动滚动默认开启。
          日志来源可在「设置」页配置：<code>journal:单元名</code> 表示通过 journalctl 读取，其它值按文件路径处理。
          清空仅对文件型日志生效；journal 日志由 systemd 自行管理，无法在此删除。
        </p>
      </Card>

      {confirmClear && (
        <ConfirmDialog
          title="清空日志"
          danger
          message={`确认清空「${target}」的全部内容？该操作不可恢复。`}
          confirmText="清空"
          busy={busy}
          onCancel={() => setConfirmClear(false)}
          onConfirm={() => void clearLogs()}
        />
      )}
    </Page>
  )
}