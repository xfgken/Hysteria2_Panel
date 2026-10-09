package db

// ---------------------------------------------------------------------------
// 订阅
// ---------------------------------------------------------------------------

// Subscription 是一条订阅 Token 记录。
type Subscription struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	Token      string `json:"token"`
	CreatedAt  string `json:"createdAt"`
	RevokedAt  string `json:"revokedAt,omitempty"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

// CreateSubscription 为用户创建新的订阅 Token（旧 Token 一并作废）。
//
// Token 为 20 位小写字母 + 数字（36^20 种组合，足够抗枚举），
// 且比十六进制短一半，方便手抄。
func (d *DB) CreateSubscription(userID int64) (*Subscription, error) {
	if err := d.RevokeSubscriptions(userID); err != nil {
		return nil, err
	}
	token, err := RandomShortToken(20)
	if err != nil {
		return nil, err
	}
	res, err := d.Exec(
		`INSERT INTO subscriptions (user_id, token, created_at) VALUES (?, ?, ?)`,
		userID, token, now(),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Subscription{ID: id, UserID: userID, Token: token, CreatedAt: now()}, nil
}

// RevokeSubscriptions 作废某用户的全部有效 Token。
func (d *DB) RevokeSubscriptions(userID int64) error {
	_, err := d.Exec(
		`UPDATE subscriptions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		now(), userID,
	)
	return err
}

// RotateAllTokens 为所有「启用」用户重新生成订阅 Token（旧 Token 立即作废）。
//
// 面板用它实现「配置一改、订阅地址就换」：
// 每次保存/应用服务器配置后自动执行，因此不再需要界面上的「重置 Token」按钮。
//
// 注意：Clash 订阅地址会因此变化，用旧地址拉取的客户端需要重新导入；
// Hysteria2 节点 URI 不含 Token，不受影响。
//
// 返回成功轮换的账号数量。
func (d *DB) RotateAllTokens() (int, error) {
	rows, err := d.Query(`SELECT id FROM users WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	rotated := 0
	for _, id := range ids {
		if _, err := d.CreateSubscription(id); err != nil {
			return rotated, err
		}
		rotated++
	}
	return rotated, nil
}

// ActiveSubscription 返回用户当前有效的订阅 Token；不存在返回 nil。
func (d *DB) ActiveSubscription(userID int64) (*Subscription, error) {
	var s Subscription
	var revoked, lastUsed *string
	err := d.QueryRow(
		`SELECT id, user_id, token, created_at, revoked_at, last_used_at
		 FROM subscriptions WHERE user_id = ? AND revoked_at IS NULL
		 ORDER BY id DESC LIMIT 1`,
		userID,
	).Scan(&s.ID, &s.UserID, &s.Token, &s.CreatedAt, &revoked, &lastUsed)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	if revoked != nil {
		s.RevokedAt = *revoked
	}
	if lastUsed != nil {
		s.LastUsedAt = *lastUsed
	}
	return &s, nil
}

// SubscriptionByToken 按 Token 查询有效订阅。
func (d *DB) SubscriptionByToken(token string) (*Subscription, error) {
	var s Subscription
	err := d.QueryRow(
		`SELECT id, user_id, token, created_at FROM subscriptions
		 WHERE token = ? AND revoked_at IS NULL`,
		token,
	).Scan(&s.ID, &s.UserID, &s.Token, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// TouchSubscription 记录订阅最近一次使用时间。
func (d *DB) TouchSubscription(token string) {
	_, _ = d.Exec(`UPDATE subscriptions SET last_used_at = ? WHERE token = ?`, now(), token)
}

// ---------------------------------------------------------------------------
// 流量采样
// ---------------------------------------------------------------------------

// TrafficPoint 是某用户某一时间点的流量增量。
type TrafficPoint struct {
	TS int64 `json:"ts"`
	Tx int64 `json:"tx"`
	Rx int64 `json:"rx"`
}

// InsertSample 写入一条采样（同一 user+ts 覆盖）。
func (d *DB) InsertSample(userID int64, ts int64, txDelta, rxDelta int64) error {
	_, err := d.Exec(
		`INSERT INTO traffic_samples (user_id, ts, tx_delta, rx_delta) VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, ts) DO UPDATE SET
		   tx_delta = tx_delta + excluded.tx_delta,
		   rx_delta = rx_delta + excluded.rx_delta`,
		userID, ts, txDelta, rxDelta,
	)
	return err
}

// TrafficTotals 返回区间内的上下行总量。
func (d *DB) TrafficTotals(userID int64, since, until int64) (tx, rx int64, err error) {
	err = d.QueryRow(
		`SELECT COALESCE(SUM(tx_delta),0), COALESCE(SUM(rx_delta),0)
		 FROM traffic_samples WHERE user_id = ? AND ts >= ? AND ts < ?`,
		userID, since, until,
	).Scan(&tx, &rx)
	return
}

// AllTrafficTotals 返回区间内所有用户的总量（仪表盘用）。
func (d *DB) AllTrafficTotals(since, until int64) (tx, rx int64, err error) {
	err = d.QueryRow(
		`SELECT COALESCE(SUM(tx_delta),0), COALESCE(SUM(rx_delta),0)
		 FROM traffic_samples WHERE ts >= ? AND ts < ?`,
		since, until,
	).Scan(&tx, &rx)
	return
}

// AllTrafficSeries 返回全局按时间聚合的曲线（升序）。
//
// limit 为最大点数（取区间内最近 limit 个采样点）。
func (d *DB) AllTrafficSeries(since int64, limit int) ([]TrafficPoint, error) {
	if limit <= 0 {
		limit = 96
	}
	rows, err := d.Query(
		`SELECT ts, SUM(tx_delta) AS tx, SUM(rx_delta) AS rx
		 FROM traffic_samples WHERE ts >= ?
		 GROUP BY ts ORDER BY ts DESC LIMIT ?`,
		since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.TS, &p.Tx, &p.Rx); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 反转为升序，便于前端直接绘制。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// PruneSamples 删除早于 keepBefore 的采样（由采样器定期调用）。
func (d *DB) PruneSamples(keepBefore int64) error {
	_, err := d.Exec(`DELETE FROM traffic_samples WHERE ts < ?`, keepBefore)
	return err
}

// ---------------------------------------------------------------------------
// 配置版本 / 备份
// ---------------------------------------------------------------------------

// ConfigVersion 是一次配置备份。
type ConfigVersion struct {
	ID           int64  `json:"id"`
	CreatedAt    string `json:"createdAt"`
	HY2Version   string `json:"hy2Version"`
	PanelVersion string `json:"panelVersion"`
	Content      string `json:"content,omitempty"`
	Reason       string `json:"reason"`
	IsCurrent    bool   `json:"isCurrent"`
}

// SaveConfigVersion 保存一次配置备份并重置 current 标记。
func (d *DB) SaveConfigVersion(content, hy2Version, panelVersion, reason string) (*ConfigVersion, error) {
	if _, err := d.Exec(`UPDATE config_versions SET is_current = 0`); err != nil {
		return nil, err
	}
	v := &ConfigVersion{
		CreatedAt:    now(),
		HY2Version:   hy2Version,
		PanelVersion: panelVersion,
		Content:      content,
		Reason:       reason,
		IsCurrent:    true,
	}
	res, err := d.Exec(
		`INSERT INTO config_versions (created_at, hy2_version, panel_version, content, reason, is_current)
		 VALUES (?, ?, ?, ?, ?, 1)`,
		v.CreatedAt, v.HY2Version, v.PanelVersion, v.Content, v.Reason,
	)
	if err != nil {
		return nil, err
	}
	v.ID, _ = res.LastInsertId()
	return v, nil
}

// ListConfigVersions 返回备份列表（不含内容，避免响应过大）。
func (d *DB) ListConfigVersions(limit int) ([]*ConfigVersion, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(
		`SELECT id, created_at, hy2_version, panel_version, reason, is_current
		 FROM config_versions ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ConfigVersion
	for rows.Next() {
		var v ConfigVersion
		var cur int
		if err := rows.Scan(&v.ID, &v.CreatedAt, &v.HY2Version, &v.PanelVersion, &v.Reason, &cur); err != nil {
			return nil, err
		}
		v.IsCurrent = cur != 0
		out = append(out, &v)
	}
	return out, rows.Err()
}

