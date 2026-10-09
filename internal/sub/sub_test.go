package sub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hy2-panel/hy2-panel/internal/config"
)

func TestSplitListen(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{":443", "", "443"},
		{":20000-50000", "", "20000-50000"},
		{"0.0.0.0:443", "", "443"},
		{"[::]:443", "", "443"},
		{"1.2.3.4:8443", "1.2.3.4", "8443"},
		{"realm://t@r.example.com/x", "", ""},
		{"", "", "443"},
	}
	for _, c := range cases {
		host, port := splitListen(c.in)
		if host != c.wantHost || port != c.wantPort {
			t.Errorf("splitListen(%q) = (%q,%q)，期望 (%q,%q)", c.in, host, port, c.wantHost, c.wantPort)
		}
	}
}

func TestBuildURIUserpass(t *testing.T) {
	sc := &config.ServerConfig{
		Listen: ":443",
		Auth:   &config.AuthConfig{Type: "userpass", Userpass: map[string]string{"alice": "pw"}},
		ACME: &config.ACMEConfig{
			Domains: []string{"*.example.com"},
			Email:   "a@b.c",
			Type:    "http",
			HTTP:    &config.ACMEHTTPConfig{AltPort: 80},
		},
	}

	uri, err := BuildURI(sc, "alice", "pw", "example.com")
	if err != nil {
		t.Fatalf("生成 URI 失败: %v", err)
	}

	// 关键点：userpass 的分隔冒号不能被编码为 %3A
	if !strings.HasPrefix(uri, "hysteria2://alice:pw@example.com:443/") {
		t.Errorf("URI 认证段格式不正确: %s", uri)
	}
	if strings.Contains(uri, "%3A") {
		t.Errorf("URI 不应把 userpass 冒号编码: %s", uri)
	}
	if !strings.Contains(uri, "sni=example.com") {
		t.Errorf("应包含从 ACME 推导的 SNI: %s", uri)
	}
	if !strings.Contains(uri, "#alice") {
		t.Errorf("应包含用户名片段: %s", uri)
	}
}

func TestBuildURIPasswordAndObfs(t *testing.T) {
	sc := &config.ServerConfig{
		Listen: ":443",
		Auth:   &config.AuthConfig{Type: "password", Password: "secret"},
		Obfs: &config.ObfsConfig{
			Type:       "salamander",
			Salamander: &config.SalamanderConfig{Password: "obfspw"},
		},
	}

	uri, err := BuildURI(sc, "", "secret", "1.2.3.4")
	if err != nil {
		t.Fatalf("生成 URI 失败: %v", err)
	}
	if !strings.Contains(uri, "hysteria2://secret@1.2.3.4:443/") {
		t.Errorf("password 模式 URI 不正确: %s", uri)
	}
	if !strings.Contains(uri, "obfs=salamander") || !strings.Contains(uri, "obfs-password=obfspw") {
		t.Errorf("应输出混淆参数: %s", uri)
	}
}

func TestBuildURIPortHopping(t *testing.T) {
	sc := &config.ServerConfig{
		Listen: ":20000-50000",
		Auth:   &config.AuthConfig{Type: "password", Password: "x"},
	}
	uri, err := BuildURI(sc, "", "x", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uri, "@example.com:20000-50000/") {
		t.Errorf("端口跳跃格式不正确: %s", uri)
	}
}

func TestBuildURINoHost(t *testing.T) {
	sc := &config.ServerConfig{Listen: ":443", Auth: &config.AuthConfig{Type: "password", Password: "x"}}
	if _, err := BuildURI(sc, "", "x", ""); err == nil {
		t.Error("缺少服务器地址时应返回错误")
	}
}

func TestSinglePortURI(t *testing.T) {
	cases := map[string]string{
		// 纯端口范围 → 取第一个端口
		"hysteria2://u:p@1.2.3.4:20000-50000/?obfs=salamander#u": "hysteria2://u:p@1.2.3.4:20000/?obfs=salamander#u",
		// 单端口不动
		"hysteria2://u:p@1.2.3.4:443/?insecure=1": "hysteria2://u:p@1.2.3.4:443/?insecure=1",
		// 混合列表 → 取第一个
		"hysteria2://u:p@1.2.3.4:123,5000-6000/#n": "hysteria2://u:p@1.2.3.4:123/#n",
		// 密码里含冒号与连字符数字，不能被误改
		"hysteria2://user1:abc-123@1.2.3.4:20000-30000/#n": "hysteria2://user1:abc-123@1.2.3.4:20000/#n",
		// 没有端口
		"hysteria2://u:p@example.com/#n": "hysteria2://u:p@example.com/#n",
		// IPv6 主机
		"hysteria2://u:p@[2001:db8::1]:20000-30000/#n": "hysteria2://u:p@[2001:db8::1]:20000/#n",
	}
	for in, want := range cases {
		if got := SinglePortURI(in); got != want {
			t.Errorf("SinglePortURI(%q)\n = %q\n期望 %q", in, got, want)
		}
	}
}

