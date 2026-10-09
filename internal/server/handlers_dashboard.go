package server

import (
	"net/http"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/db"
)

// dashboardResponse 是仪表盘数据（开发文档 11 节）。
type dashboardResponse struct {
	TotalUsers        int   `json:"totalUsers"`
	OnlineUsers       int   `json:"onlineUsers"`
	Connections       int   `json:"connections"`
	SessionTx         int64 `json:"sessionTx"`
	SessionRx         int64 `json:"sessionRx"`
	TodayTx           int64 `json:"todayTx"`
	TodayRx           int64 `json:"todayRx"`
	TotalTx           int64 `json:"totalTx"`
	TotalRx           int64 `json:"totalRx"`
	CoreVersion       string `json:"coreVersion"`
	ServiceActive     bool   `json:"serviceActive"`
	RealtimeAvailable bool   `json:"realtimeAvailable"`
	RealtimeError     string `json:"realtimeError,omitempty"`
	TrafficSeries     []db.TrafficPoint `json:"trafficSeries"`
}

// handleDashboard 汇总仪表盘数据。
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var resp dashboardResponse

	resp.TotalUsers, _ = s.db.CountUsers()

	// 历史流量（Panel 统计）
	now := time.Now()
	resp.TodayTx, resp.TodayRx, _ = s.db.AllTrafficTotals(dayStartUnix(now), now.Unix()+1)
	resp.TotalTx, resp.TotalRx, _ = s.db.AllTrafficTotals(0, now.Unix()+1)
	resp.TrafficSeries, _ = s.db.AllTrafficSeries(dayStartUnix(now)-24*3600, 48)

	// 实时数据（官方 API）
	if online, traffic, err := s.realtime(); err == nil {
		resp.RealtimeAvailable = true
		for _, n := range online {
			if n > 0 {
				resp.OnlineUsers++
			}
			resp.Connections += n
		}
		for _, t := range traffic {
			resp.SessionTx += t.Tx
			resp.SessionRx += t.Rx
		}
	} else {
		resp.RealtimeError = err.Error()
	}

	resp.CoreVersion = s.coreVersion()
	resp.ServiceActive = s.serviceActive()

	writeJSON(w, http.StatusOK, resp)
}

// dayStartUnix 返回本地当天 0 点。
func dayStartUnix(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}