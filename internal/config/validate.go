package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity 校验结果的严重级别。
type Severity string

const (
	// SeverityError 表示配置无法启动服务。
	SeverityError Severity = "error"
	// SeverityWarning 表示可以启动，但存在风险或可疑设置。
	SeverityWarning Severity = "warning"
)

// Issue 单条校验结果。
type Issue struct {
	Severity Severity `json:"severity"`
	Field    string   `json:"field"`
	Message  string   `json:"message"`
}

// Validate 对配置做静态校验。
//
// 说明：这里只做“能提前发现”的语义校验；最终以官方 Core 的
// `hysteria server -c config.yaml` 加载结果为准（见开发文档 49 节）。
func Validate(sc *ServerConfig) []Issue {
	var issues []Issue
	errf := func(field, format string, args ...any) {
		issues = append(issues, Issue{SeverityError, field, fmt.Sprintf(format, args...)})
	}
	warnf := func(field, format string, args ...any) {
		issues = append(issues, Issue{SeverityWarning, field, fmt.Sprintf(format, args...)})
	}

	// ---- 基础 ----
	if sc.Listen == "" {
		warnf("listen", "未设置 listen，官方将默认监听 :443")
	} else if !ValidListen(sc.Listen) {
		errf("listen", "监听地址格式不正确：%q。正确示例：:443、:20000-50000、127.0.0.1:8443、realm://token@host/realm",
			sc.Listen)
	}

	// ---- TLS 与 ACME 互斥 ----
	if sc.TLS != nil && sc.ACME != nil {
		errf("tls/acme", "tls 与 acme 不能同时配置")
	}
	if sc.TLS != nil {
		if sc.TLS.Cert == "" || sc.TLS.Key == "" {
			errf("tls", "tls.cert 与 tls.key 均为必填")
		}
		switch sc.TLS.SNIGuard {
		case "", "strict", "disable", "dns-san":
		default:
			errf("tls.sniGuard", "取值必须是 strict / disable / dns-san")
		}
	}
	if sc.ACME != nil {
		if len(sc.ACME.Domains) == 0 {
			errf("acme.domains", "至少需要一个域名")
		}
		if sc.ACME.Email == "" {
			errf("acme.email", "acme.email 为必填")
		}
		switch sc.ACME.Type {
		case "http":
			if sc.ACME.HTTP == nil {
				errf("acme.http", "acme.type=http 时必须提供 acme.http")
			}
		case "tls":
			if sc.ACME.TLS == nil {
				errf("acme.tls", "acme.type=tls 时必须提供 acme.tls")
			}
		case "dns":
			if sc.ACME.DNS == nil || sc.ACME.DNS.Name == "" {
				errf("acme.dns", "acme.type=dns 时必须提供 acme.dns.name")
			}
		case "":
			errf("acme.type", "未设置 acme.type（http / tls / dns）")
		default:
			errf("acme.type", "不支持的类型 %q", sc.ACME.Type)
		}
	}

	// ---- 混淆 ----
	if sc.Obfs != nil {
		switch sc.Obfs.Type {
		case "salamander":
			if sc.Obfs.Salamander == nil || sc.Obfs.Salamander.Password == "" {
				errf("obfs.salamander", "Salamander 混淆必须提供 password")
			}
		case "gecko":
			if sc.Obfs.Gecko == nil || sc.Obfs.Gecko.Password == "" {
				errf("obfs.gecko", "Gecko 混淆必须提供 password")
			}
		case "":
			errf("obfs.type", "未设置混淆类型")
		default:
			errf("obfs.type", "不支持的类型 %q（仅 salamander / gecko）", sc.Obfs.Type)
		}
	}

	// ---- 拥塞控制 ----
	if sc.Congestion != nil {
		switch sc.Congestion.Type {
		case "bbr":
			switch sc.Congestion.BBRProfile {
			case "", "standard", "conservative", "aggressive":
			default:
				errf("congestion.bbrProfile", "取值必须是 standard / conservative / aggressive")
			}
		case "reno":
			if sc.Congestion.BBRProfile != "" {
				warnf("congestion.bbrProfile", "bbrProfile 仅在 type=bbr 时生效")
			}
		case "":
			warnf("congestion.type", "未设置，官方默认 bbr")
		default:
			// Brutal 通过 bandwidth 触发，并非 congestion.type 的取值。
			errf("congestion.type", "不支持的类型 %q（仅 bbr / reno；Brutal 由 bandwidth 触发）", sc.Congestion.Type)
		}
	}

	// ---- 认证 ----
	if sc.Auth == nil {
		errf("auth", "必须配置认证方式")
	} else {
		switch sc.Auth.Type {
		case "password":
			if sc.Auth.Password == "" {
				errf("auth.password", "password 认证必须提供密码")
			}
		case "userpass":
			if len(sc.Auth.Userpass) == 0 {
				warnf("auth.userpass", "userpass 认证当前没有任何用户")
			}
		case "http":
			if sc.Auth.HTTP == nil || sc.Auth.HTTP.URL == "" {
				errf("auth.http", "http 认证必须提供 url")
			}
		case "command":
			if sc.Auth.Command == "" {
				errf("auth.command", "command 认证必须提供可执行文件路径")
			}
		case "":
			errf("auth.type", "未设置认证类型")
		default:
			errf("auth.type", "不支持的类型 %q", sc.Auth.Type)
		}
	}

	// ---- 解析器 ----
	if sc.Resolver != nil {
		switch sc.Resolver.Type {
		case "udp":
			if sc.Resolver.UDP == nil || sc.Resolver.UDP.Addr == "" {
				errf("resolver.udp", "必须提供 addr")
			}
		case "tcp":
			if sc.Resolver.TCP == nil || sc.Resolver.TCP.Addr == "" {
				errf("resolver.tcp", "必须提供 addr")
			}
		case "tls":
			if sc.Resolver.TLS == nil || sc.Resolver.TLS.Addr == "" {
				errf("resolver.tls", "必须提供 addr")
			}
		case "https":
			if sc.Resolver.HTTPS == nil || sc.Resolver.HTTPS.Addr == "" {
				errf("resolver.https", "必须提供 addr")
			}
		case "":
			errf("resolver.type", "未设置解析器类型")
		default:
			errf("resolver.type", "不支持的类型 %q", sc.Resolver.Type)
		}
	}

	// ---- ACL ----
	if sc.ACL != nil {
		if sc.ACL.File != "" && len(sc.ACL.Inline) > 0 {
			errf("acl", "file 与 inline 不能同时使用")
		}
		if sc.ACL.File == "" && len(sc.ACL.Inline) == 0 {
			warnf("acl", "已配置 acl 但既无 file 也无 inline")
		}
	}

	// ---- 出站 ----
	if len(sc.Outbounds) == 0 {
		warnf("outbounds", "未配置出站，官方将使用默认直连")
	}
	seen := map[string]bool{}
	for i, ob := range sc.Outbounds {
		field := fmt.Sprintf("outbounds[%d]", i)
		if ob.Name == "" {
			errf(field+".name", "出站名称不能为空")
		} else if seen[ob.Name] {
			errf(field+".name", "出站名称 %q 重复", ob.Name)
		}
		seen[ob.Name] = true

		switch ob.Type {
		case "direct":
		case "socks5":
			if ob.Socks5 == nil || ob.Socks5.Addr == "" {
				errf(field+".socks5", "必须提供 addr")
			}
		case "http":
			if ob.HTTP == nil || ob.HTTP.URL == "" {
				errf(field+".http", "必须提供 url")
			}
		case "":
			errf(field+".type", "未设置出站类型")
		default:
			errf(field+".type", "不支持的类型 %q（仅 direct / socks5 / http）", ob.Type)
		}
	}

	// ---- Masquerade ----
	if sc.Masquerade != nil && sc.Masquerade.Type != "" {
		switch sc.Masquerade.Type {
		case "file":
			if sc.Masquerade.File == nil || sc.Masquerade.File.Dir == "" {
				errf("masquerade.file", "必须提供 dir")
			}
		case "proxy":
			if sc.Masquerade.Proxy == nil || sc.Masquerade.Proxy.URL == "" {
				errf("masquerade.proxy", "必须提供 url")
			}
		case "string":
			if sc.Masquerade.String == nil || sc.Masquerade.String.Content == "" {
				errf("masquerade.string", "必须提供 content")
			}
		default:
			errf("masquerade.type", "不支持的类型 %q（仅 file / proxy / string）", sc.Masquerade.Type)
		}
	}

	// ---- 流量统计 API（Panel 依赖）----
	if sc.TrafficStats == nil {
		warnf("trafficStats", "未启用 Traffic Stats API，Panel 将无法读取实时流量 / 在线 / 连接")
	} else {
		if sc.TrafficStats.Listen != "" && !ValidListen(sc.TrafficStats.Listen) {
			errf("trafficStats.listen", "监听地址格式不正确：%q。示例：127.0.0.1:9999", sc.TrafficStats.Listen)
		}
		if sc.TrafficStats.Secret == "" {
			warnf("trafficStats.secret", "未设置 secret，任何能访问该地址的人都能读取或踢人（官方强烈建议设置）")
		}
	}

	// ---- ACME 挑战 / 伪装监听地址 ----
	if sc.ACME != nil && sc.ACME.ListenHost != "" && !ValidListen(sc.ACME.ListenHost) {
		errf("acme.listenHost", "监听地址格式不正确：%q。示例：:443", sc.ACME.ListenHost)
	}
	if sc.Masquerade != nil {
		if sc.Masquerade.ListenHTTP != "" && !ValidListen(sc.Masquerade.ListenHTTP) {
			errf("masquerade.listenHTTP", "监听地址格式不正确：%q。示例：:80", sc.Masquerade.ListenHTTP)
		}
		if sc.Masquerade.ListenHTTPS != "" && !ValidListen(sc.Masquerade.ListenHTTPS) {
			errf("masquerade.listenHTTPS", "监听地址格式不正确：%q。示例：:443", sc.Masquerade.ListenHTTPS)
		}
	}

	return issues
}

// listenPattern 匹配「主机 + : + 端口(或端口区间)」的写法。
//
// 主机部分可为空（:443）、IPv4 / 域名、或 [IPv6]。
var listenPattern = regexp.MustCompile(`^(\[[0-9a-fA-F:.]+\]|[0-9a-zA-Z._-]*):[0-9]{1,5}(-[0-9]{1,5})?$`)

// ValidListen 判断监听地址是否为官方可接受的写法。
//
// Realms 模式（realm://…）直接放行；其余必须是「主机? + : + 端口」。
// 这一步能挡住只剩一个 “:” 之类的错误值 —— 那种配置会让官方 Core
// 监听一个随机端口，客户端全部失效。
func ValidListen(s string) bool {
	if strings.HasPrefix(s, "realm://") {
		return true
	}
	return listenPattern.MatchString(s)
}

// HasError 判断校验结果中是否存在阻断级错误。
func HasError(issues []Issue) bool {
	for _, it := range issues {
		if it.Severity == SeverityError {
			return true
		}
	}
	return false
}