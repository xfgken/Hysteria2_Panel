package db

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestUserCRUD(t *testing.T) {
	d := newTestDB(t)

	u, err := d.CreateUser("alice", "pw", "备注")
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if u.ID == 0 || !u.Enabled {
		t.Fatalf("新用户字段异常: %+v", u)
	}

	got, err := d.GetUserByUsername("alice")
	if err != nil || got.ID != u.ID {
		t.Fatalf("按用户名查询失败: %v", err)
	}

	n, _ := d.CountUsers()
	if n != 1 {
		t.Errorf("用户数期望 1，实际 %d", n)
	}

	if err := d.UpdateUserPassword(u.ID, "newpw"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetUserByID(u.ID)
	if got.HY2Password != "newpw" {
		t.Error("密码未更新")
	}

	if err := d.SetUserEnabled(u.ID, false); err != nil {
		t.Fatal(err)
	}
	userpass, _ := d.EnabledUserpass()
	if _, ok := userpass["alice"]; ok {
		t.Error("停用用户不应出现在启用列表中")
	}

	list, err := d.ListUsers("ali", 10, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("模糊查询失败: %v len=%d", err, len(list))
	}
	empty, _ := d.ListUsers("nobody", 10, 0)
	if len(empty) != 0 {
		t.Error("不匹配的查询应返回空")
	}

	if err := d.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetUserByID(u.ID); err != ErrNotFound {
		t.Errorf("删除后查询期望 ErrNotFound，实际 %v", err)
	}

	if _, err := d.CreateUser("alice", "pw", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateUser("alice", "pw", ""); err == nil {
		t.Error("重复用户名应报错")
	}
}

func TestAdminAndSession(t *testing.T) {
	d := newTestDB(t)

	a, err := d.CreateAdmin("admin", "hash")
	if err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	if n, _ := d.CountAdmins(); n != 1 {
		t.Errorf("管理员数期望 1，实际 %d", n)
	}
	if got, err := d.GetAdminByID(a.ID); err != nil || got.Username != "admin" {
		t.Fatalf("按 ID 查询管理员失败: %v", err)
	}

	sess, err := d.CreateSession(a.ID, time.Hour, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	if len(sess.ID) < 32 || len(sess.CSRFToken) < 32 {
		t.Error("会话 ID / CSRF Token 长度不足，随机性可能不够")
	}

	loaded, err := d.GetSession(sess.ID)
	if err != nil || loaded.AdminID != a.ID {
		t.Fatalf("读取会话失败: %v", err)
	}

	if err := d.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSession(sess.ID); err != ErrNotFound {
		t.Error("删除后会话应不存在")
	}

	// 过期会话不可读取
	expired, _ := d.CreateSession(a.ID, -time.Minute, "", "")
	if _, err := d.GetSession(expired.ID); err != ErrNotFound {
		t.Error("过期会话不应可读")
	}
	if err := d.CleanupExpiredSessions(); err != nil {
		t.Fatal(err)
	}

	// 改密后应能一次性注销该管理员的全部会话
	s1, _ := d.CreateSession(a.ID, time.Hour, "", "")
	s2, _ := d.CreateSession(a.ID, time.Hour, "", "")
	if err := d.DeleteAdminSessions(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSession(s1.ID); err != ErrNotFound {
		t.Error("注销后第一个会话应失效")
	}
	if _, err := d.GetSession(s2.ID); err != ErrNotFound {
		t.Error("注销后第二个会话应失效")
	}
}

func TestEnabledUserpassIncludesAllEnabledUsers(t *testing.T) {
	d := newTestDB(t)

	a, _ := d.CreateUser("hysteria2-aaaaaa", "pw-a", "")
	b, _ := d.CreateUser("hysteria2-bbbbbb", "pw-b", "")
	c, _ := d.CreateUser("hysteria2-cccccc", "pw-c", "")

	// 停用其中一个：不应出现在 userpass 里
	if err := d.SetUserEnabled(c.ID, false); err != nil {
		t.Fatal(err)
	}

	up, err := d.EnabledUserpass()
	if err != nil {
		t.Fatal(err)
	}
	if len(up) != 2 {
		t.Fatalf("应有 2 个启用账号，实际 %d：%v", len(up), up)
	}
	for _, u := range []*User{a, b} {
		if up[u.Username] != u.HY2Password {
			t.Errorf("账号 %s 的密码未正确同步：%v", u.Username, up)
		}
	}
	if _, ok := up[c.Username]; ok {
		t.Errorf("停用账号不应出现在 userpass：%v", up)
	}

	// 全部停用后应为空 map
	if err := d.SetUserEnabled(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetUserEnabled(b.ID, false); err != nil {
		t.Fatal(err)
	}
	up, err = d.EnabledUserpass()
	if err != nil {
		t.Fatal(err)
	}
	if len(up) != 0 {
		t.Errorf("全部停用后 userpass 应为空，实际 %v", up)
	}
}

func TestSubscriptionLifecycle(t *testing.T) {
	d := newTestDB(t)
	u, _ := d.CreateUser("bob", "pw", "")

	s1, err := d.CreateSubscription(u.ID)
	if err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	// 订阅 Token 固定为 20 位小写字母 + 数字
	if len(s1.Token) != 20 {
		t.Errorf("订阅 Token 应为 20 位，实际 %d 位：%s", len(s1.Token), s1.Token)
	}
	for _, c := range s1.Token {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			t.Errorf("订阅 Token 含非法字符 %q：%s", c, s1.Token)
			break
		}
	}

	active, _ := d.ActiveSubscription(u.ID)
	if active == nil || active.Token != s1.Token {
		t.Fatal("有效订阅查询失败")
	}

	// 重新生成后旧 Token 立即失效
	s2, _ := d.CreateSubscription(u.ID)
	if s2.Token == s1.Token {
		t.Error("新 Token 不应与旧 Token 相同")
	}
	if _, err := d.SubscriptionByToken(s1.Token); err == nil {
		t.Error("旧 Token 应立即失效")
	}
	if found, err := d.SubscriptionByToken(s2.Token); err != nil || found.UserID != u.ID {
		t.Fatalf("新 Token 查询失败: %v", err)
	}

	// 删除用户后级联删除订阅
	if err := d.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SubscriptionByToken(s2.Token); err == nil {
		t.Error("用户删除后订阅应级联删除")
	}
}

func TestTrafficAggregation(t *testing.T) {
	d := newTestDB(t)
	u, _ := d.CreateUser("carol", "pw", "")

	base := time.Now().Truncate(time.Hour).Unix()
	for i := int64(0); i < 3; i++ {
		if err := d.InsertSample(u.ID, base+i*3600, 100, 200); err != nil {
			t.Fatalf("写入采样失败: %v", err)
		}
	}
	// 同一小时重复写入应累加
	if err := d.InsertSample(u.ID, base, 50, 50); err != nil {
		t.Fatal(err)
	}

	tx, rx, err := d.TrafficTotals(u.ID, base, base+3*3600)
	if err != nil {
		t.Fatal(err)
	}
	if tx != 350 || rx != 650 {
		t.Errorf("聚合结果期望 tx=350 rx=650，实际 tx=%d rx=%d", tx, rx)
	}

	atx, arx, _ := d.AllTrafficTotals(base, base+3*3600)
	if atx != 350 || arx != 650 {
		t.Errorf("全局限量聚合不正确: %d/%d", atx, arx)
	}

	series, err := d.AllTrafficSeries(base, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 3 {
		t.Fatalf("曲线点数期望 3，实际 %d", len(series))
	}
	// 应升序
	if series[0].TS >= series[len(series)-1].TS {
		t.Error("曲线应按时间升序")
	}

	if err := d.PruneSamples(base + 3600); err != nil {
		t.Fatal(err)
	}
	// 保留 ts >= base+3600，即最早的一条被删除，剩余 2 条
	after, _ := d.AllTrafficSeries(base, 10)
	if len(after) != 2 {
		t.Errorf("清理后应剩 2 个采样点，实际 %d", len(after))
	}
	if after[0].TS < base+3600 {
		t.Errorf("清理后不应保留早于阈值的采样：%d", after[0].TS)
	}
}

func TestConfigVersions(t *testing.T) {
	d := newTestDB(t)

	v1, err := d.SaveConfigVersion("a: 1\n", "v2.13.0", "0.1.0", "gui")
	if err != nil {
		t.Fatalf("保存备份失败: %v", err)
	}
	v2, _ := d.SaveConfigVersion("a: 2\n", "v2.13.0", "0.1.0", "raw")

	list, err := d.ListConfigVersions(10)
	if err != nil || len(list) != 2 {
		t.Fatalf("备份列表异常: %v len=%d", err, len(list))
	}
	if !list[0].IsCurrent {
		t.Error("最新备份应标记为 current")
	}
	if list[1].IsCurrent {
		t.Error("旧备份不应标记为 current")
	}

	got, err := d.GetConfigVersion(v1.ID)
	if err != nil || got.Content != "a: 1\n" {
		t.Fatalf("读取备份内容失败: %v", err)
	}

	latest, err := d.LatestConfigVersion()
	if err != nil || latest.ID != v2.ID {
		t.Fatalf("最新备份查询失败: %v", err)
	}

	if err := d.DeleteConfigVersion(v2.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = d.ListConfigVersions(10)
	if len(list) != 1 {
		t.Errorf("删除后应剩 1 条，实际 %d", len(list))
	}
}

func TestSettings(t *testing.T) {
	d := newTestDB(t)

	if v, err := d.GetSetting("missing"); err != nil || v != "" {
		t.Fatalf("缺失设置应返回空串，实际 %q err=%v", v, err)
	}
	if err := d.SetSetting("server_host", "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSetting("server_host", "example.org"); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.GetSetting("server_host"); v != "example.org" {
		t.Errorf("设置应被覆盖，实际 %q", v)
	}
}

func TestRandomTokenUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := RandomToken(16)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatal("随机 Token 出现重复")
		}
		seen[tok] = true
		if len(tok) != 32 {
			t.Fatalf("16 字节应编码为 32 位十六进制，实际 %d", len(tok))
		}
	}
}