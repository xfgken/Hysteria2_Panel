package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/hy2"
)

// handleLogs 返回指定来源的日志（开发文档 47 节）。
//
// source: hysteria | panel | nginx
// 日志源配置支持两种形式：
//   - "journal:<unit>" 通过 journalctl 读取 systemd 日志；
//   - 其它值视为文件路径，读取文件尾部。
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if source != "hysteria" && source != "panel" && source != "nginx" {
		writeError(w, http.StatusBadRequest, "不支持的日志来源")
		return
	}

	lines := 300
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := parseInt(v); err == nil && n > 0 && n <= 5000 {
			lines = n
		}
	}

	target := s.logPath(source)
	var (
		content string
		err     error
	)
	if strings.HasPrefix(target, "journal:") {
		unit := strings.TrimPrefix(target, "journal:")
		content, err = readJournal(unit, lines)
	} else {
		content, err = readTail(target, lines)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// 过滤：关键字搜索 + 级别
	keyword := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	level := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("level")))
	if keyword != "" || level != "" {
		var kept []string
		for _, line := range strings.Split(content, "\n") {
			lower := strings.ToLower(line)
			if keyword != "" && !strings.Contains(lower, keyword) {
				continue
			}
			if level != "" && !strings.Contains(lower, level) {
				continue
			}
			kept = append(kept, line)
		}
		content = strings.Join(kept, "\n")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source": source,
		"target": target,
		"lines":  lines,
		"content": content,
	})
}

// handleClearLogs 清空某个日志来源的内容。
//
// 仅支持文件型日志；journal 类型无法直接删除（journald 自行管理），
// 此时返回明确说明，避免误以为已清空。
func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	source := r.PathValue("source")
	if source != "hysteria" && source != "panel" && source != "nginx" {
		writeError(w, http.StatusBadRequest, "不支持的日志来源")
		return
	}

	target := s.logPath(source)
	if strings.HasPrefix(target, "journal:") {
		writeError(w, http.StatusBadRequest,
			"该日志来源是 systemd journal（"+target+"），无法直接清空；"+
				"可在「设置」页把日志来源改为文件路径，或用 journalctl --vacuum-time 清理")
		return
	}

	if err := os.Truncate(target, 0); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "日志文件不存在: "+target)
			return
		}
		writeError(w, http.StatusInternalServerError, "清空日志失败: "+err.Error())
		return
	}

	log.Printf("[用户操作] 清空日志 source=%s file=%s", source, target)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "日志已清空",
		"target":  target,
	})
}

// readJournal 通过 journalctl 读取 systemd 日志。
func readJournal(unit string, lines int) (string, error) {
	bin, err := exec.LookPath("journalctl")
	if err != nil {
		return "", &logError{"当前系统没有 journalctl，无法读取 " + unit + " 的日志"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin,
		"-u", unit,
		"-n", itoa(lines),
		"--no-pager",
		"-o", "short-iso",
	).CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", &logError{"读取 journalctl 失败: " + err.Error()}
	}
	return string(out), nil
}

// readTail 读取文件尾部若干行。
func readTail(path string, lines int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &logError{"日志文件不存在: " + path}
		}
		return "", &logError{"读取日志失败: " + err.Error()}
	}
	// 仅保留末尾 256KB，避免超大日志拖垮内存。
	const maxBytes = 256 * 1024
	if len(data) > maxBytes {
		data = data[len(data)-maxBytes:]
	}
	all := strings.Split(string(data), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n"), nil
}

// logError 是日志读取错误。
type logError struct{ msg string }

func (e *logError) Error() string { return e.msg }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// coreVersion 读取官方 Core 版本（供仪表盘展示）。
func coreVersion(binary string) (string, error) {
	return hy2.BinaryVersion(binary)
}

// serviceActive 判断服务是否在运行。
func serviceActive(unit string) (bool, error) {
	return hy2.ServiceActive(unit)
}