package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/db"
)

// apiKey 把账号名转换成官方 Traffic Stats API 的 key。
//
// 官方 Core 对 userpass 的用户名做**小写化**处理，因此
// hysteria2-AbCxYz 在 /online 与 /traffic 里都是 hysteria2-abcxyz。
// 面板与它对账时必须同样小写，否则在线状态与本次流量永远读不到。
func apiKey(username string) string {
	return strings.ToLower(username)
}

// realtime 读取官方实时数据（在线数、本次运行累计流量）。
//
// 数据来源优先使用官方 Traffic Stats API（开发文档 16 / 42 / 46 节）。
func (s *Server) realtime() (online map[string]int, traffic map[string]trafficView, err error) {
	client, err := s.hy2Client()
	if err != nil {
		return nil, nil, err
	}
	online, err = client.Online()
	if err != nil {
		return nil, nil, err
	}
	raw, err := client.Traffic(false)
	if err != nil {
		return nil, nil, err
	}
	traffic = make(map[string]trafficView, len(raw))
	for k, v := range raw {
		traffic[k] = trafficView{Tx: v.Tx, Rx: v.Rx}
	}
	return online, traffic, nil
}

// trafficView 是流量视图。
type trafficView struct {
	Tx int64 `json:"tx"`
	Rx int64 `json:"rx"`
}

// userView 是用户列表项。
type userView struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Enabled     bool   `json:"enabled"`
	Note        string `json:"note"`
	CreatedAt   string `json:"createdAt"`
	Online      int    `json:"online"`      // 当前连接（设备）数
	SessionTx   int64  `json:"sessionTx"`   // 本次运行上行
	SessionRx   int64  `json:"sessionRx"`   // 本次运行下行
	Historical  int64  `json:"historical"`  // 历史累计（Panel 统计）
	HistoricalTx int64 `json:"historicalTx"`
	HistoricalRx int64 `json:"historicalRx"`
}

// handleListUsers 返回用户列表（含实时与历史流量）。
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size <= 0 || size > 200 {
		size = 50
	}

	users, err := s.db.ListUsers(q, size, (page-1)*size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "查询用户失败: "+err.Error())
		return
	}
	total, _ := s.db.CountUsers()

	online, traffic, rtErr := s.realtime()

	items := make([]userView, 0, len(users))
	for _, u := range users {
		v := userView{
			ID:        u.ID,
			Username:  u.Username,
			Enabled:   u.Enabled,
			Note:      u.Note,
			CreatedAt: u.CreatedAt,
		}
		if rtErr == nil {
			// 官方 Core 把用户名小写化后作为 API 的 key，这里用小写对账
			v.Online = online[apiKey(u.Username)]
			if t, ok := traffic[apiKey(u.Username)]; ok {
				v.SessionTx, v.SessionRx = t.Tx, t.Rx
			}
		}
		v.HistoricalTx, v.HistoricalRx, _ = s.db.TrafficTotals(u.ID, 0, time.Now().Unix()+1)
		v.Historical = v.HistoricalTx + v.HistoricalRx
		items = append(items, v)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"pageSize":  size,
		"realtime":  rtErr == nil,
		"realtimeError": errString(rtErr),
	})
}

