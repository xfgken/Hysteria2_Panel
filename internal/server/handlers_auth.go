package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hy2-panel/hy2-panel/internal/auth"
)

// handleLogin 处理面板登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}

	admin, err := s.db.GetAdminByUsername(strings.TrimSpace(req.Username))
	if err != nil || !auth.CheckPassword(admin.PasswordHash, req.Password) {
		// 统一文案，避免暴露用户名是否存在。
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	sess, err := s.db.CreateSession(admin.ID, auth.SessionTTL, clientIP(r), r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"username":  admin.Username,
		"csrfToken": sess.CSRFToken,
	})
}

// handleLogout 注销当前会话。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess := currentSession(r); sess != nil {
		_ = s.db.DeleteSession(sess.ID)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSession 返回当前登录态。
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "未登录")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"csrfToken": sess.CSRFToken,
		"expiresAt": sess.ExpiresAt,
	})
}

// handleChangePassword 修改当前管理员密码。
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "新密码长度至少 8 位")
		return
	}

	sess := currentSession(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "未登录")
		return
	}

	admin, err := s.db.GetAdminByID(sess.AdminID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取管理员失败")
		return
	}
	if !auth.CheckPassword(admin.PasswordHash, req.CurrentPassword) {
		writeError(w, http.StatusForbidden, "当前密码不正确")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成密码哈希失败")
		return
	}
	if err := s.db.UpdateAdminPassword(admin.ID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "保存密码失败")
		return
	}

	// 改密后使其它会话失效。
	_ = s.db.DeleteSession(sess.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	return host
}

// randomToken 生成用于 CSRF / 内部用途的随机串。
func randomToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}