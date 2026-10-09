package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hy2-panel/hy2-panel/internal/auth"
	"github.com/hy2-panel/hy2-panel/internal/config"
	"github.com/hy2-panel/hy2-panel/internal/db"
	"github.com/hy2-panel/hy2-panel/internal/manager"
)

// testEnv 搭建一套完整的 Panel 运行时（临时目录 + SQLite + 空管理器）。
func testEnv(t *testing.T) (*httptest.Server, *http.Client, string) {
	t.Helper()
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "hysteria.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
listen: :443
auth:
  type: userpass
  userpass: {}
trafficStats:
  listen: 127.0.0.1:19999
  secret: s
`), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hash, err := auth.HashPassword("pw12345678")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAdmin("admin", hash); err != nil {
		t.Fatal(err)
	}

	mgr := manager.New(manager.Options{
		DB:           database,
		ConfigPath:   cfgPath,
		Binary:       filepath.Join(dir, "no-such-hysteria"),
		BackupDir:    filepath.Join(dir, "backup"),
		ServiceUnit:  "hysteria-server",
		PanelVersion: Version,
	})

	srv := New(Options{DB: database, Manager: mgr, DataDir: dir, TemplatePath: writeTestTemplate(t, dir)})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	return ts, client, dir
}

// writeTestTemplate 写入一份最小可用的 Clash 订阅模板，让测试自包含。
func writeTestTemplate(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "clash-meta.yaml")
	content := `proxies:
  - name: "{{ .ProxyName }}"
    type: hysteria2
    server: {{ .Server }}
    port: {{ .Port }}
    password: "{{ .Password }}"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func do(t *testing.T, c *http.Client, method, url, body, csrf string) (*http.Response, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set(auth.CSRFHeader, csrf)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func login(t *testing.T, c *http.Client, base string) string {
	t.Helper()
	resp, body := do(t, c, http.MethodPost, base+"/api/login",
		`{"username":"admin","password":"pw12345678"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录失败: HTTP %d %s", resp.StatusCode, body)
	}
	var out struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	if out.CSRFToken == "" {
		t.Fatal("登录响应缺少 CSRF Token")
	}
	return out.CSRFToken
}

func TestPublicEndpoints(t *testing.T) {
	ts, client, _ := testEnv(t)

	resp, body := do(t, client, http.MethodGet, ts.URL+"/api/health", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("健康检查应返回 200，实际 %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"panel":"ok"`) {
		t.Errorf("健康检查内容异常: %s", body)
	}

	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/version", "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, Version) {
		t.Errorf("版本接口异常: %d %s", resp.StatusCode, body)
	}
}

func TestProtectedEndpointsRequireSession(t *testing.T) {
	ts, client, _ := testEnv(t)

	for _, path := range []string{"/api/dashboard", "/api/users", "/api/server/config", "/api/settings"} {
		resp, _ := do(t, client, http.MethodGet, ts.URL+path, "", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s 未登录应返回 401，实际 %d", path, resp.StatusCode)
		}
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ts, client, _ := testEnv(t)

	resp, _ := do(t, client, http.MethodPost, ts.URL+"/api/login",
		`{"username":"admin","password":"wrong"}`, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("错误密码应返回 401，实际 %d", resp.StatusCode)
	}

	resp, _ = do(t, client, http.MethodPost, ts.URL+"/api/login",
		`{"username":"nobody","password":"whatever"}`, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("不存在的用户应返回 401，实际 %d", resp.StatusCode)
	}
}

func TestCSRFIsEnforced(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	// 缺少 CSRF 头的写操作必须被拒绝
	resp, _ := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"username":"alice","password":"pw"}`, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("缺少 CSRF 头应返回 403，实际 %d", resp.StatusCode)
	}

	// 错误的 CSRF Token 同样被拒绝
	resp, _ = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"username":"alice","password":"pw"}`, "bogus-token")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("错误 CSRF Token 应返回 403，实际 %d", resp.StatusCode)
	}

	// 正确 Token 放行
	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"username":"alice","password":"pw"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("带正确 CSRF 应创建成功，实际 %d %s", resp.StatusCode, body)
	}
}

