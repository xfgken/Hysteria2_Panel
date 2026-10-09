package hy2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 真实的 `hysteria version` 输出（含 ASCII logo 与开头空行）。
const realVersionOutput = `
░█░█░█░█░█▀▀░▀█▀░█▀▀░█▀▄░▀█▀░█▀█░░░▀▀▄
░█▀█░░█░░▀▀█░░█░░█▀▀░█▀▄░░█░░█▀█░░░▄▀░
░▀░▀░░▀░░▀▀▀░░▀░░▀▀▀░▀░▀░▀▀▀░▀░▀░░░▀▀▀
a powerful, lightning fast and censorship resistant proxy
Aperture Internet Laboratory <https://github.com/apernet>

Version:	v2.13.0
BuildDate:	2026-10-05T04:16:24Z
BuildType:	release
Toolchain:	go1.26.8 linux/amd64
Platform:	linux
Architecture:	amd64
`

func TestParseVersionOutput(t *testing.T) {
	got := parseVersionOutput(realVersionOutput)
	if got != "v2.13.0" {
		t.Fatalf("期望解析出 v2.13.0，实际 %q", got)
	}
	// 绝不能把 logo 行当成版本号
	if strings.ContainsAny(got, "░█▀▄") {
		t.Error("解析结果不应包含 ASCII logo 字符")
	}
}

