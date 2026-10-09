// Package updater 负责更新官方 Hysteria 2 核心二进制，并在失败时回滚。
//
// 设计要点：
//   - Panel 只更新「官方 Core」，不自我更新（避免运行中替换自身）；
//   - 下载后先执行 `hysteria version` 验证可用，再原子替换；
//   - 替换前保留上一版本为 <binary>.bak，服务启动失败时自动回滚。
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/hy2"
)

// ReleaseURL 是官方发布信息接口。
const ReleaseURL = "https://api.github.com/repos/apernet/hysteria/releases/latest"

// CheckResult 是版本检查结果。
type CheckResult struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	UpToDate    bool   `json:"upToDate"`
	AssetName   string `json:"assetName"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// UpdateResult 是更新结果。
type UpdateResult struct {
	From        string `json:"from"`
	To          string `json:"to"`
	BackupPath  string `json:"backupPath"`
	Restarted   bool   `json:"restarted"`
	DownloadURL string `json:"downloadUrl"`
}

// Updater 管理官方 Core 的更新。
type Updater struct {
	binaryPath  string
	serviceUnit string
	client      *http.Client
}

// New 构造 Updater。
func New(binaryPath, serviceUnit string) *Updater {
	return &Updater{
		binaryPath:  binaryPath,
		serviceUnit: serviceUnit,
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// BackupPath 返回上一版本二进制的保留路径。
func (u *Updater) BackupPath() string { return u.binaryPath + ".bak" }

// release 是 GitHub Release 的裁剪结构。
type release struct {
	TagName     string  `json:"tag_name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Check 查询官方最新版本并与当前版本比较。
func (u *Updater) Check(ctx context.Context) (*CheckResult, error) {
	rel, err := u.latestRelease(ctx)
	if err != nil {
		return nil, err
	}

	res := &CheckResult{
		Latest:      cleanTag(rel.TagName),
		PublishedAt: rel.PublishedAt,
		Notes:       truncate(rel.Body, 2000),
	}
	if v, err := hy2.BinaryVersion(u.binaryPath); err == nil {
		res.Current = v
		res.UpToDate = normalizeVersion(v) == normalizeVersion(rel.TagName)
	}

	a, err := pickAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return res, err
	}
	res.AssetName = a.Name
	res.DownloadURL = a.URL
	return res, nil
}

// Update 下载并安装最新官方 Core。
func (u *Updater) Update(ctx context.Context) (*UpdateResult, error) {
	rel, err := u.latestRelease(ctx)
	if err != nil {
		return nil, err
	}
	a, err := pickAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}

	current := ""
	if v, verr := hy2.BinaryVersion(u.binaryPath); verr == nil {
		current = v
	}

	tmpDir := filepath.Dir(u.binaryPath)
	// 下载到同目录，保证后面可以用 rename 原子替换（跨设备 rename 会失败）。
	tmp, err := os.CreateTemp(tmpDir, ".hysteria-download-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := u.download(ctx, a.URL, tmp); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("写入下载文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return nil, fmt.Errorf("设置可执行权限失败: %w", err)
	}

	// 验证新二进制可运行。
	newVersion, err := hy2.BinaryVersion(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("下载的二进制无法运行，已放弃更新: %w", err)
	}

	// 备份当前版本。
	backupPath := u.BackupPath()
	if _, err := os.Stat(u.binaryPath); err == nil {
		if err := copyFile(u.binaryPath, backupPath, 0o755); err != nil {
			return nil, fmt.Errorf("备份当前二进制失败: %w", err)
		}
	}

	// 原子替换。
	if err := os.Rename(tmpPath, u.binaryPath); err != nil {
		return nil, fmt.Errorf("替换二进制失败: %w", err)
	}

	res := &UpdateResult{
		From:        current,
		To:          newVersion,
		BackupPath:  backupPath,
		DownloadURL: a.URL,
	}

	// 重启并健康检查；失败则回滚。
	if hy2.HasSystemd() {
		if _, err := hy2.ServiceAction("restart", u.serviceUnit); err != nil {
			_ = u.Rollback(ctx)
			return nil, fmt.Errorf("重启服务失败，已回滚到上一版本: %w", err)
		}
		if !hy2.WaitActive(u.serviceUnit, 15*time.Second) {
			_ = u.Rollback(ctx)
			return nil, errors.New("新版本启动失败，已回滚到上一版本")
		}
		res.Restarted = true
	}
	return res, nil
}

// Rollback 恢复上一版本二进制并重启服务。
func (u *Updater) Rollback(ctx context.Context) error {
	backupPath := u.BackupPath()
	if _, err := os.Stat(backupPath); err != nil {
		return errors.New("没有可用的上一版本备份")
	}
	if err := copyFile(backupPath, u.binaryPath, 0o755); err != nil {
		return fmt.Errorf("恢复上一版本失败: %w", err)
	}
	if hy2.HasSystemd() {
		if _, err := hy2.ServiceAction("restart", u.serviceUnit); err != nil {
			return fmt.Errorf("回滚后重启服务失败: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

func (u *Updater) latestRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "hy2-panel")

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询官方最新版本失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询官方最新版本失败：HTTP %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析发布信息失败: %w", err)
	}
	if rel.TagName == "" {
		return nil, errors.New("发布信息缺少版本号")
	}
	return &rel, nil
}

func (u *Updater) download(ctx context.Context, url string, dst *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "hy2-panel")

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("下载官方 Core 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载官方 Core 失败：HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("写入官方 Core 失败: %w", err)
	}
	return nil
}

// pickAsset 按平台选择发布资产。
//
// 官方命名形如 hysteria-linux-amd64、hysteria-linux-arm64。
func pickAsset(assets []asset, goos, goarch string) (*asset, error) {
	want := fmt.Sprintf("%s-%s", goos, goarch)
	for i := range assets {
		name := strings.ToLower(assets[i].Name)
		if strings.Contains(name, want) && !strings.HasSuffix(name, ".deb") &&
			!strings.HasSuffix(name, ".rpm") && !strings.HasSuffix(name, ".apk") {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("官方发布中没有匹配 %s 的可执行文件", want)
}

// normalizeVersion 把版本号统一成可比较的形式。
//
// 官方仓库使用 monorepo 风格的 tag（如 "app/v2.13.0"），
// 因此必须先去掉路径前缀，再去掉 "v" 前缀，
// 否则 "v2.13.0" 与 "app/v2.13.0" 会被判定为不同版本，
// 导致「永远提示有新版本」。
func normalizeVersion(v string) string {
	return strings.TrimPrefix(cleanTag(v), "v")
}

// cleanTag 去掉 tag 的路径前缀，保留 "v" 前缀，便于界面展示。
//
// 例："app/v2.13.0" → "v2.13.0"；"v2.13.0" → "v2.13.0"。
func cleanTag(v string) string {
	v = strings.TrimSpace(v)
	if idx := strings.LastIndex(v, "/"); idx >= 0 {
		v = v[idx+1:]
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// copyFile 复制文件并设置权限。
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Version 读取指定二进制的版本（用于测试与展示）。
func Version(binary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return hy2.BinaryVersionContext(ctx, binary)
}
