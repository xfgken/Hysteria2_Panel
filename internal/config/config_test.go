package config

import (
	"strings"
	"testing"
)

const sampleYAML = `
listen: :443
acme:
  domains:
    - example.com
  email: a@b.c
  type: dns
  dns:
    name: cloudflare
    config:
      cloudflare_api_token: token123
trafficStats:
  listen: 127.0.0.1:9999
  secret: s3cret
auth:
  type: userpass
  userpass:
    alice: alicepass
quic:
  maxIdleTimeout: 30s
  maxIncomingStreams: 1024
congestion:
  type: bbr
  bbrProfile: standard
outbounds:
  - name: default
    type: direct
# 官方未来新增的字段（Panel 尚未建模）
futureFeature:
  enabled: true
`

func TestParseAndMarshalRoundTrip(t *testing.T) {
	cfg, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if cfg.Server.Listen != ":443" {
		t.Errorf("listen 期望 :443，实际 %q", cfg.Server.Listen)
	}
	if cfg.Server.ACME == nil || cfg.Server.ACME.DNS == nil {
		t.Fatal("acme.dns 未解析")
	}
	if got := cfg.Server.ACME.DNS.Config["cloudflare_api_token"]; got != "token123" {
		t.Errorf("dns config 期望 token123，实际 %v", got)
	}
	if cfg.Server.Auth == nil || cfg.Server.Auth.Userpass["alice"] != "alicepass" {
		t.Error("userpass 用户未正确解析")
	}
	if cfg.Server.QUIC == nil || cfg.Server.QUIC.MaxIncomingStreams != 1024 {
		t.Error("quic 字段未正确解析")
	}

	out, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 未知字段不出现在结构体序列化结果中（但会通过 UnknownTopLevelKeys 提示）
	if strings.Contains(string(out), "futureFeature") {
		t.Error("结构体序列化不应包含未建模字段")
	}

	again, err := Parse(out)
	if err != nil {
		t.Fatalf("二次解析失败: %v", err)
	}
	if again.Server.Listen != cfg.Server.Listen || again.Server.Auth.Userpass["alice"] != "alicepass" {
		t.Error("往返后配置不一致")
	}
}

func TestUnknownTopLevelKeys(t *testing.T) {
	cfg, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	keys := cfg.UnknownTopLevelKeys()
	if len(keys) != 1 || keys[0] != "futureFeature" {
		t.Errorf("期望识别到 futureFeature，实际 %v", keys)
	}
	if !cfg.HasUnknownFields() {
		t.Error("HasUnknownFields 应为 true")
	}
}

func TestValidateHappyPath(t *testing.T) {
	cfg, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	issues := Validate(&cfg.Server)
	if HasError(issues) {
		t.Fatalf("合法配置不应有阻断错误: %+v", issues)
	}
}

func TestValidateDetectsProblems(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name: "tls 与 acme 冲突",
			yaml: `
listen: :443
tls: { cert: c, key: k }
acme: { domains: [a.com], email: a@b.c, type: dns, dns: { name: cloudflare } }
auth: { type: password, password: x }
`,
			wantSub: "tls 与 acme 不能同时配置",
		},
		{
			name: "Brutal 不是拥塞控制类型",
			yaml: `
listen: :443
congestion: { type: brutal }
auth: { type: password, password: x }
`,
			wantSub: "Brutal 由 bandwidth 触发",
		},
		{
			name:    "未配置认证",
			yaml:    "listen: :443\n",
			wantSub: "必须配置认证方式",
		},
		{
			name: "socks5 缺少地址",
			yaml: `
listen: :443
auth: { type: password, password: x }
outbounds:
  - name: s
    type: socks5
`,
			wantSub: "必须提供 addr",
		},
		{
			name: "acme 缺少邮箱",
			yaml: `
listen: :443
acme: { domains: [a.com], type: http, http: { altPort: 80 } }
auth: { type: password, password: x }
`,
			wantSub: "acme.email 为必填",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Parse([]byte(c.yaml))
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			issues := Validate(&cfg.Server)
			if !HasError(issues) {
				t.Fatalf("期望产生阻断错误，实际 %+v", issues)
			}
			found := false
			for _, it := range issues {
				if strings.Contains(it.Message, c.wantSub) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("期望包含 %q，实际 %+v", c.wantSub, issues)
			}
		})
	}
}

func TestDefaultsMatchOfficial(t *testing.T) {
	d := Defaults()
	if d.Listen != ":443" {
		t.Errorf("默认 listen 期望 :443，实际 %q", d.Listen)
	}
	if d.QUIC == nil || d.QUIC.MaxIncomingStreams != 1024 {
		t.Error("QUIC 默认值不符合官方文档")
	}
	if d.Congestion == nil || d.Congestion.Type != "bbr" {
		t.Error("默认拥塞控制应为 bbr")
	}
	if d.TrafficStats == nil || d.TrafficStats.Listen == "" {
		t.Error("默认应启用 trafficStats（面板依赖）")
	}
}