func TestRenderClashOmitsObfsWhenDisabled(t *testing.T) {
	tpl := `proxies:
  - name: "{{ .ProxyName }}"
    type: hysteria2
    server: {{ .Server }}
    port: {{ .Port }}
    password: "{{ .Password }}"
{{- if .SNI }}
    sni: {{ .SNI }}
{{- end }}
{{- if .ObfsType }}
    obfs: {{ .ObfsType }}
{{- end }}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clash.yaml")
	if err := os.WriteFile(path, []byte(tpl), 0o600); err != nil {
		t.Fatal(err)
	}

	sc := &config.ServerConfig{
		Listen: ":443",
		Auth:   &config.AuthConfig{Type: "userpass"},
	}
	data, err := BuildClashData(sc, "alice", "pw", "example.com", 7890)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RenderClash(path, data)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	body := string(out)
	if strings.Contains(body, "obfs") {
		t.Errorf("未启用混淆时不应输出 obfs: %s", body)
	}
	if !strings.Contains(body, `name: "hysteria2-alice"`) {
		t.Errorf("代理名不正确: %s", body)
	}
	if !strings.Contains(body, "server: example.com") {
		t.Errorf("服务器地址不正确: %s", body)
	}
}

func TestRenderClashIncludesObfsWhenEnabled(t *testing.T) {
	tpl := `{{- if .ObfsType }}
obfs: {{ .ObfsType }}
obfs-password: "{{ .ObfsPassword }}"
{{- end }}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "clash.yaml")
	if err := os.WriteFile(path, []byte(tpl), 0o600); err != nil {
		t.Fatal(err)
	}

	sc := &config.ServerConfig{
		Listen: ":443",
		Obfs: &config.ObfsConfig{
			Type:       "salamander",
			Salamander: &config.SalamanderConfig{Password: "op"},
		},
	}
	data, err := BuildClashData(sc, "bob", "pw", "example.com", 7890)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := RenderClash(path, data)
	if !strings.Contains(string(out), "obfs: salamander") || !strings.Contains(string(out), `obfs-password: "op"`) {
		t.Errorf("启用混淆时应输出对应参数: %s", out)
	}
}

// TestProxyNameNoDuplicatePrefix 确认 Clash 节点名不会出现 hysteria2-hysteria2-xxx。
func TestProxyNameNoDuplicatePrefix(t *testing.T) {
	cases := map[string]string{
		"hysteria2-hzd": "hysteria2-hzd",
		"hysteria2-AbC": "hysteria2-AbC",
		"hzd":           "hysteria2-hzd",
	}
	for in, want := range cases {
		if got := proxyName(in); got != want {
			t.Errorf("proxyName(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestClashPasswordCarriesUsername 确认 Clash 订阅里的 password 是
//「用户名:密码」整串，而不是光秃秃的密码。
//
// hysteria2 的 userpass 认证要求客户端发送完整 auth 串，只发密码会被官方
// Core 直接拒绝（404 authentication failed），客户端表现为「节点能导入、
// 能显示，但延迟永远测不出来、流量不通」。
//
// HY2 链接（hysteria2://user:pass@host）天生带用户名，所以这个坑只在
// Clash 订阅里才会出现 —— 曾经就是因为少了这个前缀，害得 Clash 一直连不上。
func TestClashPasswordCarriesUsername(t *testing.T) {
	sc := &config.ServerConfig{
		Listen: ":443",
		Auth:   &config.AuthConfig{Type: "userpass"},
	}
	data, err := BuildClashData(sc, "hysteria2-hzd", "s3cret", "example.com", 7890)
	if err != nil {
		t.Fatal(err)
	}
	if want := "hysteria2-hzd:s3cret"; data.Password != want {
		t.Errorf("ClashData.Password = %q，期望 %q", data.Password, want)
	}

	tpl := "password: \"{{ .Password }}\"\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "clash.yaml")
	if err := os.WriteFile(path, []byte(tpl), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := RenderClash(path, data)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(string(out), `password: "hysteria2-hzd:s3cret"`) {
		t.Errorf("渲染结果应带用户名前缀，实际: %s", out)
	}
}
