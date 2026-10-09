package server

// 单个用户的接入 / 断开历史。

import (
	"net/http"
	"strconv"
)

// handleListUserEvents 返回某个用户的接入 / 断开记录（时间倒序）。
func (s *Server) handleListUserEvents(w http.ResponseWriter, r *http.Request) {
	user, err := s.lookupUser(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.db.ListUserEvents(user.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取连接记录失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"userId": user.ID,
		"items":  events,
	})
}
