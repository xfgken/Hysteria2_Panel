package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("记录不存在")

// ---------------------------------------------------------------------------
// 用户
// ---------------------------------------------------------------------------

// User 是一条 HY2 用户记录。
//
// 注意：HY2Password 属于敏感字段，调用方不得将其直接写入 API 响应。
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	HY2Password string `json:"-"`
	Enabled     bool   `json:"enabled"`
	Note        string `json:"note"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// CreateUser 新建用户。
func (d *DB) CreateUser(username, password, note string) (*User, error) {
	if username == "" {
		return nil, errors.New("用户名不能为空")
	}
	ts := now()
	res, err := d.Exec(
		`INSERT INTO users (username, hy2_password, enabled, note, created_at, updated_at)
		 VALUES (?, ?, 1, ?, ?, ?)`,
		username, password, note, ts, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	id, _ := res.LastInsertId()
	return d.GetUserByID(id)
}

// GetUserByID 按 ID 查询用户。
func (d *DB) GetUserByID(id int64) (*User, error) {
	return d.scanUser(d.QueryRow(
		`SELECT id, username, hy2_password, enabled, note, created_at, updated_at
		 FROM users WHERE id = ?`, id))
}

// GetUserByUsername 按用户名查询用户。
func (d *DB) GetUserByUsername(username string) (*User, error) {
	return d.scanUser(d.QueryRow(
		`SELECT id, username, hy2_password, enabled, note, created_at, updated_at
		 FROM users WHERE username = ?`, username))
}

func (d *DB) scanUser(row *sql.Row) (*User, error) {
	var u User
	var enabled int
	err := row.Scan(&u.ID, &u.Username, &u.HY2Password, &enabled, &u.Note, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Enabled = enabled != 0
	return &u, nil
}

// ListUsers 分页查询用户；query 非空时按用户名模糊匹配。
func (d *DB) ListUsers(query string, limit, offset int) ([]*User, error) {
	if limit <= 0 {
		limit = 50
	}
	sqlStr := `SELECT id, username, hy2_password, enabled, note, created_at, updated_at
	           FROM users`
	args := []any{}
	if query != "" {
		sqlStr += ` WHERE username LIKE ?`
		args = append(args, "%"+query+"%")
	}
	sqlStr += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := d.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		var u User
		var enabled int
		if err := rows.Scan(&u.ID, &u.Username, &u.HY2Password, &enabled, &u.Note, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		out = append(out, &u)
	}
	return out, rows.Err()
}

// CountUsers 返回用户总数。
func (d *DB) CountUsers() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// UpdateUserPassword 修改用户密码。
func (d *DB) UpdateUserPassword(id int64, password string) error {
	_, err := d.Exec(`UPDATE users SET hy2_password = ?, updated_at = ? WHERE id = ?`,
		password, now(), id)
	return err
}

// UpdateUserNote 修改备注。
func (d *DB) UpdateUserNote(id int64, note string) error {
	_, err := d.Exec(`UPDATE users SET note = ?, updated_at = ? WHERE id = ?`,
		note, now(), id)
	return err
}

// SetUserEnabled 启用 / 禁用用户。
func (d *DB) SetUserEnabled(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := d.Exec(`UPDATE users SET enabled = ?, updated_at = ? WHERE id = ?`, v, now(), id)
	return err
}

// DeleteUser 删除用户（订阅与流量采样随之级联删除）。
func (d *DB) DeleteUser(id int64) error {
	_, err := d.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// EnabledUserpass 返回所有“启用”账号的 userpass 映射，用于同步到 HY2 配置。
//
// 官方 userpass 认证本身就是一张表、支持多账号，因此面板里每个启用账号
// 都必须写进配置 —— 只写第一个会导致其余账号无法通过认证。
func (d *DB) EnabledUserpass() (map[string]string, error) {
	rows, err := d.Query(`SELECT username, hy2_password FROM users WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var u, p string
		if err := rows.Scan(&u, &p); err != nil {
			return nil, err
		}
		out[u] = p
	}
	return out, rows.Err()
}

// SingleUser 返回唯一的用户（按 id 升序第一个）；不存在时返回 nil, nil。
//
// 本面板定位为「单用户」：所有用户管理都围绕这一个账号进行。
func (d *DB) SingleUser() (*User, error) {
	u, err := d.scanUser(d.QueryRow(
		`SELECT id, username, hy2_password, enabled, note, created_at, updated_at
		 FROM users ORDER BY id LIMIT 1`))
	if err == ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SetUsername 修改用户名。
func (d *DB) SetUsername(id int64, username string) error {
	_, err := d.Exec(`UPDATE users SET username = ?, updated_at = ? WHERE id = ?`,
		username, now(), id)
	return err
}

// usernameChars 是用户名允许使用的字符：大小写字母与数字。
const usernameChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// UsernamePrefix 是自动生成用户名的固定前缀。
const UsernamePrefix = "hysteria2-"

// RandomUsername 生成形如 hysteria2-xxxxxx 的用户名（6 位随机大小写字母与数字）。
func RandomUsername() (string, error) {
	const n = 6
	buf := make([]byte, n)
	max := big.NewInt(int64(len(usernameChars)))
	for i := range buf {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("生成随机用户名失败: %w", err)
		}
		buf[i] = usernameChars[idx.Int64()]
	}
	return UsernamePrefix + string(buf), nil
}

// UsernameSuffixMaxLen 是账号名后缀的长度上限（前缀 + 后缀不超过官方与 UI 的舒适范围）。
const UsernameSuffixMaxLen = 32

