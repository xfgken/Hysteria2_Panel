package server

// 用户接入 / 断开检测。
//
// 官方 Traffic Stats API 的 /online 只给出“当前连接数”，
// Panel 周期性比对前后两次快照：0 → N 记为接入，N → 0 记为断开，
// 从而得到官方 Core 本身不提供的连接历史（每个用户何时连上、何时断开）。

import (
	"context"
	"log"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/db"
)

// sessionPollInterval 是接入 / 断开检测的轮询周期。
const sessionPollInterval = 15 * time.Second

// sessionSnapshot 是某一时刻所有用户的在线连接数（离线用户记 0）。
type sessionSnapshot struct {
	at    int64
	count map[string]int
	id    map[string]int64
}

// runSessionTracker 后台检测每个用户的接入 / 断开。
func (s *Server) runSessionTracker(ctx context.Context) {
	var prev sessionSnapshot
	// 启动时先建立基线，避免把“启动瞬间已在线的用户”误记成刚刚接入。
	if snap, err := s.takeSessionSnapshot(); err == nil {
		prev = snap
	}

	ticker := time.NewTicker(sessionPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap, err := s.takeSessionSnapshot()
			if err != nil {
				// 官方 API 不可用（服务重启 / 未启用 trafficStats）时静默跳过。
				continue
			}
			s.recordSessionChanges(prev, snap)
			prev = snap
		}
	}
}

// takeSessionSnapshot 读取官方 /online，并补全所有用户（缺省 0）。
func (s *Server) takeSessionSnapshot() (sessionSnapshot, error) {
	client, err := s.hy2Client()
	if err != nil {
		return sessionSnapshot{}, err
	}
	online, err := client.Online()
	if err != nil {
		return sessionSnapshot{}, err
	}
	users, err := s.db.ListUsers("", 5000, 0)
	if err != nil {
		return sessionSnapshot{}, err
	}

	snap := sessionSnapshot{
		at:    time.Now().Unix(),
		count: make(map[string]int, len(users)),
		id:    make(map[string]int64, len(users)),
	}
	for _, u := range users {
		k := apiKey(u.Username)
		snap.id[k] = u.ID
		snap.count[k] = online[k]
	}
	return snap, nil
}

// recordSessionChanges 比对两次快照并写入事件。
func (s *Server) recordSessionChanges(prev, cur sessionSnapshot) {
	for k, c := range cur.count {
		p, seen := prev.count[k]
		if !seen {
			// 上一轮还不存在的用户（刚创建）只作基线，不记事件。
			continue
		}
		switch {
		case p == 0 && c > 0:
			if err := s.db.InsertUserEvent(cur.id[k], db.EventConnect, cur.at, c); err != nil {
				log.Printf("写入连接事件失败: %v", err)
			}
		case p > 0 && c == 0:
			if err := s.db.InsertUserEvent(cur.id[k], db.EventDisconnect, cur.at, 0); err != nil {
				log.Printf("写入断开事件失败: %v", err)
			}
		}
	}
}