// GetConfigVersion 读取某次备份（含内容）。
func (d *DB) GetConfigVersion(id int64) (*ConfigVersion, error) {
	var v ConfigVersion
	var cur int
	err := d.QueryRow(
		`SELECT id, created_at, hy2_version, panel_version, content, reason, is_current
		 FROM config_versions WHERE id = ?`, id,
	).Scan(&v.ID, &v.CreatedAt, &v.HY2Version, &v.PanelVersion, &v.Content, &v.Reason, &cur)
	if err != nil {
		return nil, err
	}
	v.IsCurrent = cur != 0
	return &v, nil
}

// DeleteConfigVersion 删除某次备份。
func (d *DB) DeleteConfigVersion(id int64) error {
	_, err := d.Exec(`DELETE FROM config_versions WHERE id = ?`, id)
	return err
}

// LatestConfigVersion 返回最近一次备份。
func (d *DB) LatestConfigVersion() (*ConfigVersion, error) {
	var v ConfigVersion
	var cur int
	err := d.QueryRow(
		`SELECT id, created_at, hy2_version, panel_version, content, reason, is_current
		 FROM config_versions ORDER BY id DESC LIMIT 1`,
	).Scan(&v.ID, &v.CreatedAt, &v.HY2Version, &v.PanelVersion, &v.Content, &v.Reason, &cur)
	if err != nil {
		return nil, err
	}
	v.IsCurrent = cur != 0
	return &v, nil
}

// ---------------------------------------------------------------------------
// 用户连接事件（接入 / 断开）
// ---------------------------------------------------------------------------

// 事件类型。
const (
	EventConnect    = "connect"
	EventDisconnect = "disconnect"
)

// UserEvent 是一次接入 / 断开记录。
type UserEvent struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"userId"`
	Kind   string `json:"kind"`
	TS     int64  `json:"ts"`
	Online int    `json:"online"`
}

// InsertUserEvent 记录一次接入 / 断开（同一秒同类型自动去重）。
func (d *DB) InsertUserEvent(userID int64, kind string, ts int64, online int) error {
	_, err := d.Exec(
		`INSERT OR IGNORE INTO user_events (user_id, kind, ts, online) VALUES (?, ?, ?, ?)`,
		userID, kind, ts, online,
	)
	return err
}

// ListUserEvents 返回某用户最近的连接事件（时间倒序）。
func (d *DB) ListUserEvents(userID int64, limit int) ([]*UserEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := d.Query(
		`SELECT id, user_id, kind, ts, online FROM user_events
 WHERE user_id = ? ORDER BY ts DESC, id DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*UserEvent, 0, limit)
	for rows.Next() {
		var e UserEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.TS, &e.Online); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// PruneUserEvents 删除 before 之前的连接事件。
func (d *DB) PruneUserEvents(before int64) error {
	_, err := d.Exec(`DELETE FROM user_events WHERE ts < ?`, before)
	return err
}
