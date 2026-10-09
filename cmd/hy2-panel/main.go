// 命令 hy2-panel 是 HY2 Panel 的主程序。
//
// 它启动 Web 管理面板，并在首次运行时引导管理员账号。
// 注意：Panel 本身不是代理核心；真正的网络服务由官方 Hysteria 2 提供。
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/auth"
	"github.com/hy2-panel/hy2-panel/internal/db"
	"github.com/hy2-panel/hy2-panel/internal/hy2"
	"github.com/hy2-panel/hy2-panel/internal/manager"
	"github.com/hy2-panel/hy2-panel/internal/server"
	"github.com/hy2-panel/hy2-panel/internal/updater"
)

func main() {
	// 先处理 CLI 子命令；非子命令时继续走服务启动流程。
	if len(os.Args) > 1 {
		if handled, err := runCLI(os.Args[1:]); handled {
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				os.Exit(1)
			}
			return
		}
	}

	var (
		listen      = flag.String("listen", ":8080", "Panel Web 监听地址")
		dataDir     = flag.String("data-dir", "/opt/hy2-panel/data", "数据目录（SQLite / 日志）")
		hy2Config   = flag.String("hy2-config", "/opt/hy2-panel/config/hysteria.yaml", "官方 Hysteria 2 配置文件路径")
		hy2Binary   = flag.String("hy2-bin", "/usr/local/bin/hysteria", "官方 Hysteria 2 二进制路径")
		backupDir   = flag.String("backup-dir", "/opt/hy2-panel/backup", "配置备份目录")
		serviceUnit = flag.String("service-unit", "hysteria-server", "官方服务使用的 systemd 单元名")
		webDir      = flag.String("web-dir", "", "前端静态资源目录（默认自动探测）")
		adminUser   = flag.String("admin-user", "admin", "首次启动时创建的管理员用户名")
		adminPass   = flag.String("admin-password", "", "首次启动时创建的管理员密码（留空则随机生成，至少 8 位）")
		initialUser = flag.String("initial-user", "user1", "首次启动时自动创建的 HY2 用户名")
		initialPass = flag.String("initial-user-password", "", "首次启动时 HY2 初始用户的密码（留空则随机生成）")
	)
	flag.Parse()

	opts := Options{
		Listen:              *listen,
		DataDir:             *dataDir,
		ConfigPath:          *hy2Config,
		Binary:              *hy2Binary,
		BackupDir:           *backupDir,
		ServiceUnit:         *serviceUnit,
		WebDir:              *webDir,
		AdminUser:           *adminUser,
		AdminPassword:       *adminPass,
		InitialUser:         *initialUser,
		InitialUserPassword: *initialPass,
	}
	if err := run(opts); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

// Options 是主程序的运行参数。
type Options struct {
	Listen              string
	DataDir             string
	ConfigPath          string
	Binary              string
	BackupDir           string
	ServiceUnit         string
	WebDir              string
	AdminUser           string
	AdminPassword       string
	InitialUser         string
	InitialUserPassword string
}

