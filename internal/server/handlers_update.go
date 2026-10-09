package server

import (
	"net/http"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/updater"
)

// updater 构造官方 Core 更新器。
func (s *Server) updater() *updater.Updater {
	return updater.New(s.mgr.Binary(), s.mgr.ServiceUnit())
}

// handleUpdateCheck 查询官方最新版本（开发文档 48 节「更新」）。
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 60*time.Second)
	defer cancel()

	res, err := s.updater().Check(ctx)
	if err != nil {
		if res != nil {
			// 仍返回已获取到的部分信息，便于界面展示。
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error":   err.Error(),
				"current": res.Current,
				"latest":  res.Latest,
			})
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleUpdateApply 下载并安装最新官方 Core，失败自动回滚。
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 15*time.Minute)
	defer cancel()

	res, err := s.updater().Update(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// 版本已变化，失效缓存
	s.cache.invalidate(cacheKeyCoreVersion)
	s.cache.invalidate(cacheKeyServiceActive)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": res,
	})
}

// handleUpdateRollback 回滚到上一个官方 Core 版本。
func (s *Server) handleUpdateRollback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := timeoutContext(r, 60*time.Second)
	defer cancel()

	if err := s.updater().Rollback(ctx); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.cache.invalidate(cacheKeyCoreVersion)
	s.cache.invalidate(cacheKeyServiceActive)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "已回滚到上一版本"})
}