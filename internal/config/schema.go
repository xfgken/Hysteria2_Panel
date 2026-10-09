// Package config 定义并管理官方 Hysteria 2 服务端配置。
//
// 本文件中的结构体严格对齐官方文档 v2.13.0 的 Full Server Config：
// https://v2.hysteria.network/docs/advanced/Full-Server-Config/
//
// 重要约定（见开发文档 2.1 / 2.2 / 2.3 / 70 节）：
//   - 官方 HY2 Core 是唯一网络核心，本包只负责生成 / 校验 config.yaml；
//   - “官方有什么，Panel 就有什么”，字段命名与官方保持一致；
//   - 不使用 yaml:",inline" 吞并未知字段；未知字段由 Config.Raw 原样保留，
//     以便官方新增参数时旧版 Panel 也不会破坏配置（见 51 节 Raw Config）。
package config

// ServerConfig 是 Hysteria 2 服务端配置的完整映射。
type ServerConfig struct {
	// Listen 监听地址，如 ":443"；支持端口跳跃 ":20000-50000"；
	// 亦支持 Realms 的 "realm://token@rendezvous/realm-name"。
	Listen string `yaml:"listen,omitempty"`

	// Realm 为 Realms（P2P / NAT 穿透）模式的调优项。
	Realm *RealmConfig `yaml:"realm,omitempty"`

	// TLS 与 ACME 二选一，不能同时存在。
	TLS  *TLSConfig  `yaml:"tls,omitempty"`
	ACME *ACMEConfig `yaml:"acme,omitempty"`

	// ECH 加密客户端问候。
	ECH *ECHConfig `yaml:"ech,omitempty"`

	// Obfs 混淆（Salamander / Gecko）。
	Obfs *ObfsConfig `yaml:"obfs,omitempty"`

	// QUIC 参数。
	QUIC *QUICConfig `yaml:"quic,omitempty"`

	// Mimic 伪 TCP（仅 Linux，需内核模块）。
	Mimic *MimicConfig `yaml:"mimic,omitempty"`

	// Bandwidth 服务端限速（每客户端）。
	Bandwidth *BandwidthConfig `yaml:"bandwidth,omitempty"`

	// IgnoreClientBandwidth 忽略客户端带宽提示。
	IgnoreClientBandwidth bool `yaml:"ignoreClientBandwidth,omitempty"`

	// Congestion 拥塞控制（方向未走 Brutal 时生效）。
	Congestion *CongestionConfig `yaml:"congestion,omitempty"`

	// SpeedTest 内置测速服务。
	SpeedTest bool `yaml:"speedTest,omitempty"`

	// UDP 相关。
	DisableUDP     bool   `yaml:"disableUDP,omitempty"`
	UDPIdleTimeout string `yaml:"udpIdleTimeout,omitempty"`

	// Auth 认证（password / userpass / http / command）。
	Auth *AuthConfig `yaml:"auth,omitempty"`

	// Resolver 出站 DNS 解析器。
	Resolver *ResolverConfig `yaml:"resolver,omitempty"`

	// Sniff 协议嗅探。
	Sniff *SniffConfig `yaml:"sniff,omitempty"`

	// ACL 访问控制。
	ACL *ACLConfig `yaml:"acl,omitempty"`

	// Outbounds 出站定义；未使用 ACL 时全部流量走第一个（默认）出站。
	Outbounds []OutboundConfig `yaml:"outbounds,omitempty"`

	// TrafficStats HTTP 流量统计 API（Panel 依赖它读取实时数据）。
	TrafficStats *TrafficStatsConfig `yaml:"trafficStats,omitempty"`

	// Masquerade 伪装。
	Masquerade *MasqueradeConfig `yaml:"masquerade,omitempty"`
}

// ---------------------------------------------------------------------------
// Realm（Hysteria Realms）
// ---------------------------------------------------------------------------

// RealmConfig 为 Realms 模式的调优项，全部可选。
type RealmConfig struct {
	STUNServers       []string            `yaml:"stunServers,omitempty"`
	STUNTimeout       string              `yaml:"stunTimeout,omitempty"`
	PunchTimeout      string              `yaml:"punchTimeout,omitempty"`
	HeartbeatInterval string              `yaml:"heartbeatInterval,omitempty"`
	Insecure          bool                `yaml:"insecure,omitempty"`
	IPMode            string              `yaml:"ipMode,omitempty"` // dual | v4 | v6
	PortMapping       *PortMappingConfig  `yaml:"portMapping,omitempty"`
}

// PortMappingConfig 端口映射（UPnP / NAT-PMP）。
type PortMappingConfig struct {
	Enabled  bool   `yaml:"enabled,omitempty"`
	Timeout  string `yaml:"timeout,omitempty"`
	Lifetime string `yaml:"lifetime,omitempty"`
}

// ---------------------------------------------------------------------------
// TLS / ACME
// ---------------------------------------------------------------------------