// handleCreateUser 创建账号。
//
// 面板支持多个账号（官方 userpass 一张表，每行一个），账号名仍由面板生成；
// 但至少要保留一个账号 —— userpass 为空时官方 Core 会拒绝启动。
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Note     string `json:"note"`
		// Suffix 是自定义名称后缀（hysteria2-<suffix>）；为空时随机生成
		Suffix string `json:"suffix"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if req.Password == "" {
		req.Password = randomToken(9)
	}

	// 账号名固定为 hysteria2-<6 位>：未指定后缀时随机生成
	var username string
	if suffix := strings.TrimSpace(req.Suffix); suffix != "" {
		if err := db.ValidateUsernameSuffix(suffix); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		username = db.UsernamePrefix + suffix
	} else {
		generated, err := db.RandomUsername()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		username = generated
	}

	user, err := s.db.CreateUser(username, req.Password, req.Note)
	if err != nil {
		writeError(w, http.StatusConflict, "创建账号失败: "+err.Error())
		return
	}

	syncErr := s.mgr.SyncUsers(r.Context())
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":      user,
		"password":  req.Password, // 仅在创建时回显一次
		"syncError": errString(syncErr),
	})
}

// handleGetUser 返回用户详情。
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	detail := map[string]any{
		"user": user,
	}

	// 历史流量
	tx, rx, _ := s.db.TrafficTotals(user.ID, 0, time.Now().Unix()+1)
	detail["historical"] = trafficView{Tx: tx, Rx: rx}

	// 实时流量与连接
	if online, traffic, rtErr := s.realtime(); rtErr == nil {
		detail["online"] = online[apiKey(user.Username)]
		if t, ok := traffic[apiKey(user.Username)]; ok {
			detail["session"] = t
		}
		// 连接详情来自官方 /dump/streams
		if client, cerr := s.hy2Client(); cerr == nil {
			if streams, serr := client.DumpStreams(); serr == nil {
				var mine []any
				for _, st := range streams {
					if st.Auth == user.Username {
						mine = append(mine, st)
					}
				}
				detail["streams"] = mine
			}
		}
	} else {
		detail["realtimeError"] = rtErr.Error()
	}

	writeJSON(w, http.StatusOK, detail)
}

// handleUpdateUser 修改用户（密码 / 备注 / 启用状态）。
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	var req struct {
		Password *string `json:"password"`
		Note     *string `json:"note"`
		Enabled  *bool   `json:"enabled"`
		// Suffix 可选：自定义名称后缀（完整名称 = hysteria2-<suffix>）
		Suffix *string `json:"suffix"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}

	if req.Suffix != nil {
		suffix := strings.TrimSpace(*req.Suffix)
		if err := db.ValidateUsernameSuffix(suffix); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		name := db.UsernamePrefix + suffix
		if name != user.Username {
			if err := s.db.SetUsername(user.ID, name); err != nil {
				writeError(w, http.StatusConflict, "修改名称失败（可能已被占用）: "+err.Error())
				return
			}
		}
	}
	if req.Password != nil {
		if *req.Password == "" {
			writeError(w, http.StatusBadRequest, "密码不能为空")
			return
		}
		if err := s.db.UpdateUserPassword(user.ID, *req.Password); err != nil {
			writeError(w, http.StatusInternalServerError, "修改密码失败")
			return
		}
	}
	if req.Note != nil {
		if err := s.db.UpdateUserNote(user.ID, *req.Note); err != nil {
			writeError(w, http.StatusInternalServerError, "修改备注失败")
			return
		}
	}
	if req.Enabled != nil {
		if err := s.db.SetUserEnabled(user.ID, *req.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "修改状态失败")
			return
		}
	}

	syncErr := s.mgr.SyncUsers(r.Context())

	// 账号被真正编辑过（改密码 / 名称 / 备注）就换掉它的订阅 Token。
	//
	// 注意：单纯的启用/停用**不**轮换 —— 停用后订阅地址仍返回 403（明确的
	// “账号已停用”），而不是 404（地址不存在），语义更清楚。
	edited := req.Password != nil || req.Suffix != nil || req.Note != nil
	rotated := false
	if edited {
		if _, err := s.db.CreateSubscription(user.ID); err != nil {
			log.Printf("[用户操作] 修改账号后重新生成订阅 Token 失败 user=%s: %v", user.Username, err)
		} else {
			rotated = true
			log.Printf("[用户操作] 修改账号后已重新生成订阅 Token user=%s", user.Username)
		}
	}

	updated, _ := s.db.GetUserByID(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":         updated,
		"syncError":    errString(syncErr),
		"rotatedToken": rotated,
	})
}

// handleRegenerateName 重新生成账号名（首页「名称」旁的刷新按钮）。
//
// 账号名由面板生成，每次调用都会换一个新的；新名字需要写回官方配置的
// userpass 并重载服务，所以：
//   - Clash 订阅地址（基于 Token）保持不变，拉取到的内容会自动是新名字；
//   - 直接用 URI 导入的客户端需要重新导入。
func (s *Server) handleRegenerateName(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	username, err := db.RandomUsername()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.SetUsername(user.ID, username); err != nil {
		writeError(w, http.StatusInternalServerError, "重新生成名称失败: "+err.Error())
		return
	}

	syncErr := s.mgr.SyncUsers(r.Context())
	updated, _ := s.db.GetUserByID(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": updated, "syncError": errString(syncErr)})
}

// handleDeleteUser 删除账号。
//
// 允许删除任意账号（包括最后一个）。但官方 Core 要求 userpass 非空：
// 如果删完一个都不剩，面板会立即补建一个随机账号，保证服务仍然可启动，
// 并把新账号的名称与密码一并返回，由前端提示用户。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	// 先踢下线，避免已连接用户继续使用。
	if client, cerr := s.hy2Client(); cerr == nil {
		_ = client.Kick([]string{user.Username})
	}
	if err := s.db.DeleteUser(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "删除用户失败")
		return
	}

	// 删空了就补建一个：userpass 为空时官方 Core 会拒绝启动。
	var replacement *db.User
	replacementPassword := ""
	if left, _ := s.db.CountUsers(); left == 0 {
		name, nerr := db.RandomUsername()
		if nerr != nil {
			writeError(w, http.StatusInternalServerError, "账号已删除，但补建账号失败: "+nerr.Error())
			return
		}
		pw := randomToken(9)
		created, cerr := s.db.CreateUser(name, pw, "自动补建")
		if cerr != nil {
			writeError(w, http.StatusInternalServerError, "账号已删除，但补建账号失败: "+cerr.Error())
			return
		}
		replacement, replacementPassword = created, pw
	}

	syncErr := s.mgr.SyncUsers(r.Context())
	out := map[string]any{"status": "ok", "syncError": errString(syncErr)}
	if replacement != nil {
		out["replacement"] = replacement
		out["replacementPassword"] = replacementPassword
	}
	writeJSON(w, http.StatusOK, out)
}

// handleKickUser 通过官方 /kick 踢用户下线（开发文档 17 节）。
func (s *Server) handleKickUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	client, err := s.hy2Client()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := client.Kick([]string{user.Username}); err != nil {
		writeError(w, http.StatusBadGateway, "踢下线失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

// lookupUser 从路径参数解析并查询用户。
func (s *Server) lookupUser(r *http.Request) (*db.User, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return nil, errors.New("非法的用户 ID")
	}
	return s.db.GetUserByID(id)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}