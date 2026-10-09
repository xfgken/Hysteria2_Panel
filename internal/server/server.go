// Package server 提供 Panel 的 HTTP 服务与 API。
//
// 路由规划对齐开发文档 63 节。
package server

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/db"
	"github.com/hy2-panel/hy2-panel/internal/hy2"
	"github.com/hy2-panel/hy2-panel/internal/manager"
)

// Version 是 Panel 版本号。
//
// 声明为变量而非常量，便于构建时通过
// -ldflags "-X .../internal/server.Version=x.y.z" 注入。
var Version = "0.1.0-dev"

// Options 构造 Server 所需的依赖。
type Options struct {
	DB           *db.DB
	Manager      *manager.Manager
	Assets       fs.FS // 前端构建产物（web/dist），可为空
	TemplatePath string
	DataDir      string
}

// Server 持有 Panel 的依赖与路由。
type Server struct {
	db           *db.DB
	mgr          *manager.Manager
	assets       fs.FS
	templatePath string
	dataDir      string
	cache        *ttlCache
	mux          *http.ServeMux
}

// New 构造 Server 并注册路由。
func New(opts Options) *Server {
	s := &Server{
		db:           opts.DB,
		mgr:          opts.Manager,
		assets:       opts.Assets,
		templatePath: opts.TemplatePath,
		dataDir:      opts.DataDir,
		cache:        newTTLCache(),
		mux:          http.NewServeMux(),
	}
	s.routes()
	return s
}

// Handler 返回带中间件的处理器。
func (s *Server) Handler() http.Handler {
	return s.withMiddleware(s.mux)
}

// hy2Client 依据当前官方配置构造 Traffic Stats API 客户端。
//
// 这样设计是为了让用户在「服务器配置」页改动 trafficStats 后立即生效，
// 无需重启 Panel。
func (s *Server) hy2Client() (*hy2.Client, error) {
	cfg, err := s.mgr.Current()
	if err != nil {
		return nil, err
	}
	if cfg.Server.TrafficStats == nil || cfg.Server.TrafficStats.Listen == "" {
		return nil, errors.New("官方配置未启用 trafficStats，无法读取实时数据")
	}
	return hy2.NewClient(cfg.Server.TrafficStats.Listen, cfg.Server.TrafficStats.Secret), nil
}