// TLSConfig 静态证书 TLS。
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`

	// SNIGuard: strict | disable | dns-san（默认 dns-san）。
	SNIGuard string `yaml:"sniGuard,omitempty"`

	// ClientCA 用于 mTLS 校验。
	ClientCA string `yaml:"clientCA,omitempty"`
}

// ACMEConfig 自动证书。
type ACMEConfig struct {
	Domains []string `yaml:"domains"`
	Email   string   `yaml:"email"`

	// CA 可取 letsencrypt（默认）或 zerossl。
	CA string `yaml:"ca,omitempty"`

	// Type: http | tls | dns。
	Type string `yaml:"type"`

	HTTP *ACMEHTTPConfig `yaml:"http,omitempty"`
	TLS  *ACMETLSConfig  `yaml:"tls,omitempty"`
	DNS  *ACMEDNSConfig  `yaml:"dns,omitempty"`

	// ListenHost ACME HTTP/TLS 挑战的绑定地址。
	ListenHost string `yaml:"listenHost,omitempty"`
}

// ACMEHTTPConfig HTTP-01 挑战。
type ACMEHTTPConfig struct {
	AltPort int `yaml:"altPort,omitempty"`
}

// ACMETLSConfig TLS-ALPN-01 挑战。
type ACMETLSConfig struct {
	AltPort int `yaml:"altPort,omitempty"`
}

// ACMEDNSConfig DNS-01 挑战。
// Name 为服务商标识（cloudflare / duckdns / gandi / godaddy ...），
// Config 为服务商专有配置键值（如 cloudflare_api_token）。
type ACMEDNSConfig struct {
	Name   string         `yaml:"name"`
	Config map[string]any `yaml:"config,omitempty"`
}

// ECHConfig 加密客户端问候。
type ECHConfig struct {
	KeyPath string `yaml:"keyPath"`
}

// ---------------------------------------------------------------------------
// 混淆 / QUIC / Mimic
// ---------------------------------------------------------------------------

// ObfsConfig 为“类型选择器”结构（官方文档顶部的 type selector 约定）。
type ObfsConfig struct {
	Type       string            `yaml:"type"` // salamander | gecko
	Salamander *SalamanderConfig `yaml:"salamander,omitempty"`
	Gecko      *GeckoConfig      `yaml:"gecko,omitempty"`
}

// SalamanderConfig Salamander 混淆。
type SalamanderConfig struct {
	Password string `yaml:"password"`
}

// GeckoConfig Gecko 混淆（官方标记为 Experimental）。
type GeckoConfig struct {
	Password      string `yaml:"password"`
	MinPacketSize int    `yaml:"minPacketSize,omitempty"`
	MaxPacketSize int    `yaml:"maxPacketSize,omitempty"`
}

// QUICConfig QUIC 参数。
type QUICConfig struct {
	InitStreamReceiveWindow  int    `yaml:"initStreamReceiveWindow,omitempty"`
	MaxStreamReceiveWindow   int    `yaml:"maxStreamReceiveWindow,omitempty"`
	InitConnReceiveWindow    int    `yaml:"initConnReceiveWindow,omitempty"`
	MaxConnReceiveWindow     int    `yaml:"maxConnReceiveWindow,omitempty"`
	MaxIdleTimeout           string `yaml:"maxIdleTimeout,omitempty"`
	MaxIncomingStreams       int    `yaml:"maxIncomingStreams,omitempty"`
	DisablePathMTUDiscovery  bool   `yaml:"disablePathMTUDiscovery,omitempty"`
	DisableStatelessReset    bool   `yaml:"disableStatelessReset,omitempty"`
}

// MimicConfig 伪 TCP（仅 Linux）。
type MimicConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Interface string   `yaml:"interface,omitempty"`
	XDPMode   string   `yaml:"xdpMode,omitempty"` // native | skb
	Path      string   `yaml:"path,omitempty"`
	ExtraArgs []string `yaml:"extraArgs,omitempty"`
}

// BandwidthConfig 服务端限速。
//
// 注意：Brutal 不是 congestion.type 的取值，而是通过带宽字段触发
// （见官方 Congestion 一节）。开发文档 35 节将其列为拥塞控制类型有误。
type BandwidthConfig struct {
	Up                      string `yaml:"up,omitempty"`
	Down                    string `yaml:"down,omitempty"`
	DisableLossCompensation bool   `yaml:"disableLossCompensation,omitempty"`
}

// CongestionConfig 拥塞控制。仅 bbr / reno。
type CongestionConfig struct {
	Type       string `yaml:"type"`                 // bbr | reno
	BBRProfile string `yaml:"bbrProfile,omitempty"` // standard | conservative | aggressive
}

// ---------------------------------------------------------------------------
// 认证 / 解析器 / 嗅探
// ---------------------------------------------------------------------------

// AuthConfig 为“类型选择器”结构。
type AuthConfig struct {
	Type     string            `yaml:"type"` // password | userpass | http | command
	Password string            `yaml:"password,omitempty"`
	Userpass map[string]string `yaml:"userpass,omitempty"`
	HTTP     *AuthHTTPConfig   `yaml:"http,omitempty"`
	Command  string            `yaml:"command,omitempty"`
}

// AuthHTTPConfig 外部 HTTP 认证后端。
type AuthHTTPConfig struct {
	URL      string `yaml:"url"`
	Insecure bool   `yaml:"insecure,omitempty"`
}

// ResolverConfig 为“类型选择器”结构。
type ResolverConfig struct {
	Type  string               `yaml:"type"` // udp | tcp | tls | https
	UDP   *PlainResolverConfig `yaml:"udp,omitempty"`
	TCP   *PlainResolverConfig `yaml:"tcp,omitempty"`
	TLS   *TLSResolverConfig   `yaml:"tls,omitempty"`
	HTTPS *TLSResolverConfig   `yaml:"https,omitempty"`
}

// PlainResolverConfig UDP / TCP 解析器。
type PlainResolverConfig struct {
	Addr    string `yaml:"addr"`
	Timeout string `yaml:"timeout,omitempty"`
}

// TLSResolverConfig TLS / HTTPS 解析器。
type TLSResolverConfig struct {
	Addr     string `yaml:"addr"`
	Timeout  string `yaml:"timeout,omitempty"`
	SNI      string `yaml:"sni,omitempty"`
	Insecure bool   `yaml:"insecure,omitempty"`
}

// SniffConfig 协议嗅探。支持 HTTP(Host) / TLS(SNI) / QUIC(SNI)。
type SniffConfig struct {
	Enable        bool   `yaml:"enable"`
	Timeout       string `yaml:"timeout,omitempty"`
	RewriteDomain bool   `yaml:"rewriteDomain,omitempty"`
	TCPPorts      string `yaml:"tcpPorts,omitempty"`
	UDPPorts      string `yaml:"udpPorts,omitempty"`
}

// ---------------------------------------------------------------------------
// ACL / 出站
// ---------------------------------------------------------------------------

// ACLConfig ACL。file 与 inline 二选一。
type ACLConfig struct {
	File              string   `yaml:"file,omitempty"`
	Inline            []string `yaml:"inline,omitempty"`
	GeoIP             string   `yaml:"geoip,omitempty"`
	GeoSite           string   `yaml:"geosite,omitempty"`
	GeoUpdateInterval string   `yaml:"geoUpdateInterval,omitempty"`
}

// OutboundConfig 为“类型选择器”结构。支持 direct / socks5 / http。
type OutboundConfig struct {
	Name   string                 `yaml:"name"`
	Type   string                 `yaml:"type"`
	Direct *DirectOutboundConfig  `yaml:"direct,omitempty"`
	Socks5 *Socks5OutboundConfig  `yaml:"socks5,omitempty"`
	HTTP   *HTTPOutboundConfig    `yaml:"http,omitempty"`
}

// DirectOutboundConfig 直连出站。
type DirectOutboundConfig struct {
	Mode       string `yaml:"mode,omitempty"` // auto | 64 | 46 | 6 | 4
	BindIPv4   string `yaml:"bindIPv4,omitempty"`
	BindIPv6   string `yaml:"bindIPv6,omitempty"`
	BindDevice string `yaml:"bindDevice,omitempty"`
	FastOpen   bool   `yaml:"fastOpen,omitempty"`
}

// Socks5OutboundConfig SOCKS5 出站。
type Socks5OutboundConfig struct {
	Addr     string `yaml:"addr"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// HTTPOutboundConfig HTTP / HTTPS 出站（不支持 UDP）。
