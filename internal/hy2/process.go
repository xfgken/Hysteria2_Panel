package hy2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// 官方 Core 的行为常量。
//
// 依据源码（app/cmd/root.go、app/cmd/server.go）：
//   - server 命令没有 --check / --dry-run；
//   - 配置错误会以 logger.Fatal 输出上述文案并退出；
//   - 成功加载并开始监听后会输出 “server up and running”。
const (
	markerServerUp = "server up and running"
)

var fatalMarkers = []string{
	"failed to read server config",
	"failed to parse server config",
	"failed to load server config",
	"failed to initialize server",
	"failed to start mimic",
	"failed to serve",
}

// syncBuffer 是并发安全的输出缓冲（进程输出与轮询读取并发）。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// ValidateConfig 使用官方二进制校验配置是否可加载。
//
// 设计说明（开发文档 49 节要求“官方 HY2 配置验证”）：
// 官方未提供独立的校验子命令，因此这里采用「净化副本 + 短时启动 + 观测日志」：
//   - listen 与 trafficStats.listen 改写为 127.0.0.1:0（随机端口），避免占用生产端口；
//   - 移除 masquerade 的 listenHTTP / listenHTTPS，避免抢占 80/443；
//   - 强制 mimic.enabled=false，避免依赖内核模块；
//   - 若配置了 acme，则用临时自签名证书替换该块（见下）；
//   - 一旦观测到 “server up and running” 即判定合法并立即结束进程。
//
// 关于 ACME：官方 Core 在**启动阶段**就会真实申请证书（并非握手时懒加载），
// 因此直接校验 acme 配置会对 CA 发起真实申请，反复保存配置可能触发
// Let's Encrypt 的速率限制。故校验时临时换成自签名证书，
// acme 各字段本身的合法性由静态校验负责。
func ValidateConfig(ctx context.Context, binary string, cfg []byte) error {
	sanitized, cleanup, err := sanitizeForValidation(binary, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	tmp, err := os.CreateTemp("", "hy2-validate-*.yaml")
	if err != nil {
		return fmt.Errorf("创建临时配置文件失败: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(sanitized); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时配置失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary,
		"server", "-c", tmp.Name(),
		"--disable-update-check",
		"-l", "info",
	)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动官方 Core: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			if strings.Contains(out.String(), markerServerUp) {
				return nil
			}
			return fmt.Errorf("%s", explainFailure(out.String()))
		case <-ticker.C:
			if strings.Contains(out.String(), markerServerUp) {
				// 配置可用，立即结束临时进程。
				_ = cmd.Process.Kill()
				<-done
				return nil
			}
		case <-ctx.Done():
			if strings.Contains(out.String(), markerServerUp) {
				return nil
			}
			return fmt.Errorf("配置校验超时，未能观察到服务启动标志：%s", tail(out.String()))
		}
	}
}

// sanitizeForValidation 生成用于校验的安全副本。
//
// 返回净化后的 YAML、清理临时文件的函数，以及错误。
func sanitizeForValidation(binary string, cfg []byte) ([]byte, func(), error) {
	noop := func() {}

	var root map[string]any
	if err := yaml.Unmarshal(cfg, &root); err != nil {
		return nil, noop, fmt.Errorf("YAML 语法错误: %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}

	root["listen"] = "127.0.0.1:0"

	if ts, ok := root["trafficStats"].(map[string]any); ok {
		ts["listen"] = "127.0.0.1:0"
	}
	if m, ok := root["mimic"].(map[string]any); ok {
		m["enabled"] = false
	}
	if m, ok := root["masquerade"].(map[string]any); ok {
		delete(m, "listenHTTP")
		delete(m, "listenHTTPS")
		delete(m, "forceHTTPS")
	}

	cleanup := noop

	// ACME 会在启动阶段真实申请证书：校验时换成临时自签名证书，
	// 避免对 CA 发起真实申请而触发限流。
	// 若临时证书生成失败，则保留原 acme 块（宁可如实校验，也不静默改变语义）。
	if _, hasACME := root["acme"]; hasACME {
		cert, key, cl, err := tempSelfSignedCert(binary)
		if err == nil {
			delete(root, "acme")
			root["tls"] = map[string]any{"cert": cert, "key": key}
			cleanup = cl
		}
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, noop, err
	}
	return out, cleanup, nil
}

// tempSelfSignedCert 借用官方二进制生成一份临时自签名证书。
func tempSelfSignedCert(binary string) (cert, key string, cleanup func(), err error) {
	noop := func() {}

	dir, err := os.MkdirTemp("", "hy2-validate-cert-*")
	if err != nil {
		return "", "", noop, err
	}

	cert = filepath.Join(dir, "server.crt")
	key = filepath.Join(dir, "server.key")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, cerr := exec.CommandContext(ctx, binary,
		"cert",
		"--cert", cert,
		"--key", key,
		"--host", "localhost",
		"--overwrite",
	).CombinedOutput()
	if cerr != nil {
		_ = os.RemoveAll(dir)
		return "", "", noop, fmt.Errorf("%v: %s", cerr, strings.TrimSpace(string(out)))
	}
	return cert, key, func() { _ = os.RemoveAll(dir) }, nil
}

// explainFailure 从输出中提取可读的失败原因。
func explainFailure(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		for _, m := range fatalMarkers {
			if strings.Contains(line, m) {
				return strings.TrimSpace(line)
			}
		}
	}
	return "官方 Core 拒绝了该配置：" + tail(output)
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[len(s)-500:]
	}
	return s
}