// ValidateUsernameSuffix 校验自定义账号名后缀。
//
// 规则：长度 1~32，且只能是大写字母、小写字母或数字（与自动生成的格式保持一致）。
func ValidateUsernameSuffix(s string) error {
	if s == "" {
		return fmt.Errorf("名称后缀不能为空")
	}
	if len(s) > UsernameSuffixMaxLen {
		return fmt.Errorf("名称后缀最长 %d 位", UsernameSuffixMaxLen)
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return fmt.Errorf("名称后缀只能包含大小写字母和数字")
		}
	}
	return nil
}

// RandomUsernameSuffix 生成一个 6 位随机后缀。
func RandomUsernameSuffix() (string, error) {
	username, err := RandomUsername()
	if err != nil {
		return "", err
	}
	return username[len(UsernamePrefix):], nil
}

// ---------------------------------------------------------------------------
// 管理员
// ---------------------------------------------------------------------------

// Admin 是面板管理员。
type Admin struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// CreateAdmin 新建管理员（passwordHash 由调用方用 bcrypt 生成）。
func (d *DB) CreateAdmin(username, passwordHash string) (*Admin, error) {
	ts := now()
	res, err := d.Exec(
		`INSERT INTO admins (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, ts, ts,
	)
	if err != nil {
		return nil, fmt.Errorf("创建管理员失败: %w", err)
	}
	id, _ := res.LastInsertId()
	a := &Admin{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: ts, UpdatedAt: ts}
	return a, nil
}

// GetAdminByUsername 按用户名查询管理员。
func (d *DB) GetAdminByUsername(username string) (*Admin, error) {
	var a Admin
	err := d.QueryRow(
		`SELECT id, username, password_hash, created_at, updated_at FROM admins WHERE username = ?`,
		username,
	).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAdminByID 按 ID 查询管理员。
func (d *DB) GetAdminByID(id int64) (*Admin, error) {
	var a Admin
	err := d.QueryRow(
		`SELECT id, username, password_hash, created_at, updated_at FROM admins WHERE id = ?`,
		id,
	).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CountAdmins 返回管理员数量。
func (d *DB) CountAdmins() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

// UpdateAdminPassword 修改管理员密码哈希。
func (d *DB) UpdateAdminPassword(id int64, passwordHash string) error {
	_, err := d.Exec(`UPDATE admins SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, now(), id)
	return err
}

// ---------------------------------------------------------------------------
// 设置
// ---------------------------------------------------------------------------

// SetSetting 写入键值设置（不存在则插入）。
func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// GetSetting 读取键值设置；不存在返回空串。
func (d *DB) GetSetting(key string) (string, error) {
	var v string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// ---------------------------------------------------------------------------
// 会话
// ---------------------------------------------------------------------------

// Session 是一条登录会话。
type Session struct {
	ID        string
	AdminID   int64
	CSRFToken string
	CreatedAt string
	ExpiresAt string
	IP        string
	UserAgent string
}

// CreateSession 创建会话，返回会话 ID 与 CSRF Token。
func (d *DB) CreateSession(adminID int64, ttl time.Duration, ip, ua string) (*Session, error) {
	id, err := RandomToken(32)
	if err != nil {
		return nil, err
	}
	csrf, err := RandomToken(32)
	if err != nil {
		return nil, err
	}
	s := &Session{
		ID:        id,
		AdminID:   adminID,
		CSRFToken: csrf,
		CreatedAt: time.Now().Format(time.RFC3339),
		ExpiresAt: time.Now().Add(ttl).Format(time.RFC3339),
		IP:        ip,
		UserAgent: ua,
	}
	_, err = d.Exec(
		`INSERT INTO sessions (id, admin_id, csrf_token, created_at, expires_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.AdminID, s.CSRFToken, s.CreatedAt, s.ExpiresAt, s.IP, s.UserAgent,
	)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// GetSession 读取未过期会话。
func (d *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := d.QueryRow(
		`SELECT id, admin_id, csrf_token, created_at, expires_at, ip, user_agent
		 FROM sessions WHERE id = ? AND expires_at > ?`,
		id, time.Now().Format(time.RFC3339),
	).Scan(&s.ID, &s.AdminID, &s.CSRFToken, &s.CreatedAt, &s.ExpiresAt, &s.IP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// DeleteSession 删除会话（登出）。
func (d *DB) DeleteSession(id string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteAdminSessions 删除某管理员的全部会话（例如改密后强制重新登录）。
func (d *DB) DeleteAdminSessions(adminID int64) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE admin_id = ?`, adminID)
	return err
}

// CleanupExpiredSessions 清理过期会话。
func (d *DB) CleanupExpiredSessions() error {
	_, err := d.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Format(time.RFC3339))
	return err
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

// RandomToken 生成 n 字节的密码学安全随机 Token（十六进制编码）。
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// shortTokenAlphabet 是短 Token 的字符集：小写字母 + 数字。
const shortTokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomShortToken 生成 n 位的短 Token（小写字母 + 数字）。
//
// 订阅地址里的 Token 用它：比十六进制短一半，便于手抄与辨认。
// 采用拒绝采样，避免 256 不能整除 36 带来的分布偏差。
func RandomShortToken(n int) (string, error) {
	if n <= 0 {
		n = 20
	}
	// 36 的 7 倍是 252，只接受 < 252 的字节即可保证均匀。
	const limit = 252

	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成随机数失败: %w", err)
		}
		for _, b := range buf {
			if int(b) < limit {
				out = append(out, shortTokenAlphabet[int(b)%len(shortTokenAlphabet)])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out), nil
}