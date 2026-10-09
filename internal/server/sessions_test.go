package server

// 接入 / 断开检测的单元测试。
//
// 检测逻辑不依赖真实客户端，只需比对两次在线快照，
// 因此这里用临时 SQLite 直接驱动 recordSessionChanges。

import (
	"path/filepath"
	"testing"

	"github.com/hy2-panel/hy2-panel/internal/db"
)

func newSessionTestServer(t *testing.T) (*Server, *db.DB, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	u, err := d.CreateUser("hysteria2-abc", "pw12345678", "")
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return &Server{db: d}, d, u.ID
}

// 离线 -> 在线 记“接入”，在线 -> 离线 记“断开”。
func TestRecordSessionChanges(t *testing.T) {
	s, d, id := newSessionTestServer(t)
	key := "hysteria2-abc"
	ids := map[string]int64{key: id}

	// 基线：离线。
	prev := sessionSnapshot{at: 100, count: map[string]int{key: 0}, id: ids}

	// 接入（2 个连接）。
	online := sessionSnapshot{at: 200, count: map[string]int{key: 2}, id: ids}
	s.recordSessionChanges(prev, online)

	// 仍在线的重复快照不应重复记录。
	s.recordSessionChanges(online, sessionSnapshot{at: 215, count: map[string]int{key: 2}, id: ids})

	// 断开。
	offline := sessionSnapshot{at: 300, count: map[string]int{key: 0}, id: ids}
	s.recordSessionChanges(online, offline)

	events, err := d.ListUserEvents(id, 50)
	if err != nil {
		t.Fatalf("读取事件失败: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("期望 2 条事件，实际 %d 条", len(events))
	}
	// 倒序返回：最新的在前。
	if events[0].Kind != db.EventDisconnect || events[0].TS != 300 {
		t.Errorf("第一条应为 300 秒的断开，实际 %+v", events[0])
	}
	if events[1].Kind != db.EventConnect || events[1].TS != 200 || events[1].Online != 2 {
		t.Errorf("第二条应为 200 秒、2 连接的接入，实际 %+v", events[1])
	}
}

// 第一轮就把在线用户误报为“接入”是不允许的（应只作基线）。
func TestRecordSessionChangesNoPhantomConnect(t *testing.T) {
	s, d, id := newSessionTestServer(t)
	key := "hysteria2-abc"
	ids := map[string]int64{key: id}

	// 上一轮快照里没有这个用户（例如刚创建）。
	s.recordSessionChanges(
		sessionSnapshot{at: 100, count: map[string]int{}, id: ids},
		sessionSnapshot{at: 115, count: map[string]int{key: 1}, id: ids},
	)

	events, err := d.ListUserEvents(id, 50)
	if err != nil {
		t.Fatalf("读取事件失败: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("期望 0 条事件，实际 %d 条", len(events))
	}
}
