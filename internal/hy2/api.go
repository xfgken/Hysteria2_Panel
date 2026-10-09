// Package hy2 封装对官方 Hysteria 2 的网络调用。
//
// 本包只做两件事：
//  1. 调用官方 Traffic Stats API（/traffic、/online、/kick、/dump/streams）；
//  2. 调用官方二进制做配置校验。
//
// 官方文档：https://v2.hysteria.network/docs/advanced/Traffic-Stats-API/
package hy2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是官方 Traffic Stats API 的客户端。
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewClient 依据 trafficStats.listen 与 secret 构造客户端。
// listen 形如 ":9999" 或 "127.0.0.1:9999"；无主机时按本机处理。
func NewClient(listen, secret string) *Client {
	host := listen
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	base := host
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	return &Client{
		baseURL: strings.TrimSuffix(base, "/"),
		secret:  secret,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Traffic 单用户在某一时刻的累计流量。
type Traffic struct {
	Tx int64 `json:"tx"`
	Rx int64 `json:"rx"`
}

// Traffic 调用 GET /traffic，返回 client id -> 累计流量。
// clear 为 true 时对应官方 /traffic?clear=1（返回后清零）。
//
// 注意：官方 Core 会把 userpass 的用户名统一转成**小写**再作为 client id
// （配置里写 hysteria2-AbC，/traffic 里是 hysteria2-abc），因此这里统一把
// key 规范成小写，调用方一律用小写去查，避免大小写不同导致“永远匹配不上”。
func (c *Client) Traffic(clear bool) (map[string]Traffic, error) {
	endpoint := c.baseURL + "/traffic"
	if clear {
		endpoint += "?clear=1"
	}
	var out map[string]Traffic
	if err := c.doJSON(http.MethodGet, endpoint, nil, &out); err != nil {
		return nil, err
	}
	norm := make(map[string]Traffic, len(out))
	for k, v := range out {
		norm[strings.ToLower(k)] = v
	}
	return norm, nil
}

// Online 调用 GET /online，返回 client id -> 连接（设备）数。
//
// key 同样按官方行为规范成小写（见 Traffic 的说明）。
func (c *Client) Online() (map[string]int, error) {
	var out map[string]int
	if err := c.doJSON(http.MethodGet, c.baseURL+"/online", nil, &out); err != nil {
		return nil, err
	}
	norm := make(map[string]int, len(out))
	for k, v := range out {
		norm[strings.ToLower(k)] = v
	}
	return norm, nil
}

// Kick 调用 POST /kick，按 id 列表踢下线。
func (c *Client) Kick(ids []string) error {
	return c.doJSON(http.MethodPost, c.baseURL+"/kick", ids, nil)
}

// Stream 描述一条 QUIC 代理流。
type Stream struct {
	State         string `json:"state"`
	Auth          string `json:"auth"`
	Connection    uint64 `json:"connection"`
	Stream        uint64 `json:"stream"`
	ReqAddr       string `json:"req_addr"`
	HookedReqAddr string `json:"hooked_req_addr"`
	Tx            int64  `json:"tx"`
	Rx            int64  `json:"rx"`
	InitialAt     string `json:"initial_at"`
	LastActiveAt  string `json:"last_active_at"`
}

// DumpStreams 调用 GET /dump/streams，返回当前所有代理流。
func (c *Client) DumpStreams() ([]Stream, error) {
	var wrapper struct {
		Streams []Stream `json:"streams"`
	}
	if err := c.doJSON(http.MethodGet, c.baseURL+"/dump/streams", nil, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Streams, nil
}

// Ping 探测 API 是否可达（用于系统页状态显示）。
func (c *Client) Ping() error {
	return c.doJSON(http.MethodGet, c.baseURL+"/online", nil, nil)
}

// doJSON 发送请求并解析 JSON 响应。
func (c *Client) doJSON(method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("请求体编码失败: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// 官方约定：设置了 secret 时通过 Authorization 头传递。
	if c.secret != "" {
		req.Header.Set("Authorization", c.secret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求官方 API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("官方 API 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析官方 API 响应失败: %w", err)
	}
	return nil
}

// EnsureBaseURL 校验并返回规范化的 base URL（供测试与调试使用）。
func (c *Client) EnsureBaseURL() (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}