package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// 设置项键名。
const (
	settingServerHost  = "server_host"      // 对外服务器地址（用于生成 URI / 订阅）
	settingSubBaseURL  = "sub_base_url"     // 订阅基础地址，如 https://example.com
	settingMixedPort   = "clash_mixed_port" // Clash 模板中的 mixed-port
	settingLogHysteria = "log_hysteria"
	settingLogPanel    = "log_panel"
	settingLogNginx    = "log_nginx"
)

// 默认日志来源。
const (
	defaultLogHysteria = "journal:hysteria-server"
	defaultLogNginx    = "/var/log/nginx/error.log"
)

// setting 读取字符串设置。
func (s *Server) setting(key, def string) string {
	v, err := s.db.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	return v
}

// intSetting 读取整型设置。
func (s *Server) intSetting(key string, def int) int {
	v, err := s.db.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// serverHost 返回对外服务器地址。
func (s *Server) serverHost(r *http.Request) string {
	if h := s.setting(settingServerHost, ""); h != "" {
		return h
	}
	// 回退到请求 Host（去掉端口）。
	host := r.Host
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}
	return host
}

// subBaseURL 返回订阅基础地址。
func (s *Server) subBaseURL(r *http.Request) string {
	if u := s.setting(settingSubBaseURL, ""); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

// logPath 返回指定日志源的配置值。
func (s *Server) logPath(source string) string {
	switch source {
	case "hysteria":
		return s.setting(settingLogHysteria, defaultLogHysteria)
	case "nginx":
		return s.setting(settingLogNginx, defaultLogNginx)
	default:
		return s.setting(settingLogPanel, s.dataDir+"/panel.log")
	}
}

// settingsResponse 是 GET /api/settings 的响应体。
type settingsResponse struct {
	ServerHost  string `json:"serverHost"`
	SubBaseURL  string `json:"subBaseURL"`
	MixedPort   int    `json:"mixedPort"`
	LogHysteria string `json:"logHysteria"`
	LogPanel    string `json:"logPanel"`
	LogNginx    string `json:"logNginx"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, settingsResponse{
		ServerHost:  s.setting(settingServerHost, ""),
		SubBaseURL:  s.setting(settingSubBaseURL, ""),
		MixedPort:   s.intSetting(settingMixedPort, 7890),
		LogHysteria: s.setting(settingLogHysteria, defaultLogHysteria),
		LogPanel:    s.setting(settingLogPanel, s.dataDir+"/panel.log"),
		LogNginx:    s.setting(settingLogNginx, defaultLogNginx),
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}

	pairs := map[string]string{
		settingServerHost:  req.ServerHost,
		settingSubBaseURL:  req.SubBaseURL,
		settingMixedPort:   strconv.Itoa(req.MixedPort),
		settingLogHysteria: req.LogHysteria,
		settingLogPanel:    req.LogPanel,
		settingLogNginx:    req.LogNginx,
	}
	for k, v := range pairs {
		if err := s.db.SetSetting(k, v); err != nil {
			writeError(w, http.StatusInternalServerError, "保存设置失败: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}