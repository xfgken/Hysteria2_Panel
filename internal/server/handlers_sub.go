package server

import (
	"log"
	"net/http"
	"strings"

	"github.com/hy2-panel/hy2-panel/internal/sub"
)

// handleUserURI 生成 Hysteria 2 URI（开发文档 18 节）。
//
// 同时返回“单端口兼容版”：端口跳跃的写法部分客户端无法识别，
// 单端口版本可保证任何客户端都能导入。
func (s *Server) handleUserURI(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	cfg, err := s.mgr.Current()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}

	uri, err := sub.BuildURI(&cfg.Server, user.Username, user.HY2Password, s.serverHost(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	single := sub.SinglePortURI(uri)
	writeJSON(w, http.StatusOK, map[string]string{
		"uri":       uri,
		"uriSingle": single,
	})
}

// subscriptionInfo 是订阅信息视图。
type subscriptionInfo struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

// handleUserSubscription 返回（必要时创建）用户的订阅信息。
func (s *Server) handleUserSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	active, err := s.db.ActiveSubscription(user.ID)
	if err != nil || active == nil {
		active, err = s.db.CreateSubscription(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "创建订阅失败: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, subscriptionInfo{
		Token:     active.Token,
		URL:       s.subBaseURL(r) + "/sub/" + active.Token,
		CreatedAt: active.CreatedAt,
	})
}

// handleResetSubscription 重新生成订阅 Token（旧 Token 立即失效）。
func (s *Server) handleResetSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	created, err := s.db.CreateSubscription(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "重置订阅失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, subscriptionInfo{
		Token:     created.Token,
		URL:       s.subBaseURL(r) + "/sub/" + created.Token,
		CreatedAt: created.CreatedAt,
	})
}

// handleSubFallback 处理写错的订阅路径。
//
// 以前这类请求会落到前端 index.html，客户端拿到一段 HTML，
// 报出「profile does not contain `proxies`」这种看不懂的错。
// 现在明确回 JSON 并记日志。
func (s *Server) handleSubFallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("[订阅] 路径不匹配（path=%q ua=%q 来源=%s），返回 404",
		r.URL.RequestURI(), r.UserAgent(), r.RemoteAddr)
	writeError(w, http.StatusNotFound, "订阅地址不完整：应为 /sub/<TOKEN>")
}

// legacyClientHint 判断拉取订阅的客户端是否属于「不支持 hysteria2 的老内核」。
//
// 原版 Clash for Android（Clash Premium 内核）、Clash for Windows、老版 ClashX
// 都不认识 hysteria2：导入后节点能显示，但延迟永远测不出来。直接把结论写进
// 日志，省得下次再从「端口、密码、证书」一路排查。
func legacyClientHint(ua string) string {
	if ua == "" {
		return ""
	}
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "clashmetaforandroid"), strings.Contains(lower, "clash.meta"),
		strings.Contains(lower, "mihomo"), strings.Contains(lower, "flclash"),
		strings.Contains(lower, "clash-verge"), strings.Contains(lower, "clashverge"):
		return " ✓ mihomo 内核，支持 hysteria2"
	case strings.Contains(lower, "clashforandroid"):
		return " ⚠ 检测到「Clash for Android」（Clash Premium 内核），不支持 hysteria2，请换 Clash Meta for Android"
	case strings.Contains(lower, "clashforwindows"), strings.Contains(lower, "clashx"):
		return " ⚠ 检测到老版桌面 Clash 内核，不支持 hysteria2，请换 Clash Verge Rev / mihomo"
	}
	return ""
}

// handleSubscription 是公开的订阅入口：/sub/{token}
//
// 默认返回 Clash Meta YAML；附带 ?format=uri 时返回 Hysteria 2 URI 文本。
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	// 订阅访问日志：出问题时能看清客户端请求的原始路径与返回码。
	log.Printf("[订阅] 收到请求 path=%q ua=%q 来源=%s", r.URL.RequestURI(), r.UserAgent(), r.RemoteAddr)
	subscription, err := s.db.SubscriptionByToken(token)
	if err != nil {
		log.Printf("[订阅] Token 无效（收到的 token=%q，长度 %d），返回 404", token, len(token))
		writeError(w, http.StatusNotFound, "订阅不存在或已失效")
		return
	}
	user, err := s.db.GetUserByID(subscription.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "订阅对应的用户不存在")
		return
	}
	if !user.Enabled {
		writeError(w, http.StatusForbidden, "该用户的订阅已被停用")
		return
	}

	s.db.TouchSubscription(token)

	// 记录是谁在拉订阅：出问题时能直接看到客户端与内核类型。
	kind := "clash"
	if r.URL.Query().Get("format") == "uri" {
		kind = "uri"
	}
	log.Printf("[订阅] %s 拉取订阅（%s，UA=%q，来源=%s）%s",
		user.Username, kind, r.UserAgent(), r.RemoteAddr, legacyClientHint(r.UserAgent()))

	cfg, err := s.mgr.Current()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器配置失败")
		return
	}

	if r.URL.Query().Get("format") == "uri" {
		uri, err := sub.BuildURI(&cfg.Server, user.Username, user.HY2Password, s.serverHost(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(uri + "\n"))
		return
	}

	data, err := sub.BuildClashData(&cfg.Server, user.Username, user.HY2Password, s.serverHost(r), s.intSetting(settingMixedPort, 7890))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	content, err := sub.RenderClash(s.templatePath, data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Profile-Update-Interval", "24")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}