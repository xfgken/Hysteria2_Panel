// Package sub 生成订阅相关内容。
//
// 重要（开发文档 19 / 23 / 54 节）：
// 官方 Hysteria 2 本身不是 Clash 订阅服务器，因此这里是 Panel 的扩展功能，
// 属于 Panel Subscription Service，而非 HY2 Core 官方能力。
package sub

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/template"

	"github.com/hy2-panel/hy2-panel/internal/config"
)

// BuildURI 依据当前官方配置生成 Hysteria 2 URI。
//
// 官方格式（https://v2.hysteria.network/docs/developers/URI-Scheme/）：
//
//	hysteria2://[auth@]hostname[:port]/?[key=value]...
//
// 注意：不得硬编码服务器参数，全部从实际配置推导。
func BuildURI(sc *config.ServerConfig, username, password, fallbackHost string) (string, error) {
	host, port := splitListen(sc.Listen)
	if host == "" {
		host = fallbackHost
	}
	if host == "" {
		return "", fmt.Errorf("无法确定服务器地址：listen 未指定主机且未设置服务器域名")
	}

	// 认证部分：password 模式直接用密码；userpass 模式用 用户名:密码。
	//
	// 说明：这里手工拼接而不使用 url.URL.User，因为后者会把
	// userpass 的分隔冒号编码为 %3A，不符合官方 URI 规范。
	var authPart string
	if sc.Auth != nil && sc.Auth.Type == "userpass" {
		authPart = url.PathEscape(username) + ":" + url.PathEscape(password)
	} else {
		authPart = url.PathEscape(password)
	}

	hostPort := host
	if port != "" {
		hostPort = host + ":" + port
	}

	sni, insecure, pin := clientTLS(sc)

	q := url.Values{}
	if sni != "" {
		q.Set("sni", sni)
	}
	// 自签名证书：必须让客户端跳过校验，同时给出证书指纹以防中间人
	//（官方推荐组合：insecure + pinSHA256）。
	if insecure {
		q.Set("insecure", "1")
	}
	if pin != "" {
		q.Set("pinSHA256", pin)
	}

	// 混淆只在服务端确实启用时输出。
	if sc.Obfs != nil && sc.Obfs.Type != "" {
		q.Set("obfs", sc.Obfs.Type)
		switch sc.Obfs.Type {
		case "salamander":
			if sc.Obfs.Salamander != nil && sc.Obfs.Salamander.Password != "" {
				q.Set("obfs-password", sc.Obfs.Salamander.Password)
			}
		case "gecko":
			if sc.Obfs.Gecko != nil && sc.Obfs.Gecko.Password != "" {
				q.Set("obfs-password", sc.Obfs.Gecko.Password)
			}
		}
	}

	var b strings.Builder
	b.WriteString("hysteria2://")
	b.WriteString(authPart)
	b.WriteString("@")
	b.WriteString(hostPort)
	b.WriteString("/")
	if len(q) > 0 {
		b.WriteString("?")
		b.WriteString(q.Encode())
	}
	if username != "" {
		b.WriteString("#")
		b.WriteString(url.PathEscape(username))
	}
	return b.String(), nil
}

// SinglePortURI 把 URI 中“多端口”写法替换为其中第一个端口。
//
// 用途：官方 URI 支持多端口（端口跳跃）写法，例如
//
//	hysteria2://auth@host:20000-50000/?...
//	hysteria2://auth@host:123,5000-6000/?...
//
// 但部分客户端 App 无法解析这种写法。服务端实际上只监听第一个端口，
// 并用防火墙把其余端口重定向过去，因此替换成第一个端口依然可以正常连接，
// 可作为兼容方案。
//
// 不使用正则：Go 的 regexp 属于 RE2，不支持前瞻断言，
// 而且这里需要跳过 userinfo（可能含冒号）与 IPv6 方括号，字符串解析更可靠。
func SinglePortURI(uri string) string {
	schemeIdx := strings.Index(uri, "://")
	if schemeIdx < 0 {
		return uri
	}

	hostStart := schemeIdx + 3
	// 跳过认证部分（可能与密码一起含有冒号）
	if at := strings.LastIndex(uri[hostStart:], "@"); at >= 0 {
		hostStart += at + 1
	}

	// host[:port] 结束于 / ? # 之一
	endOfHost := len(uri)
	if i := strings.IndexAny(uri[hostStart:], "/?#"); i >= 0 {
		endOfHost = hostStart + i
	}

	// 端口前的冒号：取该区间内最后一个（兼容 IPv6 的 [::1]:443）
	portSep := -1
	if i := strings.LastIndex(uri[hostStart:endOfHost], ":"); i >= 0 {
		portSep = hostStart + i
	}
	if portSep < 0 {
		return uri
	}

	portPart := uri[portSep+1 : endOfHost]
	first := portPart
	if i := strings.IndexAny(first, ",-"); i >= 0 {
		first = first[:i]
	}
	if first == portPart || first == "" {
		return uri
	}
	return uri[:portSep+1] + first + uri[endOfHost:]
}