func TestSingleAccountLifecycleAndSecretHiding(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	// 创建唯一账号：账号名由面板生成，请求里不需要提交用户名
	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"alicepass","note":"测试"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建账号失败: %d %s", resp.StatusCode, body)
	}
	var created struct {
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	name := created.User.Username
	if !strings.HasPrefix(name, db.UsernamePrefix) || len(name) != len(db.UsernamePrefix)+6 {
		t.Fatalf("账号名应为 hysteria2-xxxxxx，实际 %q", name)
	}

	// 列表不得泄露密码，且只应有一个账号
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("账号列表失败: %d", resp.StatusCode)
	}
	if strings.Contains(body, "alicepass") {
		t.Error("列表不应返回 HY2 密码明文")
	}
	if !strings.Contains(body, `"total":1`) {
		t.Errorf("单用户模式应只有一个账号: %s", body)
	}

	// 多账号：允许再创建一个（各自独立密码与订阅）
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users", `{"password":"pw2"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("应允许创建第二个账号，实际 %d %s", resp.StatusCode, body)
	}
	var second struct {
		User struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	_ = json.Unmarshal([]byte(body), &second)
	if second.User.ID == 0 || second.User.ID == created.User.ID {
		t.Fatalf("第二个账号 ID 异常: %s", body)
	}
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if !strings.Contains(body, `"total":2`) {
		t.Errorf("创建后应有两个账号: %s", body)
	}

	// 多账号必须全部写进官方配置的 userpass，否则第二个账号连不上
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/server/config/raw", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取原始配置失败: %d %s", resp.StatusCode, body)
	}
	var raw struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal([]byte(body), &raw)
	if !strings.Contains(raw.Content, name) {
		t.Errorf("配置 userpass 应包含第一个账号 %s:\n%s", name, raw.Content)
	}
	if !strings.Contains(raw.Content, second.User.Username) {
		t.Errorf("配置 userpass 应包含第二个账号 %s（多账号同步缺失）:\n%s",
			second.User.Username, raw.Content)
	}

	// HY2 URI（同时返回单端口兼容版）
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users/1/uri", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("URI 生成失败: %d %s", resp.StatusCode, body)
	}
	var uriInfo struct {
		URI       string `json:"uri"`
		URISingle string `json:"uriSingle"`
	}
	_ = json.Unmarshal([]byte(body), &uriInfo)
	if !strings.HasPrefix(uriInfo.URI, "hysteria2://") {
		t.Errorf("URI 格式异常: %s", uriInfo.URI)
	}
	if uriInfo.URISingle == "" {
		t.Error("应同时返回单端口兼容 URI")
	}

	// 订阅
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users/1/subscription", "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "/sub/") {
		t.Fatalf("订阅信息异常: %d %s", resp.StatusCode, body)
	}
	var subInfo struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(body), &subInfo)

	resp, body = do(t, client, http.MethodGet, ts.URL+"/sub/"+subInfo.Token, "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "type: hysteria2") {
		t.Fatalf("订阅渲染失败: %d %s", resp.StatusCode, body)
	}

	// 停用后订阅不可用
	resp, _ = do(t, client, http.MethodPut, ts.URL+"/api/users/1", `{"enabled":false}`, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("更新账号失败: %d", resp.StatusCode)
	}
	resp, _ = do(t, client, http.MethodGet, ts.URL+"/sub/"+subInfo.Token, "", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("停用后订阅应返回 403，实际 %d", resp.StatusCode)
	}

	// 多账号时删除其中一个：不触发补建
	resp, body = do(t, client, http.MethodDelete,
		ts.URL+"/api/users/"+fmt.Sprintf("%d", second.User.ID), "", csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("多账号时应允许删除，实际 %d %s", resp.StatusCode, body)
	}
	if strings.Contains(body, `"replacement"`) {
		t.Errorf("还有账号剩余时不应补建: %s", body)
	}

	// 允许删除最后一个账号：面板立即补建一个随机账号，保证官方 Core 仍可启动
	resp, body = do(t, client, http.MethodDelete,
		ts.URL+"/api/users/"+fmt.Sprintf("%d", created.User.ID), "", csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除最后一个账号应成功，实际 %d %s", resp.StatusCode, body)
	}
	var delResp struct {
		Replacement *struct {
			Username string `json:"username"`
		} `json:"replacement"`
		ReplacementPassword string `json:"replacementPassword"`
	}
	_ = json.Unmarshal([]byte(body), &delResp)
	if delResp.Replacement == nil || delResp.ReplacementPassword == "" {
		t.Errorf("删空后应返回补建账号与密码: %s", body)
	} else if !strings.HasPrefix(delResp.Replacement.Username, db.UsernamePrefix) {
		t.Errorf("补建账号名格式异常: %s", delResp.Replacement.Username)
	}
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if !strings.Contains(body, `"total":1`) {
		t.Errorf("删空后应自动补建一个账号: %s", body)
	}
}

