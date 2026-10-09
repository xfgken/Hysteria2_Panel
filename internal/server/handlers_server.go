package server
import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/hy2-panel/hy2-panel/internal/config"
	"github.com/hy2-panel/hy2-panel/internal/firewall"
	"github.com/hy2-panel/hy2-panel/internal/manager"
	"gopkg.in/yaml.v3"
)

// handleGetConfig 返回当前官方配置的结构化视图。
//
// 实现说明：配置结构体只带 yaml 标签，因此这里走
// 结构体 → YAML → map → JSON 的转换，保证键名与官方配置完全一致，
// 前端也无需感知两套命名。
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.mgr.Current()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}

	yamlBytes, err := cfg.Marshal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "序列化配置失败: "+err.Error())
		return
	}
	view, err := yamlToJSONMap(yamlBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "转换配置失败: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":       view,
		"unknownKeys":  cfg.UnknownTopLevelKeys(),
		"issues":       config.Validate(&cfg.Server),
		"configPath":   s.mgr.ConfigPath(),
		"serviceUnit":  s.mgr.ServiceUnit(),
	})
}

// handleValidateConfig 仅校验，不落盘。
func (s *Server) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	body, err := readConfigRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	issues := s.mgr.Validate(r.Context(), body)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     !config.HasError(issues),
		"issues": issues,
	})
}

// handlePutConfig / handleApplyConfig 应用 GUI 提交的配置。
func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	s.applyFromRequest(w, r, "gui")
}

func (s *Server) handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	s.applyFromRequest(w, r, "apply")
}

// syncUserpass 把数据库里所有「启用」账号写入即将保存的配置。
//
// 这里**不再自动轮换账号名**：
//   - 名称可能是用户自定义的（例如 hysteria2-family），自动改名会让自定义失效；
//   - 也会迫使通过 URI 导入的客户端反复重新导入。
//
// 需要换名字时用首页的「刷新」按钮（POST /api/users/{id}/regenerate-name）。
//
// 注意：必须写入全部账号 —— 只写一个会把其它账号从官方配置里抹掉。
func (s *Server) syncUserpass(sc *config.ServerConfig) error {
	if sc.Auth == nil || sc.Auth.Type != "userpass" {
		return nil
	}
	up, err := s.db.EnabledUserpass()
	if err != nil {
		return err
	}
	sc.Auth.Userpass = up
	return nil
}

// rotateTokensOnApply 配置生效后自动轮换全部订阅 Token。
//
// 面板约定的行为：**配置一改，订阅地址就换**，因此不再提供界面上的「重置 Token」。
// 轮换失败不影响配置本身已经生效的结果，只记日志。
func (s *Server) rotateTokensOnApply(reason string) int {
	n, err := s.db.RotateAllTokens()
	if err != nil {
		log.Printf("[用户操作] 配置应用后重新生成订阅 Token 失败 reason=%s: %v", reason, err)
		return 0
	}
	log.Printf("[用户操作] 配置应用后已重新生成订阅 Token reason=%s 账号数=%d", reason, n)
	return n
}

// ensureFirewallOpen 把监听端口在服务器本地防火墙上放行。
//
// 面板以 root 身份运行，配置生效时顺手放行，避免用户换了端口之后
// 「连上了却不通」，还得自己上服务器敲防火墙命令。
// 服务器没有本地防火墙（或者拦截发生在云厂商安全组）时安静返回。
func (s *Server) ensureFirewallOpen(listen string) firewall.Result {
	start, end := config.ListenPortRange(listen)
	if start == 0 {
		return firewall.Result{Firewall: "none", Detail: "无法解析监听端口，跳过防火墙放行"}
	}
	res := firewall.EnsureUDP(start, end)
	log.Printf("[防火墙] %s", res.Detail)
	return res
}

// EnsureFirewall 启动时调用：保证当前监听端口在本地防火墙里是放行的。
//
// 面板重启（比如升级二进制）后也顺手检查一遍，避免端口是新配的、
// 防火墙规则却还是旧的。
func (s *Server) EnsureFirewall() firewall.Result {
	return s.ensureFirewallOpenCurrent()
}

// ensureFirewallOpenCurrent 按当前生效的配置放行端口（启动时、改原始配置后调用）。
func (s *Server) ensureFirewallOpenCurrent() firewall.Result {
	cfg, err := s.mgr.Current()
	if err != nil {
		return firewall.Result{Firewall: "none", Detail: "读取配置失败，跳过防火墙放行"}
	}
	return s.ensureFirewallOpen(cfg.Server.Listen)
}

