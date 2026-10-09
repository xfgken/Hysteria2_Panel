// Package manager 编排 Panel 对官方 Hysteria 2 的配置生命周期。
//
// 对应开发文档 49 / 50 / 51 节：编辑 → 校验 → 备份 → 应用 → 重启 → 健康检查 → 失败回滚。
package manager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/config"
	"github.com/hy2-panel/hy2-panel/internal/db"
	"github.com/hy2-panel/hy2-panel/internal/hy2"
)

// Options 构造 Manager 所需的路径与依赖。
type Options struct {
	DB           *db.DB
	ConfigPath   string // 官方 HY2 正式配置路径
	Binary       string // 官方 HY2 二进制
	BackupDir    string // 备份目录
	ServiceUnit  string // systemd 单元名，默认 hysteria-server
	PanelVersion string // Panel 版本号（写入备份元信息）
}

// Manager 负责配置文件与服务的编排。
type Manager struct {
	db           *db.DB
	configPath   string
	binary       string
	backupDir    string
	serviceUnit  string
	panelVersion string
}

// New 构造 Manager。
func New(o Options) *Manager {
	unit := o.ServiceUnit
	if unit == "" {
		unit = "hysteria-server"
	}
	return &Manager{
		db:           o.DB,
		configPath:   o.ConfigPath,
		binary:       o.Binary,
		backupDir:    o.BackupDir,
		serviceUnit:  unit,
		panelVersion: o.PanelVersion,
	}
}

// ServiceUnit 返回受管理的 systemd 单元名。
func (m *Manager) ServiceUnit() string { return m.serviceUnit }

// ConfigPath 返回正式配置路径。
func (m *Manager) ConfigPath() string { return m.configPath }

// Binary 返回官方二进制路径。
func (m *Manager) Binary() string { return m.binary }

// BackupDir 返回备份目录。
func (m *Manager) BackupDir() string { return m.backupDir }

// Current 读取当前正式配置。
func (m *Manager) Current() (*config.Config, error) {
	return config.Load(m.configPath)
}

// CurrentBytes 读取当前正式配置的原始文本。
func (m *Manager) CurrentBytes() ([]byte, error) { return os.ReadFile(m.configPath) }

// Validate 做静态校验 + 官方二进制校验。
func (m *Manager) Validate(ctx context.Context, cfgBytes []byte) []config.Issue {
	var issues []config.Issue

	cfg, err := config.Parse(cfgBytes)
	if err != nil {
		return []config.Issue{{Severity: config.SeverityError, Field: "yaml", Message: err.Error()}}
	}
	issues = append(issues, config.Validate(&cfg.Server)...)

	if config.HasError(issues) {
		return issues
	}

	// 官方 Core 校验（二进制存在时才做）
	if _, err := os.Stat(m.binary); err == nil {
		if err := hy2.ValidateConfig(ctx, m.binary, cfgBytes); err != nil {
			issues = append(issues, config.Issue{
				Severity: config.SeverityError,
				Field:    "core",
				Message:  err.Error(),
			})
		}
	} else {
		issues = append(issues, config.Issue{
			Severity: config.SeverityWarning,
			Field:    "core",
			Message:  "未找到官方 Hysteria 二进制，已跳过硬校验：" + m.binary,
		})
	}
	return issues
}

