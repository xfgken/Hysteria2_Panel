-- HY2 Panel 数据库结构（SQLite）
--
-- 说明（见开发文档 43 / 57 节）：
--   * Panel 只保存“HY2 官方不提供”的历史与自有数据；
--   * 实时流量 / 在线 / 连接仍以官方 Traffic Stats API 为准；
--   * HY2 用户密码因官方 userpass 认证需要明文比对，故必须可逆保存，
--     仓库层负责“绝不通过 API 明文返回”（见 62 节）。

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- ---------------------------------------------------------------------------
-- 面板管理员（Web 登录用，与 HY2 用户无关）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS admins (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,              -- bcrypt
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
);

-- ---------------------------------------------------------------------------
-- HY2 用户（映射官方 userpass 认证）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    username     TEXT    NOT NULL UNIQUE,        -- 即 HY2 认证 ID
    hy2_password TEXT    NOT NULL,               -- 敏感字段：永不通过 API 明文返回
    enabled      INTEGER NOT NULL DEFAULT 1,
    note         TEXT    NOT NULL DEFAULT '',
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- ---------------------------------------------------------------------------
-- 订阅 Token（Clash Meta 订阅，Panel 自有功能）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS subscriptions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token        TEXT    NOT NULL UNIQUE,
    created_at   TEXT    NOT NULL,
    revoked_at   TEXT,                           -- 非空表示已失效
    last_used_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user ON subscriptions(user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_token ON subscriptions(token);

-- ---------------------------------------------------------------------------
-- 流量采样（历史流量，Panel 自有）
-- 存“增量”而非累计快照，便于按任意窗口聚合。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS traffic_samples (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ts       INTEGER NOT NULL,                   -- 采样时刻（Unix 秒，对齐到采样周期）
    tx_delta INTEGER NOT NULL DEFAULT 0,
    rx_delta INTEGER NOT NULL DEFAULT 0,
    UNIQUE(user_id, ts)
);

CREATE INDEX IF NOT EXISTS idx_traffic_user_ts ON traffic_samples(user_id, ts);
CREATE INDEX IF NOT EXISTS idx_traffic_ts ON traffic_samples(ts);

-- ---------------------------------------------------------------------------
-- 配置版本 / 备份
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS config_versions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at    TEXT    NOT NULL,
    hy2_version   TEXT    NOT NULL DEFAULT '',
    panel_version TEXT    NOT NULL DEFAULT '',
    content       TEXT    NOT NULL,              -- 完整 config.yaml
    reason        TEXT    NOT NULL DEFAULT '',   -- backup / apply / rollback / manual
    is_current    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_config_versions_created ON config_versions(created_at);

-- ---------------------------------------------------------------------------
-- 键值设置
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- ---------------------------------------------------------------------------
-- 会话
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT    PRIMARY KEY,              -- 高随机
    admin_id   INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    csrf_token TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    ip         TEXT    NOT NULL DEFAULT '',
    user_agent TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_admin ON sessions(admin_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
-- ---------------------------------------------------------------------------
-- 用户连接事件（接入 / 断开）
-- 官方 Traffic Stats API 的 /online 只给“当前连接数”，
-- Panel 周期性比对快照：0 → N 记“接入”，N → 0 记“断开”。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL,                   -- connect / disconnect
    ts      INTEGER NOT NULL,                   -- Unix 秒
    online  INTEGER NOT NULL DEFAULT 0,         -- 事件发生后的在线连接数
    UNIQUE(user_id, kind, ts)
);
CREATE INDEX IF NOT EXISTS idx_user_events_user_ts ON user_events(user_id, ts);
CREATE INDEX IF NOT EXISTS idx_user_events_ts ON user_events(ts);