// splitListen 拆分 listen 为主机与端口段。
//
// 支持 ":"、":443"、":20000-50000"、"0.0.0.0:443"、"[::]:443"。
func splitListen(listen string) (host, port string) {
	if listen == "" {
		return "", "443"
	}
	// Realms 模式下 listen 是 realm:// URI，无法直接给出 host:port。
	if strings.HasPrefix(listen, "realm://") {
		return "", ""
	}
	if strings.HasPrefix(listen, ":") {
		return "", strings.TrimPrefix(listen, ":")
	}
	idx := strings.LastIndex(listen, ":")
	if idx < 0 {
		return listen, ""
	}
	h := strings.Trim(listen[:idx], "[]")
	// 通配地址不作为对外地址。
	if h == "0.0.0.0" || h == "::" || h == "" {
		h = ""
	}
	return h, listen[idx+1:]
}

// proxyName 生成 Clash 里的节点名。
//
// 面板的账号名一律形如 hysteria2-xxxx（见 db.UsernamePrefix），
// 所以**不要**再拼一次前缀；只有历史遗留的不带前缀的名字才补上。
func proxyName(username string) string {
	if strings.HasPrefix(username, "hysteria2-") {
		return username
	}
	return "hysteria2-" + username
}

// ClashData 是 Clash Meta 模板的渲染数据。
type ClashData struct {
	MixedPort          int
	AllowLAN           bool
	Mode               string
	LogLevel           string
	ExternalController string

	ProxyName string
	Server    string
	Port      string
	Password  string
	SNI          string
	Insecure     bool
	ObfsType     string
	ObfsPassword string

	Up   string
	Down string
}

// BuildClashData 由官方配置与用户信息构造模板数据。
func BuildClashData(sc *config.ServerConfig, username, password, serverHost string, mixedPort int) (ClashData, error) {
	host, port := splitListen(sc.Listen)
	if host == "" {
		host = serverHost
	}
	if host == "" {
		return ClashData{}, fmt.Errorf("无法确定服务器地址")
	}

	sni, insecure, _ := clientTLS(sc)

	d := ClashData{
		MixedPort: mixedPort,
		AllowLAN:  true,
		Mode:      "rule",
		LogLevel:  "info",
		// 账号名本身已经带 hysteria2- 前缀（db.UsernamePrefix），
		// 这里**不能再拼一次**，否则 Clash 里的节点名会变成
		// hysteria2-hysteria2-xxx。
		ProxyName: proxyName(username),
		Server:    host,
		Port:      port,
		// ⚠ hysteria2 的 userpass 认证要求客户端发送「用户名:密码」整串，
		// 官方 Core 才会拿去和 userpass 表比对。只发密码会直接返回
		// 404 authentication failed，mihomo / Clash 系客户端就表现为
		//「节点能显示但永远测不出延迟」。
		// HY2 链接天生带用户名，所以 NekoBox 能连、Clash 连不上。
		Password: username + ":" + password,
		SNI:       sni,
		Insecure:  insecure,
	}

	if sc.Bandwidth != nil {
		d.Up = sc.Bandwidth.Up
		d.Down = sc.Bandwidth.Down
	}
	if sc.Obfs != nil && sc.Obfs.Type != "" {
		d.ObfsType = sc.Obfs.Type
		switch sc.Obfs.Type {
		case "salamander":
			if sc.Obfs.Salamander != nil {
				d.ObfsPassword = sc.Obfs.Salamander.Password
			}
		case "gecko":
			if sc.Obfs.Gecko != nil {
				d.ObfsPassword = sc.Obfs.Gecko.Password
			}
		}
	}
	return d, nil
}

// RenderClash 使用模板文件渲染 Clash Meta 订阅内容。
//
// 模板与程序代码分离（开发文档 21 节）。
func RenderClash(templatePath string, data ClashData) ([]byte, error) {
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("读取订阅模板失败: %w", err)
	}
	tpl, err := template.New("clash").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("解析订阅模板失败: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("渲染订阅模板失败: %w", err)
	}
	return buf.Bytes(), nil
}