func run(opts Options) error {
	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// ---- 日志同时写入文件，便于「日志」页读取（开发文档 47 节）----
	logFile, err := os.OpenFile(filepath.Join(opts.DataDir, "panel.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开面板日志失败: %w", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	log.SetFlags(log.Ldate | log.Ltime)

	// ---- 数据库 ----
	database, err := db.Open(filepath.Join(opts.DataDir, "panel.db"))
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.CleanupExpiredSessions(); err != nil {
		log.Printf("清理过期会话失败: %v", err)
	}

	// ---- 引导管理员 ----
	generatedPassword, err := bootstrapAdmin(database, opts.AdminUser, opts.AdminPassword)
	if err != nil {
		return err
	}

	// ---- 管理器 ----
	mgr := manager.New(manager.Options{
		DB:           database,
		ConfigPath:   opts.ConfigPath,
		Binary:       opts.Binary,
		BackupDir:    opts.BackupDir,
		ServiceUnit:  opts.ServiceUnit,
		PanelVersion: server.Version,
	})

	// ---- 前端资源 ----
	assets, err := resolveAssets(opts.WebDir)
	if err != nil {
		log.Printf("前端资源不可用：%v（API 仍可正常使用）", err)
	}

	// ---- 播种初始 HY2 用户 ----
	// userpass 为空的配置会被官方 Core 拒绝，因此首次启动必须保证至少有一个用户。
	initialUserPassword, err := seedInitialUser(database, mgr, opts.InitialUser, opts.InitialUserPassword)
	if err != nil {
		log.Printf("创建初始 HY2 用户失败: %v", err)
	}

	// ---- HTTP 服务 ----
	srv := server.New(server.Options{
		DB:           database,
		Manager:      mgr,
		Assets:       assets,
		TemplatePath: resolveTemplatePath(opts.WebDir),
		DataDir:      opts.DataDir,
	})

	// 启动时顺手放行一次监听端口（内部会自己记日志，没防火墙则静默）
	srv.EnsureFirewall()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.Start(ctx)

	httpServer := &http.Server{
		Addr:              opts.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	printBanner(opts.Listen, opts.AdminUser, generatedPassword, opts.AdminPassword != "", opts.InitialUser, initialUserPassword)
	log.Printf("HY2 Panel %s 已启动，监听 %s", server.Version, opts.Listen)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// bootstrapAdmin 确保至少存在一个管理员。
//
// 安全要求（开发文档 60 / 62 节）：
//   - 绝不写死默认密码；
//   - 未指定密码时生成高强度随机密码，并仅在首次启动时打印一次；
//   - 指定的密码不会回显到日志，避免敏感信息落盘。
//
// 返回值为「自动生成的密码」；若由调用方指定密码则返回空串。
func bootstrapAdmin(database *db.DB, username, password string) (generated string, err error) {
	n, err := database.CountAdmins()
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", nil
	}

	if username == "" {
		username = "admin"
	}

	if password == "" {
		password, err = randomPassword(18)
		if err != nil {
			return "", err
		}
		generated = password
	} else if len(password) < 8 {
		return "", errors.New("指定的管理员密码至少需要 8 位")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希失败: %w", err)
	}
	if _, err := database.CreateAdmin(username, hash); err != nil {
		return "", err
	}
	return generated, nil
}

// seedInitialUser 在没有任何 HY2 用户时创建一个初始用户。
//
// 原因：官方 Core 会拒绝 userpass 为空的配置
// （invalid config: auth.userpass: empty auth userpass），
// 若不在首次启动时播种一个用户，全新安装的服务将无法启动。
//
// 返回「自动生成的密码」；由调用方指定密码时返回空串。
func seedInitialUser(database *db.DB, mgr *manager.Manager, username, password string) (generated string, err error) {
	// 仅当官方配置使用 userpass 认证时才需要播种
	cfg, err := mgr.Current()
	if err != nil {
		return "", nil // 配置还不存在，交给后续流程处理
	}
	if cfg.Server.Auth == nil || cfg.Server.Auth.Type != "userpass" {
		return "", nil
	}

	n, err := database.CountUsers()
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", nil
	}

	if username == "" {
		username = "user1"
	}

	pw := password
	if pw == "" {
		pw, err = randomPassword(16)
		if err != nil {
			return "", err
		}
		generated = pw
	} else if len(pw) < 8 {
		return "", errors.New("指定的初始用户密码至少需要 8 位")
	}

	if _, err := database.CreateUser(username, pw, "初始用户（由 Panel 自动创建）"); err != nil {
		return "", err
	}

	// 同步进官方配置；失败不阻塞启动，仅记录。
	if err := mgr.SyncUsers(context.Background()); err != nil {
		log.Printf("同步初始用户到官方配置失败（可稍后在面板中手动同步）: %v", err)
	}
	return generated, nil
}

// randomPassword 生成 URL 安全的随机密码。
func randomPassword(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机密码失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// printBanner 输出首次启动信息（对齐开发文档 60 节）。
//
// 只回显「自动生成」的密码；手动指定的密码不回显，避免写入日志。
// 标签刻意区分「管理员密码」与「HY2 用户密码」，便于安装脚本精确提取。
func printBanner(listen, adminUser, adminPassword string, adminPasswordProvided bool, hy2User, hy2Password string) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println(" HY2 Panel")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf(" Panel:   http://<本机地址>%s\n", displayListen(listen))

	if adminPassword != "" {
		fmt.Println(" 面板管理员（随机密码仅本次打印，请立即保存并修改）：")
		fmt.Printf("   用户名: %s\n", adminUser)
		fmt.Printf("   管理员密码: %s\n", adminPassword)
	} else if adminPasswordProvided {
		fmt.Printf(" 面板管理员：%s（密码已按指定值设置，不回显）\n", adminUser)
	}

	if hy2Password != "" {
		fmt.Println(" HY2 初始用户（客户端连接用；随机密码仅本次打印）：")
		fmt.Printf("   用户名: %s\n", hy2User)
		fmt.Printf("   HY2 用户密码: %s\n", hy2Password)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// displayListen 把监听地址转成便于拼接 URL 的形式。
//
// 例如 "0.0.0.0:8080" → ":8080"，"[::]:8080" → ":8080"。
func displayListen(listen string) string {
	switch {
	case strings.HasPrefix(listen, "0.0.0.0:"):
		return listen[len("0.0.0.0"):]
	case strings.HasPrefix(listen, "[::]:"):
		return listen[len("[::]"):]
	default:
		return listen
	}
}

// resolveAssets 解析前端静态资源目录。
func resolveAssets(webDir string) (fs.FS, error) {
	dirs := []string{}
	if webDir != "" {
		dirs = append(dirs, webDir)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "web", "dist"))
	}
	dirs = append(dirs, "/opt/hy2-panel/web/dist", "web/dist")

	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "index.html")); err == nil {
			return os.DirFS(d), nil
		}
	}
	return nil, errors.New("未找到前端构建产物（需包含 index.html）")
}

