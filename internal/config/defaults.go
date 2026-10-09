package config

// Defaults 返回一份与官方默认值一致的基础配置。
//
// 数值来源：官方 Full Server Config（v2.13.0）。
// 认证默认使用 userpass，便于 Panel 做用户管理（见开发文档 14 节）。
func Defaults() ServerConfig {
	return ServerConfig{
		Listen: ":443",
		QUIC: &QUICConfig{
			InitStreamReceiveWindow: 8 * 1024 * 1024,
			MaxStreamReceiveWindow:  8 * 1024 * 1024,
			InitConnReceiveWindow:   20 * 1024 * 1024,
			MaxConnReceiveWindow:    20 * 1024 * 1024,
			MaxIdleTimeout:          "30s",
			MaxIncomingStreams:      1024,
		},
		Congestion: &CongestionConfig{
			Type:       "bbr",
			BBRProfile: "standard",
		},
		UDPIdleTimeout: "60s",
		Auth: &AuthConfig{
			Type:     "userpass",
			Userpass: map[string]string{},
		},
		// Panel 需要读取实时流量 / 在线 / streams，默认开启并仅监听本机。
		TrafficStats: &TrafficStatsConfig{
			Listen: "127.0.0.1:9999",
		},
		Outbounds: []OutboundConfig{
			{Name: "default", Type: "direct"},
		},
	}
}