func TestCreateUserWithCustomSuffix(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	// 合法后缀：账号名 = hysteria2- + 自定义 6 位
	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"Ab12Cd"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("自定义后缀创建失败: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"username":"hysteria2-Ab12Cd"`) {
		t.Errorf("账号名应为 hysteria2-Ab12Cd: %s", body)
	}

	// 重名
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"Ab12Cd"}`, csrf)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("重复名称应返回 409，实际 %d %s", resp.StatusCode, body)
	}

	// 长度不再限制为 6：3 位这种短后缀也是合法的
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"abc"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("短后缀应被接受，实际 %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"username":"hysteria2-abc"`) {
		t.Errorf("账号名应为 hysteria2-abc: %s", body)
	}

	// 任意长度（例如好记的英文名）
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"family"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("自定义长度后缀应被接受，实际 %d %s", resp.StatusCode, body)
	}

	// 非法字符
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"ab-cd!"}`, csrf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("后缀含非法字符应返回 400，实际 %d %s", resp.StatusCode, body)
	}

	// 超长
	long := strings.Repeat("a", 40)
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"`+long+`"}`, csrf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("超长后缀应返回 400，实际 %d %s", resp.StatusCode, body)
	}
}

func TestRenameUserViaUpdate(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw","suffix":"before"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建失败: %d %s", resp.StatusCode, body)
	}
	var created struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal([]byte(body), &created)
	url := ts.URL + "/api/users/" + fmt.Sprintf("%d", created.User.ID)

	// 改名（只改 hysteria2- 之后的部分）
	resp, body = do(t, client, http.MethodPut, url, `{"suffix":"after"}`, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("改名失败: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"username":"hysteria2-after"`) {
		t.Errorf("名称应为 hysteria2-after: %s", body)
	}

	// 官方配置也要同步
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/server/config/raw", "", "")
	if !strings.Contains(body, "hysteria2-after") {
		t.Errorf("配置 userpass 未同步新名称:\n%s", body)
	}
	if strings.Contains(body, "hysteria2-before") {
		t.Errorf("配置里不应再有旧名称:\n%s", body)
	}

	// 非法后缀应被拒绝
	resp, body = do(t, client, http.MethodPut, url, `{"suffix":"ab-cd"}`, csrf)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("非法后缀应返回 400，实际 %d %s", resp.StatusCode, body)
	}
}

func TestListenValidation(t *testing.T) {
	valid := []string{":443", ":20000-50000", "127.0.0.1:8443", "[::]:443",
		"example.com:443", "realm://token@host/realm"}
	for _, s := range valid {
		if !config.ValidListen(s) {
			t.Errorf("应视为合法监听地址: %q", s)
		}
	}
	invalid := []string{":", "", "443", ":abc", ":99999x", ":::443", "http://x"}
	for _, s := range invalid {
		if config.ValidListen(s) {
			t.Errorf("应视为非法监听地址: %q", s)
		}
	}
}

func TestApplyConfigKeepsAllAccounts(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	// 两个自定义名称的账号
	for _, s := range []string{"alpha", "beta"} {
		resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
			`{"password":"pw-`+s+`","suffix":"`+s+`"}`, csrf)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("创建 %s 失败: %d %s", s, resp.StatusCode, body)
		}
	}

	// 读取结构化配置，改一个与账号无关的字段
	resp, body := do(t, client, http.MethodGet, ts.URL+"/api/server/config", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取配置失败: %d", resp.StatusCode)
	}
	var view struct {
		Config map[string]any `json:"config"`
	}
	_ = json.Unmarshal([]byte(body), &view)
	view.Config["udpIdleTimeout"] = "66s"
	payload, _ := json.Marshal(map[string]any{"config": view.Config})

	resp, body = do(t, client, http.MethodPut, ts.URL+"/api/server/config", string(payload), csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("保存配置失败: %d %s", resp.StatusCode, body)
	}

	// 关键：保存配置不能把其它账号从 userpass 里抹掉，也不能改掉自定义名称
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/server/config/raw", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取原始配置失败: %d", resp.StatusCode)
	}
	for _, s := range []string{"hysteria2-alpha", "hysteria2-beta"} {
		if !strings.Contains(body, s) {
			t.Errorf("保存配置后账号 %s 丢失（userpass 被覆盖）:\n%s", s, body)
		}
	}
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if !strings.Contains(body, `"total":2`) {
		t.Errorf("账号应仍为 2 个: %s", body)
	}
}