// resolveTemplatePath 解析 Clash 订阅模板路径。
func resolveTemplatePath(webDir string) string {
	candidates := []string{
		"templates/clash-meta.yaml",
		"/opt/hy2-panel/templates/clash-meta.yaml",
	}
	if webDir != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(webDir), "templates", "clash-meta.yaml"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "templates", "clash-meta.yaml"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "templates/clash-meta.yaml"
}

// ---------------------------------------------------------------------------
// CLI（开发文档 61 节）
// ---------------------------------------------------------------------------

// runCLI 处理 hy2-panel 的子命令（开发文档 61 节）。
//
// 返回 handled=true 表示已作为 CLI 处理完毕，无需启动服务。
func runCLI(args []string) (bool, error) {
	cmd := args[0]
	rest := args[1:]
	f := parseFlags(rest)

	unit := strDefault(f["service-unit"], "hysteria-server")

	switch cmd {
	case "status", "start", "stop", "restart":
		if !hy2.HasSystemd() {
			return true, errors.New("当前系统未使用 systemd")
		}
		out, err := hy2.ServiceAction(cmd, unit)
		if out != "" {
			fmt.Println(out)
		}
		if err != nil {
			return true, err
		}
		active, _ := hy2.ServiceActive(unit)
		fmt.Printf("%s: active=%v\n", unit, active)
		return true, nil

	case "update":
		binary := strDefault(f["hy2-bin"], "/usr/local/bin/hysteria")
		u := updater.New(binary, unit)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		res, err := u.Update(ctx)
		if err != nil {
			return true, err
		}
		fmt.Printf("已更新官方 Core：%s -> %s\n", res.From, res.To)
		fmt.Printf("上一版本备份：%s\n", res.BackupPath)
		return true, nil

	case "rollback":
		binary := strDefault(f["hy2-bin"], "/usr/local/bin/hysteria")
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := updater.New(binary, unit).Rollback(ctx); err != nil {
			return true, err
		}
		fmt.Println("已回滚到上一版本")
		return true, nil

	case "backup":
		cfgPath := strDefault(f["hy2-config"], "/opt/hy2-panel/config/hysteria.yaml")
		dir := strDefault(f["backup-dir"], "/opt/hy2-panel/backup")
		path, err := backupConfig(cfgPath, dir)
		if err != nil {
			return true, err
		}
		fmt.Printf("已备份到：%s\n", path)
		return true, nil

	case "restore":
		cfgPath := strDefault(f["hy2-config"], "/opt/hy2-panel/config/hysteria.yaml")
		dir := strDefault(f["backup-dir"], "/opt/hy2-panel/backup")
		file := f["file"]
		path, err := restoreConfig(cfgPath, dir, file)
		if err != nil {
			return true, err
		}
		fmt.Printf("已从 %s 恢复配置\n", path)
		if hy2.HasSystemd() {
			if _, err := hy2.ServiceAction("restart", unit); err != nil {
				return true, err
			}
			fmt.Println("服务已重启")
		}
		return true, nil

	case "uninstall":
		return true, uninstall(f, unit)

	case "passwd":
		return true, resetAdminPassword(f)

	case "help", "-h", "--help":
		fmt.Println(strings.Join([]string{
			"用法: hy2-panel <子命令> [选项]",
			"",
			"  status | start | stop | restart   管理官方 Hysteria 服务",
			"  update                            更新官方 Core（失败自动回滚）",
			"  rollback                          回滚到上一版本官方 Core",
			"  backup                            备份当前官方配置",
			"  restore                           恢复官方配置备份",
			"  passwd                            重置面板管理员密码",
			"  uninstall                         卸载 Panel（默认保留数据）",
			"  help                              显示帮助",
			"",
			"常用选项：",
			"  -hy2-bin PATH        官方 Core 路径（默认 /usr/local/bin/hysteria）",
			"  -hy2-config PATH     官方配置文件路径",
			"  -backup-dir DIR      备份目录",
			"  -service-unit NAME   systemd 单元名（默认 hysteria-server）",
			"  -file NAME           restore 时指定备份文件名（默认取最新）",
			"  -data-dir DIR        passwd 使用的数据目录（默认 /opt/hy2-panel/data）",
			"  -admin-user NAME     passwd 目标用户名（默认 admin）",
			"  -admin-password PW   passwd 新密码（至少 8 位）",
			"  -purge               uninstall 时同时删除 /opt/hy2-panel 数据",
			"  -yes                 uninstall 时跳过确认",
			"",
			"服务启动时可用 -admin-password 指定首个管理员的初始密码（留空则随机生成）。",
			"不带子命令时按普通服务启动（见 hy2-panel -h）。",
		}, "\n"))
		return true, nil

	default:
		// 不是已知子命令，交回 flag 解析流程。
		return false, nil
	}
}