// routes 注册全部路由。
func (s *Server) routes() {
	// ---- 公开 ----
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("GET /sub/{token}", s.handleSubscription)
	// 兜底：订阅路径写错（少 token、多斜杠等）时不落到前端页面，
	// 而是明确回一段 JSON 错误并记进日志，方便排查客户端导入失败。
	s.mux.HandleFunc("/sub/", s.handleSubFallback)

	// ---- 会话 ----
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("POST /api/session/password", s.handleChangePassword)

	// ---- 仪表盘 ----
	s.mux.HandleFunc("GET /api/dashboard", s.handleDashboard)

	// ---- 用户 ----
	s.mux.HandleFunc("GET /api/users", s.handleListUsers)
	s.mux.HandleFunc("POST /api/users", s.handleCreateUser)
	s.mux.HandleFunc("GET /api/users/{id}", s.handleGetUser)
	s.mux.HandleFunc("PUT /api/users/{id}", s.handleUpdateUser)
	s.mux.HandleFunc("DELETE /api/users/{id}", s.handleDeleteUser)
	s.mux.HandleFunc("POST /api/users/{id}/kick", s.handleKickUser)
	s.mux.HandleFunc("POST /api/users/{id}/regenerate-name", s.handleRegenerateName)
	s.mux.HandleFunc("GET /api/users/{id}/uri", s.handleUserURI)
	s.mux.HandleFunc("GET /api/users/{id}/subscription", s.handleUserSubscription)
	s.mux.HandleFunc("POST /api/users/{id}/subscription/reset", s.handleResetSubscription)
	s.mux.HandleFunc("GET /api/users/{id}/events", s.handleListUserEvents)

	// ---- 服务器配置 ----
	s.mux.HandleFunc("GET /api/server/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/server/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/server/config/raw", s.handleGetRawConfig)
	s.mux.HandleFunc("PUT /api/server/config/raw", s.handlePutRawConfig)
	s.mux.HandleFunc("GET /api/server/certificate", s.handleCertificateInfo)
	s.mux.HandleFunc("POST /api/server/config/validate", s.handleValidateConfig)
	s.mux.HandleFunc("POST /api/server/config/apply", s.handleApplyConfig)
	s.mux.HandleFunc("POST /api/server/users/sync", s.handleSyncUsers)

	// ---- 备份 ----
	s.mux.HandleFunc("GET /api/server/backups", s.handleListBackups)
	s.mux.HandleFunc("POST /api/server/backups/{id}/restore", s.handleRestoreBackup)
	s.mux.HandleFunc("DELETE /api/server/backups/{id}", s.handleDeleteBackup)

	// ---- 设置 ----
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// ---- 网络 ----
	s.mux.HandleFunc("GET /api/network/online", s.handleOnline)
	s.mux.HandleFunc("GET /api/network/streams", s.handleStreams)
	s.mux.HandleFunc("GET /api/network/traffic", s.handleTraffic)

	// ---- 日志 ----
	s.mux.HandleFunc("GET /api/logs/{source}", s.handleLogs)
	s.mux.HandleFunc("DELETE /api/logs/{source}", s.handleClearLogs)

	// ---- 系统 ----
	s.mux.HandleFunc("GET /api/system/status", s.handleSystemStatus)
	s.mux.HandleFunc("GET /api/system/host", s.handleHostStats)
	s.mux.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	s.mux.HandleFunc("POST /api/system/service/{name}/{action}", s.handleServiceAction)

	// ---- 官方 Core 更新（Phase 16）----
	s.mux.HandleFunc("GET /api/system/update/check", s.handleUpdateCheck)
	s.mux.HandleFunc("POST /api/system/update/apply", s.handleUpdateApply)
	s.mux.HandleFunc("POST /api/system/update/rollback", s.handleUpdateRollback)

	// ---- 前端静态资源（SPA）----
	s.mux.HandleFunc("/", s.handleStatic)
}

// ---------------------------------------------------------------------------
// 基础端点
// ---------------------------------------------------------------------------

// handleHealth 报告 Panel 自身及依赖组件的健康状态。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"panel": "ok"}

	if err := s.db.Ping(); err != nil {
		status["database"] = "error: " + err.Error()
	} else {
		status["database"] = "ok"
	}

	client, err := s.hy2Client()
	switch {
	case err != nil:
		status["hysteria_api"] = "not_configured"
	default:
		if perr := client.Ping(); perr != nil {
			status["hysteria_api"] = "error: " + perr.Error()
		} else {
			status["hysteria_api"] = "ok"
		}
	}

	writeJSON(w, http.StatusOK, status)
}

// handleVersion 返回 Panel 版本与官方 Core 版本。
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":      Version,
		"hysteria":     s.coreVersion(),
		"service_unit": s.mgr.ServiceUnit(),
		"config_path":  s.mgr.ConfigPath(),
	})
}

// ---------------------------------------------------------------------------
// 静态资源
// ---------------------------------------------------------------------------

// handleStatic 提供前端静态资源，并对未知路径回退到 index.html（SPA）。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		writeError(w, http.StatusNotFound, "前端资源未构建，请先构建 web/dist")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// 缓存策略：
	//   · /assets/ 下是带内容哈希的构建产物，文件名一变就是新文件，可以长期强缓存；
	//   · 入口 HTML 必须每次校验，否则浏览器会一直跑旧版本的 JS/CSS
	//     （表现为“前端改了、刷新后却毫无变化”）。
	if strings.HasPrefix(path, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}
	if f, err := s.assets.Open(path); err == nil {
		_ = f.Close()
		http.FileServer(http.FS(s.assets)).ServeHTTP(w, r)
		return
	}

	// SPA 回退
	index, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		writeError(w, http.StatusNotFound, "未找到 index.html")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}

// Start 启动后台任务（流量采样、会话清理）。
func (s *Server) Start(ctx context.Context) {
	go s.runSampler(ctx)
	go s.runSessionTracker(ctx)

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.db.CleanupExpiredSessions(); err != nil {
					log.Printf("清理过期会话失败: %v", err)
				}
			}
		}
	}()
}