// Apply 执行完整的应用流程。
//
// 步骤：写入临时文件 → 校验 → 备份 → 原子替换 → 重启 → 健康检查 →（失败则回滚）。
func (m *Manager) Apply(ctx context.Context, cfgBytes []byte, reason string) error {
	tmpPath := m.configPath + ".tmp"

	if err := os.MkdirAll(filepath.Dir(m.configPath), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := os.WriteFile(tmpPath, cfgBytes, 0o600); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	// 1) 校验：失败则不动正式配置。
	issues := m.Validate(ctx, cfgBytes)
	if config.HasError(issues) {
		return &ValidationError{Issues: issues}
	}

	// 2) 备份旧配置（数据库 + 文件双写）。
	if old, err := m.CurrentBytes(); err == nil && len(old) > 0 {
		if err := m.backup(old, reason); err != nil {
			return fmt.Errorf("备份当前配置失败: %w", err)
		}
	}

	// 3) 原子替换。
	if err := os.Rename(tmpPath, m.configPath); err != nil {
		return fmt.Errorf("应用配置失败: %w", err)
	}

	// 4) 重启并等待。
	if !hy2.HasSystemd() {
		// 容器 / 非 systemd 环境下无法自动重启：配置已落盘，由使用者手动重启。
		log.Printf("当前系统未使用 systemd，已写入配置但未自动重启服务")
		return nil
	}
	if err := m.Restart(); err != nil {
		return err
	}
	if !hy2.WaitActive(m.serviceUnit, 10*time.Second) {
		// 5) 启动失败：自动回滚。
		if rbErr := m.rollback(); rbErr != nil {
			return fmt.Errorf("服务未能启动，且自动回滚失败: %v", rbErr)
		}
		return errors.New("服务未能启动，已自动回滚到上一份配置")
	}
	return nil
}

// backup 备份配置到数据库与文件系统。
func (m *Manager) backup(content []byte, reason string) error {
	hy2Version := ""
	if v, err := hy2.BinaryVersion(m.binary); err == nil {
		hy2Version = v
	}
	if _, err := m.db.SaveConfigVersion(string(content), hy2Version, m.panelVersion, reason); err != nil {
		return err
	}

	if m.backupDir != "" {
		if err := os.MkdirAll(m.backupDir, 0o700); err != nil {
			return err
		}
		name := fmt.Sprintf("config-%s.yaml", time.Now().Format("20060102-150405"))
		if err := os.WriteFile(filepath.Join(m.backupDir, name), content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// rollback 回滚到最近一次备份。
func (m *Manager) rollback() error {
	v, err := m.db.LatestConfigVersion()
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configPath, []byte(v.Content), 0o600); err != nil {
		return err
	}
	return m.Restart()
}

// Restore 恢复指定备份。
func (m *Manager) Restore(ctx context.Context, versionID int64) error {
	v, err := m.db.GetConfigVersion(versionID)
	if err != nil {
		return fmt.Errorf("备份不存在: %w", err)
	}
	return m.Apply(ctx, []byte(v.Content), "restore")
}

// SyncUsers 把数据库中所有「启用」账号同步进配置的 userpass 认证。
//
// 对应开发文档 14 节：用户管理必须映射到官方 HY2 Authentication。
// 官方 userpass 是一张表，面板里的每个账号各占一行，因此这里全量写入 ——
// 只写第一个会导致其余账号连不上。
func (m *Manager) SyncUsers(ctx context.Context) error {
	cfg, err := m.Current()
	if err != nil {
		return err
	}
	if cfg.Server.Auth == nil || cfg.Server.Auth.Type != "userpass" {
		return errors.New("当前认证方式不是 userpass，无法同步面板用户")
	}
	userpass, err := m.db.EnabledUserpass()
	if err != nil {
		return err
	}
	cfg.Server.Auth.Userpass = userpass
	out, err := cfg.Marshal()
	if err != nil {
		return err
	}
	return m.Apply(ctx, out, "sync-users")
}

// Restart 重启官方服务。
func (m *Manager) Restart() error {
	if !hy2.HasSystemd() {
		return errors.New("当前系统未使用 systemd，请在系统页手动重启服务")
	}
	_, err := hy2.ServiceAction("restart", m.serviceUnit)
	return err
}

// Valid 检查配置内容是否可被接受（供恢复前预检）。
func (m *Manager) Valid(ctx context.Context, content string) bool {
	return !config.HasError(m.Validate(ctx, []byte(content)))
}

// ValidationError 携带结构化校验结果。
type ValidationError struct {
	Issues []config.Issue
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("配置校验未通过")
	for _, it := range e.Issues {
		if it.Severity == config.SeverityError {
			b.WriteString("; ")
			b.WriteString(it.Field)
			b.WriteString(": ")
			b.WriteString(it.Message)
		}
	}
	return b.String()
}