// ---------------------------------------------------------------------------
// 服务管理
// ---------------------------------------------------------------------------

// systemctlPath 返回可用的 systemctl 路径；不存在则返回空串。
func systemctlPath() string {
	p, err := exec.LookPath("systemctl")
	if err != nil {
		return ""
	}
	return p
}

// HasSystemd 报告当前系统是否真的由 systemd 托管。
//
// 注意：仅检测 systemctl 是否存在是不够的——在容器或 proot 环境中
// systemctl 往往存在但 systemd 并非 PID 1，此时所有操作都会失败。
func HasSystemd() bool {
	if systemctlPath() == "" {
		return false
	}
	// 首选：systemd 运行时目录
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	// 回退：检查 1 号进程
	if b, err := os.ReadFile("/proc/1/comm"); err == nil {
		return strings.TrimSpace(string(b)) == "systemd"
	}
	return false
}

// ServiceAction 对 systemd 服务执行动作（start / stop / restart / status）。
func ServiceAction(action, unit string) (string, error) {
	sc := systemctlPath()
	if sc == "" {
		return "", fmt.Errorf("当前系统未使用 systemd，无法通过 Panel 管理服务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, sc, action, unit).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && action != "status" {
		return text, fmt.Errorf("systemctl %s %s 失败: %v: %s", action, unit, err, text)
	}
	return text, nil
}

// ServiceActive 判断服务是否处于 active 状态。
//
// 当 systemctl 本身执行失败时（例如 systemd 未作为 PID 1 运行）会返回错误，
// 以便调用方区分“服务已停止”与“无法查询状态”。
func ServiceActive(unit string) (bool, error) {
	sc := systemctlPath()
	if sc == "" {
		return false, fmt.Errorf("当前系统未使用 systemd")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, sc, "is-active", unit).Output()
	state := strings.TrimSpace(string(out))
	if err != nil && state == "" {
		return false, fmt.Errorf("查询服务状态失败: %w", err)
	}
	return state == "active", nil
}

// WaitActive 在超时时间内等待服务变为 active。
func WaitActive(unit string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok, _ := ServiceActive(unit); ok {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// BinaryVersion 读取官方 Core 版本。
func BinaryVersion(binary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return BinaryVersionContext(ctx, binary)
}

// BinaryVersionContext 读取指定二进制的版本。
//
// 注意：`hysteria version` 输出的是带 ASCII logo 的多行文本，形如：
//
//	（空行）
//	░█░█░█░█░█▀▀░▀█▀...
//	a powerful, lightning fast and censorship resistant proxy
//	...
//	Version:	v2.13.0
//	BuildDate:	...
//
// 因此不能简单地取第一行，必须按 `Version:` 键解析，并忽略 logo 行。
func BinaryVersionContext(ctx context.Context, binary string) (string, error) {
	out, err := exec.CommandContext(ctx, binary, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取官方 Core 版本失败: %v", err)
	}
	v := parseVersionOutput(string(out))
	if v == "" {
		return "", errors.New("无法从版本输出中解析出版本号")
	}
	return v, nil
}

// parseVersionOutput 从 `hysteria version` 的输出中提取版本号。
func parseVersionOutput(output string) string {
	lines := strings.Split(output, "\n")

	// 首选：按 "Version:" 键匹配
	for _, line := range lines {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:idx]), "version") {
			if v := strings.TrimSpace(line[idx+1:]); v != "" {
				return v
			}
		}
	}

	// 回退：取第一个形如版本号（以 v 开头且含数字）的非空行
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 1 && (line[0] == 'v' || line[0] == 'V') && strings.ContainsAny(line, "0123456789") {
			return line
		}
	}
	return ""
}