type HTTPOutboundConfig struct {
	URL      string `yaml:"url"`
	Insecure bool   `yaml:"insecure,omitempty"`
}

// ---------------------------------------------------------------------------
// Traffic Stats API / Masquerade
// ---------------------------------------------------------------------------

// TrafficStatsConfig 官方 HTTP 流量统计 API。
type TrafficStatsConfig struct {
	Listen string `yaml:"listen"`
	Secret string `yaml:"secret,omitempty"`
}

// MasqueradeConfig 伪装（类型选择器 + HTTP/HTTPS 伪装端口）。
type MasqueradeConfig struct {
	Type  string                  `yaml:"type,omitempty"` // file | proxy | string
	File  *MasqueradeFileConfig   `yaml:"file,omitempty"`
	Proxy *MasqueradeProxyConfig  `yaml:"proxy,omitempty"`
	String *MasqueradeStringConfig `yaml:"string,omitempty"`

	ListenHTTP  string `yaml:"listenHTTP,omitempty"`
	ListenHTTPS string `yaml:"listenHTTPS,omitempty"`
	ForceHTTPS  bool   `yaml:"forceHTTPS,omitempty"`
}

// MasqueradeFileConfig 静态文件伪装。
type MasqueradeFileConfig struct {
	Dir string `yaml:"dir"`
}

// MasqueradeProxyConfig 反向代理伪装。
type MasqueradeProxyConfig struct {
	URL         string `yaml:"url"`
	RewriteHost bool   `yaml:"rewriteHost,omitempty"`
	Insecure    bool   `yaml:"insecure,omitempty"`
	XForwarded  bool   `yaml:"xForwarded,omitempty"`
}

// MasqueradeStringConfig 固定字符串伪装。
type MasqueradeStringConfig struct {
	Content    string            `yaml:"content"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	StatusCode int               `yaml:"statusCode,omitempty"`
}
