package server

import (
	"net/http"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/hy2"
)

// handleOnline 返回在线用户（开发文档 46 节）。
func (s *Server) handleOnline(w http.ResponseWriter, r *http.Request) {
	client, err := s.hy2Client()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	online, err := client.Online()
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取在线用户失败: "+err.Error())
		return
	}

	users, _ := s.db.ListUsers("", 1000, 0)
	nameByUser := map[string]int64{}
	for _, u := range users {
		// 官方 API 的 key 是用户名小写后的形式
		nameByUser[apiKey(u.Username)] = u.ID
	}

	type onlineItem struct {
		Username    string `json:"username"`
		UserID      int64  `json:"userId"`
		Connections int    `json:"connections"`
		Known       bool   `json:"known"`
	}
	items := make([]onlineItem, 0, len(online))
	for name, n := range online {
		id, known := nameByUser[apiKey(name)]
		items = append(items, onlineItem{Username: name, UserID: id, Connections: n, Known: known})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleStreams 返回官方 /dump/streams 的实时连接（开发文档 16 节）。
func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	client, err := s.hy2Client()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	streams, err := client.DumpStreams()
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取连接失败: "+err.Error())
		return
	}
	if streams == nil {
		streams = []hy2.Stream{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": streams, "total": len(streams)})
}

// handleTraffic 返回实时流量与历史曲线（开发文档 43 节）。
func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	users, _ := s.db.ListUsers("", 1000, 0)

	type item struct {
		Username   string `json:"username"`
		UserID     int64  `json:"userId"`
		SessionTx  int64  `json:"sessionTx"`
		SessionRx  int64  `json:"sessionRx"`
		TodayTx    int64  `json:"todayTx"`
		TodayRx    int64  `json:"todayRx"`
		TotalTx    int64  `json:"totalTx"`
		TotalRx    int64  `json:"totalRx"`
	}

	now := time.Now().Unix()
	today := dayStartUnix(time.Now())

	session := map[string]trafficView{}
	realtimeErr := ""
	if _, traffic, err := s.realtime(); err == nil {
		session = traffic
	} else {
		realtimeErr = err.Error()
	}

	items := make([]item, 0, len(users))
	for _, u := range users {
		it := item{Username: u.Username, UserID: u.ID}
		if t, ok := session[apiKey(u.Username)]; ok {
			it.SessionTx, it.SessionRx = t.Tx, t.Rx
		}
		it.TodayTx, it.TodayRx, _ = s.db.TrafficTotals(u.ID, today, now+1)
		it.TotalTx, it.TotalRx, _ = s.db.TrafficTotals(u.ID, 0, now+1)
		items = append(items, it)
	}

	// 全局 24 小时曲线
	series, _ := s.db.AllTrafficSeries(now-24*3600, 96)

	writeJSON(w, http.StatusOK, map[string]any{
		"items":         items,
		"series":        series,
		"realtimeError": realtimeErr,
	})
}