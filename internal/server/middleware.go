package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/hy2-panel/hy2-panel/internal/auth"
	"github.com/hy2-panel/hy2-panel/internal/db"
)

type ctxKey int

const ctxSessionKey ctxKey = iota

// publicPaths 是无需登录即可访问的路径前缀。
var publicPaths = []string{
	"/api/health",
	"/api/version",
	"/api/login",
	"/sub/",
}

// withMiddleware 组合安全响应头、会话鉴权与 CSRF 校验。
func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 安全响应头（开发文档 62 节）
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		if !strings.HasPrefix(r.URL.Path, "/api/") || isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		sess, err := s.sessionFromRequest(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "未登录或会话已过期")
			return
		}

		// 变更类请求必须带正确 CSRF Token。
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			token := r.Header.Get(auth.CSRFHeader)
			if token == "" || !auth.SecureCompare(token, sess.CSRFToken) {
				writeError(w, http.StatusForbidden, "CSRF 校验失败")
				return
			}
		}

		ctx := context.WithValue(r.Context(), ctxSessionKey, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublic 判断路径是否免登录。
func isPublic(path string) bool {
	for _, p := range publicPaths {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// sessionFromRequest 从 Cookie 读取并校验会话。
func (s *Server) sessionFromRequest(r *http.Request) (*db.Session, error) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return nil, err
	}
	return s.db.GetSession(c.Value)
}

// currentSession 取出中间件注入的会话。
func currentSession(r *http.Request) *db.Session {
	if v, ok := r.Context().Value(ctxSessionKey).(*db.Session); ok {
		return v
	}
	return nil
}