func TestParseVersionOutputFallbacks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"仅版本行", "v2.13.0\n", "v2.13.0"},
		{"前后空白", "\n\n  Version:  v1.2.3  \n\n", "v1.2.3"},
		{"小写键", "version: v9.9.9\n", "v9.9.9"},
		{"无冒号但有 v 行", "logo\nsomething\nv3.0.1\n", "v3.0.1"},
		{"完全无法解析", "nothing useful here\n", ""},
		{"空输出", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseVersionOutput(c.in); got != c.want {
				t.Errorf("parseVersionOutput(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}

func TestNewClientBaseURL(t *testing.T) {
	cases := []struct {
		listen string
		want   string
	}{
		{":9999", "http://127.0.0.1:9999"},
		{"127.0.0.1:9999", "http://127.0.0.1:9999"},
		{"http://1.2.3.4:9999/", "http://1.2.3.4:9999"},
	}
	for _, c := range cases {
		got, err := NewClient(c.listen, "").EnsureBaseURL()
		if err != nil {
			t.Fatalf("解析地址失败: %v", err)
		}
		if got != c.want {
			t.Errorf("listen=%q 期望 %q，实际 %q", c.listen, c.want, got)
		}
	}
}

func TestSanitizeForValidation(t *testing.T) {
	in := `
listen: :443
mimic:
  enabled: true
  interface: eth0
masquerade:
  type: proxy
  listenHTTP: :80
  listenHTTPS: :443
  forceHTTPS: true
  proxy:
    url: https://example.com
trafficStats:
  listen: 127.0.0.1:9999
  secret: s
auth:
  type: password
  password: x
`
	out, cleanup, err := sanitizeForValidation("/nonexistent/hysteria-for-test", []byte(in))
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	defer cleanup()

	var root map[string]any
	if err := yaml.Unmarshal(out, &root); err != nil {
		t.Fatalf("净化结果不是合法 YAML: %v", err)
	}

	// listen 必须换成随机本地端口，避免占用生产端口
	if root["listen"] != "127.0.0.1:0" {
		t.Errorf("listen 未净化: %v", root["listen"])
	}

	ts, _ := root["trafficStats"].(map[string]any)
	if ts == nil || ts["listen"] != "127.0.0.1:0" {
		t.Errorf("trafficStats.listen 未净化: %v", ts)
	}
	if ts["secret"] != "s" {
		t.Error("净化不应丢失其他字段")
	}

	mimic, _ := root["mimic"].(map[string]any)
	if mimic == nil || mimic["enabled"] != false {
		t.Errorf("mimic.enabled 应被关闭: %v", mimic)
	}

	m, _ := root["masquerade"].(map[string]any)
	if m == nil {
		t.Fatal("masquerade 丢失")
	}
	for _, k := range []string{"listenHTTP", "listenHTTPS", "forceHTTPS"} {
		if _, ok := m[k]; ok {
			t.Errorf("masquerade.%s 应被移除以避免抢占 80/443", k)
		}
	}
	if m["type"] != "proxy" {
		t.Error("净化不应改动 masquerade.type")
	}

	// 原配置不得被修改
	if strings.Contains(in, "127.0.0.1:0") {
		t.Error("净化不应修改输入内容")
	}
}

func TestSanitizeRejectsBrokenYAML(t *testing.T) {
	if _, _, err := sanitizeForValidation("/nonexistent", []byte("listen: [unclosed")); err == nil {
		t.Error("非法 YAML 应返回错误")
	}
}

// 模拟官方 `hysteria cert`：把 --cert / --key 指定的文件创建出来。
func writeFakeCertTool(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-hysteria")
	script := `#!/bin/sh
cert=""; key=""
while [ $# -gt 0 ]; do
  case "$1" in
    --cert) cert="$2"; shift 2 ;;
    --key)  key="$2";  shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$cert" ] || exit 1
[ -n "$key" ] || exit 1
: > "$cert"
: > "$key"
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSanitizeReplacesACMEWithSelfSigned(t *testing.T) {
	tool := writeFakeCertTool(t)

	in := `
listen: :443
acme:
  domains:
    - real.example.org
  email: admin@real.example.org
  type: http
  http:
    altPort: 80
auth:
  type: password
  password: x
`
	out, cleanup, err := sanitizeForValidation(tool, []byte(in))
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}

	// acme 必须被替换掉，否则校验会向 CA 发起真实申请
	if _, still := root["acme"]; still {
		t.Error("校验副本不应保留 acme 块（会触发真实证书申请）")
	}
	tlsBlock, _ := root["tls"].(map[string]any)
	if tlsBlock == nil {
		t.Fatal("应将 acme 替换为 tls 块")
	}
	certPath, _ := tlsBlock["cert"].(string)
	if certPath == "" {
		t.Fatal("替换后的 tls.cert 为空")
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("替换用的临时证书不存在: %v", err)
	}

	// cleanup 应删除临时证书目录
	dir := filepath.Dir(certPath)
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup 未删除临时目录 %s", dir)
	}
}

func TestSanitizeKeepsACMEWhenCertToolMissing(t *testing.T) {
	in := `
listen: :443
acme:
  domains: [a.example.org]
  email: a@example.org
  type: http
  http:
    altPort: 80
auth:
  type: password
  password: x
`
	out, cleanup, err := sanitizeForValidation("/nonexistent/hysteria-for-test", []byte(in))
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	defer cleanup()

	var root map[string]any
	if err := yaml.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	// 生成临时证书失败时应保留原 acme，而不是静默改变语义
	if _, ok := root["acme"]; !ok {
		t.Error("无法生成临时证书时应保留 acme 块")
	}
}

func TestBinaryVersionMissingBinary(t *testing.T) {
	if _, err := BinaryVersion("/nonexistent/hysteria-for-test"); err == nil {
		t.Error("不存在的二进制应返回错误")
	}
}

func TestValidateConfigMissingBinary(t *testing.T) {
	err := ValidateConfig(context.Background(), "/nonexistent/hysteria-for-test", []byte("listen: :443\n"))
	if err == nil {
		t.Error("无法启动的二进制应返回错误")
	}
}

func TestServiceActionWithoutSystemd(t *testing.T) {
	if HasSystemd() {
		t.Skip("当前环境确有 systemd，跳过")
	}
	// 注意：systemctl 可执行文件可能存在，但 systemd 并未作为 PID 1 运行，
	// 此时任何真实操作都应返回错误，而不是静默成功。
	if _, err := ServiceAction("start", "hy2-panel-nonexistent-unit"); err == nil {
		t.Error("无 systemd 时启动服务应返回错误")
	}
	if _, err := ServiceActive("hy2-panel-nonexistent-unit"); err == nil {
		t.Error("无 systemd 时查询状态应返回错误")
	}
}