// TestApplyConfigRotatesTokens 验证「配置一改就自动换订阅 Token」。
//
// 面板不再提供「重置 Token」按钮，改为保存/应用配置后自动轮换全部启用账号的 Token。
func TestApplyConfigRotatesTokens(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw-rot","suffix":"rot1"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建账号失败: %d %s", resp.StatusCode, body)
	}

	// 取出账号 id 与当前订阅 Token
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取账号列表失败: %d", resp.StatusCode)
	}
	var list struct {
		Items []struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) == 0 {
		t.Fatalf("解析账号列表失败: %v %s", err, body)
	}
	id := list.Items[0].ID

	resp, body = do(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/users/%d/subscription", ts.URL, id), "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取订阅失败: %d %s", resp.StatusCode, body)
	}
	var sub struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(body), &sub)
	if sub.Token == "" {
		t.Fatalf("订阅 Token 为空: %s", body)
	}

	// 保存配置（只改一个与账号无关的字段）
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/server/config", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取配置失败: %d", resp.StatusCode)
	}
	var view struct {
		Config map[string]any `json:"config"`
	}
	_ = json.Unmarshal([]byte(body), &view)
	view.Config["udpIdleTimeout"] = "67s"
	payload, _ := json.Marshal(map[string]any{"config": view.Config})

	resp, body = do(t, client, http.MethodPut, ts.URL+"/api/server/config", string(payload), csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("保存配置失败: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"rotatedTokens":1`) {
		t.Errorf("响应里应报告轮换了 1 个账号: %s", body)
	}

	// Token 必须换成新的，且长度仍是 20
	resp, body = do(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/users/%d/subscription", ts.URL, id), "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("再次读取订阅失败: %d %s", resp.StatusCode, body)
	}
	var sub2 struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal([]byte(body), &sub2)
	if sub2.Token == sub.Token {
		t.Errorf("保存配置后订阅 Token 未轮换（仍是 %s）", sub.Token)
	}
	if len(sub2.Token) != 20 {
		t.Errorf("Token 长度应为 20，实际 %d（%s）", len(sub2.Token), sub2.Token)
	}
}

// TestUpdateUserRotatesToken 验证「改账号（密码 / 名称）也会换该账号的订阅 Token」。
func TestUpdateUserRotatesToken(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	resp, body := do(t, client, http.MethodPost, ts.URL+"/api/users",
		`{"password":"pw-rot","suffix":"rot2"}`, csrf)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("创建账号失败: %d %s", resp.StatusCode, body)
	}

	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) == 0 {
		t.Fatalf("解析账号列表失败: %v %s", err, body)
	}
	id := list.Items[0].ID

	readToken := func() string {
		_, b := do(t, client, http.MethodGet,
			fmt.Sprintf("%s/api/users/%d/subscription", ts.URL, id), "", "")
		var s struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal([]byte(b), &s)
		return s.Token
	}
	before := readToken()
	if before == "" {
		t.Fatal("订阅 Token 为空")
	}

	// 只改密码
	resp, body = do(t, client, http.MethodPut, fmt.Sprintf("%s/api/users/%d", ts.URL, id),
		`{"password":"newpw-123456"}`, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("修改密码失败: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"rotatedToken":true`) {
		t.Errorf("修改账号后应报告已轮换 Token: %s", body)
	}
	if after := readToken(); after == before {
		t.Errorf("改密码后订阅 Token 未轮换（仍是 %s）", before)
	}

	// 改名同样会换
	mid := readToken()
	resp, body = do(t, client, http.MethodPut, fmt.Sprintf("%s/api/users/%d", ts.URL, id),
		`{"suffix":"rot3"}`, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("改名失败: %d %s", resp.StatusCode, body)
	}
	if after := readToken(); after == mid {
		t.Errorf("改名后订阅 Token 未轮换（仍是 %s）", mid)
	}
}