// applyFromRequest 是 GUI 配置应用的公共实现。
//
// 流程：解析 → 同步账号 → 序列化 → 交给 Manager 完成「校验/备份/写入/重启/回滚」→ 轮换订阅 Token。
func (s *Server) applyFromRequest(w http.ResponseWriter, r *http.Request, reason string) {
	body, err := readConfigRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	parsed, err := config.Parse(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "配置解析失败: "+err.Error())
		return
	}

	// 阻断级校验：例如 listen 只剩一个 ":" —— 官方 Core 仍会启动，
	// 但会监听一个随机端口、所有客户端失效。这类配置绝不能写入。
	if issues := config.Validate(&parsed.Server); config.HasError(issues) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "配置存在阻断级错误，未写入",
			"issues": issues,
		})
		return
	}

	if err := s.syncUserpass(&parsed.Server); err != nil {
		writeError(w, http.StatusInternalServerError, "同步账号到配置失败: "+err.Error())
		return
	}

	out, err := parsed.Marshal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "配置序列化失败: "+err.Error())
		return
	}

	if err := s.mgr.Apply(r.Context(), out, reason); err != nil {
		s.writeApplyError(w, err)
		return
	}
	// 应用配置会重启服务，服务状态缓存立即失效
	s.cache.invalidate(cacheKeyServiceActive)

	// 配置已生效 → 自动换一批订阅 Token（面板不再提供「重置 Token」按钮）
	rotated := s.rotateTokensOnApply(reason)

	// 监听端口可能变了 → 顺手在服务器本地防火墙里放行，省得用户换了端口
	// 之后「连上了却不通」还去查半天。
	fw := s.ensureFirewallOpen(parsed.Server.Listen)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"message":       "配置已应用",
		"configPath":    s.mgr.ConfigPath(),
		"rotatedTokens": rotated,
		"firewall":      fw,
	})
}

// handleGetRawConfig 返回原始配置文本（开发文档 51 节）。
func (s *Server) handleGetRawConfig(w http.ResponseWriter, r *http.Request) {
	content, err := s.mgr.CurrentBytes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"content":    string(content),
		"configPath": s.mgr.ConfigPath(),
	})
}

// handlePutRawConfig 应用原始配置文本。
//
// 原始配置同样必须经过官方 Core 校验（开发文档 51 节）。
func (s *Server) handlePutRawConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if err := s.mgr.Apply(r.Context(), []byte(req.Content), "raw"); err != nil {
		s.writeApplyError(w, err)
		return
	}
	s.cache.invalidate(cacheKeyServiceActive)
	// 原始配置同样属于「改了配置」，一样自动换订阅 Token
	s.rotateTokensOnApply("raw")
	// 原始配置里也可能改了监听端口，同样放行
	fw := s.ensureFirewallOpenCurrent()
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"message":  "原始配置已应用",
		"firewall": fw.Detail,
	})
}

// handleSyncUsers 手动触发用户同步。
func (s *Server) handleSyncUsers(w http.ResponseWriter, r *http.Request) {
	if err := s.mgr.SyncUsers(r.Context()); err != nil {
		s.writeApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "用户已同步到官方配置"})
}

// writeApplyError 把应用失败的细节返回给前端（含结构化校验结果）。
func (s *Server) writeApplyError(w http.ResponseWriter, err error) {
	var ve *manager.ValidationError
	if asValidation(err, &ve) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  err.Error(),
			"issues": ve.Issues,
		})
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func asValidation(err error, target **manager.ValidationError) bool {
	if v, ok := err.(*manager.ValidationError); ok {
		*target = v
		return true
	}
	return false
}

// readConfigRequest 从请求体解析配置内容。
//
// 支持两种形式：
//   - {"config": {...}} 结构化 JSON（GUI 表单）
//   - {"content": "..."} 原始 YAML 文本
func readConfigRequest(r *http.Request) ([]byte, error) {
	var raw map[string]any
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("请求体格式错误: %w", err)
	}

	if content, ok := raw["content"].(string); ok {
		return []byte(content), nil
	}
	if cfg, ok := raw["config"]; ok {
		return jsonValueToYAML(cfg)
	}
	return nil, fmt.Errorf("请求体需包含 config 或 content 字段")
}

// ---------------------------------------------------------------------------
// 备份
// ---------------------------------------------------------------------------

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListConfigVersions(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取备份失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "非法的备份 ID")
		return
	}
	if err := s.mgr.Restore(r.Context(), id); err != nil {
		s.writeApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "已恢复该备份"})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "非法的备份 ID")
		return
	}
	if err := s.db.DeleteConfigVersion(id); err != nil {
		writeError(w, http.StatusInternalServerError, "删除备份失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// YAML / JSON 互转
// ---------------------------------------------------------------------------

// yamlToJSONMap 把 YAML 文本转为可直接 JSON 编码的 map。
func yamlToJSONMap(data []byte) (map[string]any, error) {
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// jsonValueToYAML 把 JSON 解码后的值转为官方配置所需的 YAML。
//
// 关键点：JSON 数字默认是 float64，直接交给 yaml 会被写成科学计数法，
// 因此这里用 json.Number 解码并还原成 int64 / float64。
func jsonValueToYAML(v any) ([]byte, error) {
	return yaml.Marshal(normalizeNumbers(v))
}

// normalizeNumbers 递归把 json.Number 还原为 int64 / float64。
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeNumbers(val)
		}
		return t
	default:
		return v
	}
}