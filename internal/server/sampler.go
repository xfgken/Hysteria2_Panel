package server

import (
	"context"
	"log"
	"strings"
	"time"
)

// samplerInterval 是流量采样周期。
//
// 官方 Traffic Stats API 返回的是「自启动以来的累计值」，
// Panel 周期性读取并与上次快照求差，得到增量后写入 SQLite，
// 从而形成官方 Core 本身不提供的历史流量（开发文档 43 节）。
const samplerInterval = 30 * time.Second

// runSampler 后台周期性采集流量。
func (s *Server) runSampler(ctx context.Context) {
	var prev map[string]trafficView

	ticker := time.NewTicker(samplerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prev = s.sampleOnce(prev)
		}
	}
}

// sampleOnce 执行一次采样，返回本次快照。
func (s *Server) sampleOnce(prev map[string]trafficView) map[string]trafficView {
	_, current, err := s.realtime()
	if err != nil {
		// 官方 API 不可用时静默跳过，避免日志噪音。
		return prev
	}

	// 首次采样只建立基线。
	if prev == nil {
		return current
	}

	users, err := s.db.ListUsers("", 2000, 0)
	if err != nil {
		return current
	}
	idByName := make(map[string]int64, len(users))
	for _, u := range users {
		// current 的 key 来自官方 API，是用户名小写后的形式；
		// 以前这里用原样用户名建索引，导致大写字母的账号**永远采不到流量**。
		idByName[strings.ToLower(u.Username)] = u.ID
	}

	bucket := time.Now().Truncate(time.Hour).Unix()
	for name, cur := range current {
		id, ok := idByName[strings.ToLower(name)]
		if !ok {
			continue
		}
		old, existed := prev[name]
		var dTx, dRx int64
		if existed && cur.Tx >= old.Tx {
			dTx = cur.Tx - old.Tx
		} else {
			// 服务重启或计数清零：把当前值整体计入。
			dTx = cur.Tx
		}
		if existed && cur.Rx >= old.Rx {
			dRx = cur.Rx - old.Rx
		} else {
			dRx = cur.Rx
		}
		if dTx == 0 && dRx == 0 {
			continue
		}
		if err := s.db.InsertSample(id, bucket, dTx, dRx); err != nil {
			log.Printf("写入流量采样失败: %v", err)
		}
	}

	// 顺带清理一年前的采样。
	if time.Now().Unix()%3600 < int64(samplerInterval.Seconds()) {
		_ = s.db.PruneSamples(time.Now().AddDate(-1, 0, 0).Unix())
		_ = s.db.PruneUserEvents(time.Now().AddDate(0, 0, -90).Unix())
	}

	return current
}