// TestOnlineMatchingIsCaseInsensitive 验证大小写账号名也能对上官方的实时数据。
//
// 官方 Core 会把 userpass 的用户名小写化后作为 stats API 的 client id，
// 面板必须同样小写对账，否则「在线状态」与「累计流量」永远读不到。
func TestOnlineMatchingIsCaseInsensitive(t *testing.T) {
	stats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/online":
			_, _ = w.Write([]byte(`{"hysteria2-abcxyz":2}`))
		case "/traffic":
			_, _ = w.Write([]byte(`{"hysteria2-abcxyz":{"tx":111,"rx":222}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(stats.Close)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "hysteria.yaml")
	cfg := "listen: :443\n" +
		"auth:\n  type: userpass\n  userpass:\n    hysteria2-AbCxYz: pw\n" +
		"trafficStats:\n  listen: " + strings.TrimPrefix(stats.URL, "http://") + "\n  secret: s\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.CreateUser("hysteria2-AbCxYz", "pw", ""); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("pw12345678")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAdmin("admin", hash); err != nil {
		t.Fatal(err)
	}

	mgr := manager.New(manager.Options{
		DB:           database,
		ConfigPath:   cfgPath,
		Binary:       filepath.Join(dir, "no-such-hysteria"),
		BackupDir:    filepath.Join(dir, "backup"),
		ServiceUnit:  "hysteria-server",
		PanelVersion: Version,
	})
	srv := New(Options{DB: database, Manager: mgr, DataDir: dir, TemplatePath: writeTestTemplate(t, dir)})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	_ = login(t, client, ts.URL)

	resp, body := do(t, client, http.MethodGet, ts.URL+"/api/users", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取账号列表失败: %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"online":2`) {
		t.Errorf("大写账号名应能匹配到官方在线数据（期望 online:2）: %s", body)
	}
	if !strings.Contains(body, `"sessionTx":111`) {
		t.Errorf("大写账号名应能匹配到官方实时流量（期望 sessionTx:111）: %s", body)
	}
}

func TestConfigEndpoints(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	// 读取配置（结构化视图）
	resp, body := do(t, client, http.MethodGet, ts.URL+"/api/server/config", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("读取配置失败: %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"config"`) || !strings.Contains(body, `"issues"`) {
		t.Errorf("配置响应结构异常: %s", body)
	}

	// 仅校验：非法配置必须报错
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/server/config/validate",
		`{"config":{"auth":{"type":"brutal"}}}`, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("校验接口返回异常: %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("非法认证类型应校验失败: %s", body)
	}

	// 仅校验：合法配置通过
	resp, body = do(t, client, http.MethodPost, ts.URL+"/api/server/config/validate",
		`{"config":{"listen":":443","auth":{"type":"password","password":"x"}}}`, csrf)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Errorf("合法配置应校验通过: %d %s", resp.StatusCode, body)
	}

	// 原始配置读取
	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/server/config/raw", "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "listen") {
		t.Errorf("原始配置读取异常: %d %s", resp.StatusCode, body)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	payload := `{"serverHost":"example.com","subBaseURL":"https://example.com","mixedPort":7890,"logHysteria":"journal:hysteria-server","logPanel":"/tmp/panel.log","logNginx":"/var/log/nginx/error.log"}`
	resp, body := do(t, client, http.MethodPut, ts.URL+"/api/settings", payload, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("保存设置失败: %d %s", resp.StatusCode, body)
	}

	resp, body = do(t, client, http.MethodGet, ts.URL+"/api/settings", "", "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "example.com") {
		t.Errorf("读取设置异常: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"mixedPort":7890`) {
		t.Errorf("整型设置未正确保存: %s", body)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	ts, client, _ := testEnv(t)
	csrf := login(t, client, ts.URL)

	resp, _ := do(t, client, http.MethodPost, ts.URL+"/api/logout", "", csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登出失败: %d", resp.StatusCode)
	}

	resp, _ = do(t, client, http.MethodGet, ts.URL+"/api/dashboard", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("登出后应返回 401，实际 %d", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	ts, client, _ := testEnv(t)

	resp, _ := do(t, client, http.MethodGet, ts.URL+"/api/health", "", "")
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("缺少 X-Content-Type-Options 响应头")
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("缺少 X-Frame-Options 响应头")
	}
}