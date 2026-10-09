package server

import (
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hy2-panel/hy2-panel/internal/hy2"
)

// handleSystemStatus 返回三个服务的运行状态（开发文档 48 节）。
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	panelState := "running"

	hy2Active, hy2Err := hy2.ServiceActive(s.mgr.ServiceUnit())
	nginxActive, nginxErr := hy2.ServiceActive("nginx")

	if !hy2.HasSystemd() {
		hy2Err = errNoSystemd()
		nginxErr = errNoSystemd()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"panel": map[string]any{
			"state":       panelState,
			"version":     Version,
			"uptime":      int64(time.Since(startTime).Seconds()),
			"systemd":     hy2.HasSystemd(),
			"serviceUnit": s.mgr.ServiceUnit(),
		},
		"hysteria": map[string]any{
			"active":  hy2Active,
			"version": mustCoreVersion(s.mgr.Binary()),
			"error":   errString(hy2Err),
		},
		"nginx": map[string]any{
			"active": nginxActive,
			"error":  errString(nginxErr),
		},
	})
}

// handleSystemInfo 返回系统信息。
func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	info := map[string]any{
		"hostname": hostname,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"goVersion": runtime.Version(),
		"numCPU":   runtime.NumCPU(),
		"goroutines": runtime.NumGoroutine(),
		"configPath": s.mgr.ConfigPath(),
		"backupDir":  s.mgr.BackupDir(),
		"serviceUnit": s.mgr.ServiceUnit(),
		"dataDir":   s.dataDir,
	}
	if up, err := readUptime(); err == nil {
		info["uptime"] = up
	}
	if total, free, err := readMemInfo(); err == nil {
		info["memTotal"] = total
		info["memFree"] = free
	}
	if load, err := readLoadAvg(); err == nil {
		info["loadAvg"] = load
	}
	writeJSON(w, http.StatusOK, info)
}

// handleServiceAction 管理服务（start / stop / restart / status）。
//
// name: hysteria | nginx
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action := r.PathValue("action")

	unit := ""
	switch name {
	case "hysteria":
		unit = s.mgr.ServiceUnit()
	case "nginx":
		unit = "nginx"
	default:
		writeError(w, http.StatusBadRequest, "不支持的服务")
		return
	}

	switch action {
	case "start", "stop", "restart", "status":
	default:
		writeError(w, http.StatusBadRequest, "不支持的操作")
		return
	}

	out, err := hy2.ServiceAction(action, unit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	active, _ := hy2.ServiceActive(unit)
	writeJSON(w, http.StatusOK, map[string]any{
		"output": out,
		"active": active,
	})
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

// startTime 记录 Panel 进程启动时间。
var startTime = time.Now()

func errNoSystemd() error { return &simpleError{"当前系统未使用 systemd"} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func mustCoreVersion(binary string) string {
	v, _ := hy2.BinaryVersion(binary)
	return v
}

// readUptime 读取系统运行时长（秒）。
func readUptime() (float64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, os.ErrInvalid
	}
	return strconv.ParseFloat(fields[0], 64)
}

// readMemInfo 读取内存总量与可用量（字节）。
func readMemInfo() (total, free uint64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, perr := strconv.ParseUint(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = val * 1024
		case "MemAvailable:":
			free = val * 1024
		}
	}
	return total, free, nil
}

// readLoadAvg 读取 1/5/15 分钟负载。
func readLoadAvg() ([]float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, os.ErrInvalid
	}
	out := make([]float64, 0, 3)
	for i := 0; i < 3; i++ {
		v, perr := strconv.ParseFloat(fields[i], 64)
		if perr != nil {
			return nil, perr
		}
		out = append(out, v)
	}
	return out, nil
}