// parseFlags 解析形如 -key value / -key=value / -flag 的简易参数。
func parseFlags(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		key := strings.TrimLeft(a, "-")
		if eq := strings.Index(key, "="); eq >= 0 {
			out[key[:eq]] = key[eq+1:]
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out[key] = args[i+1]
			i++
			continue
		}
		out[key] = "true"
	}
	return out
}

func strDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// backupConfig 备份官方配置文件。
func backupConfig(cfgPath, dir string) (string, error) {
	content, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", fmt.Errorf("读取配置失败: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	name := fmt.Sprintf("config-%s.yaml", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		return "", fmt.Errorf("写入备份失败: %w", err)
	}
	return dst, nil
}

// restoreConfig 从备份恢复配置；file 为空时取最新一份。
func restoreConfig(cfgPath, dir, file string) (string, error) {
	src := ""
	if file != "" {
		src = filepath.Join(dir, file)
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("读取备份目录失败: %w", err)
		}
		latest := ""
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			if e.Name() > latest {
				latest = e.Name()
			}
		}
		if latest == "" {
			return "", errors.New("备份目录中没有可用的 .yaml 备份")
		}
		src = filepath.Join(dir, latest)
	}

	content, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("读取备份失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return "", fmt.Errorf("创建配置目录失败: %w", err)
	}
	if err := os.WriteFile(cfgPath, content, 0o600); err != nil {
		return "", fmt.Errorf("写入配置失败: %w", err)
	}
	return src, nil
}

// uninstall 卸载 Panel。
func uninstall(f map[string]string, unit string) error {
	if f["yes"] != "true" {
		fmt.Printf("即将卸载 HY2 Panel（停止并移除 %s 单元）。确认请输入 y：", unit)
		var ans string
		_, _ = fmt.Scanln(&ans)
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("已取消")
			return nil
		}
	}

	if hy2.HasSystemd() {
		if _, err := hy2.ServiceAction("stop", unit); err != nil {
			fmt.Printf("停止服务失败（继续）：%v\n", err)
		}
		if _, err := hy2.ServiceAction("disable", unit); err != nil {
			fmt.Printf("禁用服务失败（继续）：%v\n", err)
		}
	}

	unitFiles := []string{
		"/etc/systemd/system/" + unit + ".service",
		"/etc/systemd/system/hy2-panel.service",
	}
	for _, p := range unitFiles {
		if err := os.Remove(p); err == nil {
			fmt.Printf("已删除 %s\n", p)
		}
	}
	if hy2.HasSystemd() {
		_, _ = hy2.ServiceAction("daemon-reload", "")
	}

	if f["purge"] == "true" {
		fmt.Println("正在删除 /opt/hy2-panel 数据…")
		if err := os.RemoveAll("/opt/hy2-panel"); err != nil {
			return fmt.Errorf("删除数据目录失败: %w", err)
		}
		fmt.Println("数据目录已删除")
	} else {
		fmt.Println("已保留 /opt/hy2-panel（如需彻底删除请使用 -purge）")
	}

	fmt.Println("卸载完成。官方 Hysteria 2 核心与配置未被删除。")
	return nil
}

// resetAdminPassword 重置管理员密码，并使该管理员的既有会话全部失效。
//
// 对应开发文档 61 节的 CLI 能力：hy2-panel passwd
func resetAdminPassword(f map[string]string) error {
	dataDir := strDefault(f["data-dir"], "/opt/hy2-panel/data")
	username := strDefault(f["admin-user"], "admin")
	password := f["admin-password"]

	if password == "" {
		return errors.New("请通过 -admin-password 指定新密码")
	}
	if len(password) < 8 {
		return errors.New("密码至少需要 8 位")
	}

	database, err := db.Open(filepath.Join(dataDir, "panel.db"))
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	defer database.Close()

	admin, err := database.GetAdminByUsername(username)
	if err != nil {
		return fmt.Errorf("管理员 %q 不存在（数据目录：%s）", username, dataDir)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}
	if err := database.UpdateAdminPassword(admin.ID, hash); err != nil {
		return fmt.Errorf("保存新密码失败: %w", err)
	}
	if err := database.DeleteAdminSessions(admin.ID); err != nil {
		return fmt.Errorf("注销旧会话失败: %w", err)
	}

	fmt.Printf("管理员 %q 的密码已重置，所有旧会话已失效。\n", username)
	return nil
}