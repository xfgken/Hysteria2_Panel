package server

import (
    "errors"
    "math"
    "net/http"
    "os"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "time"
)

// HostStats 是 VPS 主机的资源快照（CPU / 内存 / 磁盘 / 负载）。
type HostStats struct {
    Hostname     string  `json:"hostname"`
    Kernel       string  `json:"kernel"`
    CPUCores     int     `json:"cpuCores"`
    CPUPercent   float64 `json:"cpuPercent"`
    Load1        float64 `json:"load1"`
    Load5        float64 `json:"load5"`
    Load15       float64 `json:"load15"`
    MemTotal     uint64  `json:"memTotal"`
    MemUsed      uint64  `json:"memUsed"`
    MemAvailable uint64  `json:"memAvailable"`
    MemPercent   float64 `json:"memPercent"`
    SwapTotal    uint64  `json:"swapTotal"`
    SwapUsed     uint64  `json:"swapUsed"`
    DiskTotal    uint64  `json:"diskTotal"`
    DiskUsed     uint64  `json:"diskUsed"`
    DiskFree     uint64  `json:"diskFree"`
    DiskPercent  float64 `json:"diskPercent"`
    Uptime       float64 `json:"uptime"`
    UpdatedAt    string  `json:"updatedAt"`
}

// hostCPUStat 保存上一次 /proc/stat 的累计值，用两次采样差值算 CPU 占用率。
type hostCPUStat struct {
    mu    sync.Mutex
    total uint64
    idle  uint64
    ready bool
}

var hostCPU hostCPUStat

var errNoCPULine = errors.New("/proc/stat: 未找到 cpu 行")

// handleHostStats 返回主机资源（前端“实时刷新”轮询这里的）。
func (s *Server) handleHostStats(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, collectHostStats())
}

func collectHostStats() HostStats {
    var st HostStats
    st.Hostname, _ = os.Hostname()
    st.CPUCores = runtime.NumCPU()
    if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
        st.Kernel = strings.TrimSpace(string(b))
    }
    st.CPUPercent = hostCPUPercent()
    if load, err := readLoadAvg(); err == nil {
        if len(load) > 0 {
            st.Load1 = load[0]
        }
        if len(load) > 1 {
            st.Load5 = load[1]
        }
        if len(load) > 2 {
            st.Load15 = load[2]
        }
    }
    mem := hostMemInfo()
    st.MemTotal = mem["MemTotal"]
    st.MemAvailable = mem["MemAvailable"]
    if st.MemAvailable == 0 {
        st.MemAvailable = mem["MemFree"]
    }
    if st.MemTotal > st.MemAvailable {
        st.MemUsed = st.MemTotal - st.MemAvailable
    }
    st.SwapTotal = mem["SwapTotal"]
    if st.SwapTotal > mem["SwapFree"] {
        st.SwapUsed = st.SwapTotal - mem["SwapFree"]
    }
    if total, used, free, err := hostDiskUsage("/"); err == nil {
        st.DiskTotal, st.DiskUsed, st.DiskFree = total, used, free
    }
    st.MemPercent = hostPercent(st.MemUsed, st.MemTotal)
    st.DiskPercent = hostPercent(st.DiskUsed, st.DiskTotal)
    if up, err := readUptime(); err == nil {
        st.Uptime = up
    }
    st.UpdatedAt = time.Now().Format("15:04:05")
    return st
}

func hostPercent(used, total uint64) float64 {
    if total == 0 {
        return 0
    }
    return math.Round(float64(used)/float64(total)*1000) / 10
}

// hostCPUPercent 用 /proc/stat 两次采样差值算占用率（0~100）。
func hostCPUPercent() float64 {
    total, idle, err := hostCPUTimes()
    if err != nil {
        return 0
    }
    hostCPU.mu.Lock()
    defer hostCPU.mu.Unlock()
    if !hostCPU.ready {
        // 首次采样没有参照，短暂等一等再采一次
        hostCPU.total, hostCPU.idle, hostCPU.ready = total, idle, true
        time.Sleep(150 * time.Millisecond)
        if total, idle, err = hostCPUTimes(); err != nil {
            return 0
        }
    }
    deltaTotal := float64(total - hostCPU.total)
    deltaIdle := float64(idle - hostCPU.idle)
    hostCPU.total, hostCPU.idle = total, idle
    if deltaTotal <= 0 {
        return 0
    }
    p := (1 - deltaIdle/deltaTotal) * 100
    if p < 0 {
        p = 0
    }
    if p > 100 {
        p = 100
    }
    return math.Round(p*10) / 10
}

func hostCPUTimes() (total, idle uint64, err error) {
    b, err := os.ReadFile("/proc/stat")
    if err != nil {
        return 0, 0, err
    }
    line := strings.SplitN(string(b), "\n", 2)[0]
    fields := strings.Fields(line)
    if len(fields) < 5 || fields[0] != "cpu" {
        return 0, 0, errNoCPULine
    }
    vals := make([]uint64, 0, len(fields)-1)
    for _, raw := range fields[1:] {
        v, e := strconv.ParseUint(raw, 10, 64)
        if e != nil {
            v = 0
        }
        vals = append(vals, v)
    }
    for _, v := range vals {
        total += v
    }
    idle = vals[3]
    if len(vals) > 4 {
        idle += vals[4] // iowait 也算空闲
    }
    return total, idle, nil
}

// hostMemInfo 读 /proc/meminfo，返回字节数（键名去掉冒号）。
func hostMemInfo() map[string]uint64 {
    out := make(map[string]uint64)
    b, err := os.ReadFile("/proc/meminfo")
    if err != nil {
        return out
    }
    for _, line := range strings.Split(string(b), "\n") {
        fields := strings.Fields(line)
        if len(fields) < 2 {
            continue
        }
        key := strings.TrimSuffix(fields[0], ":")
        v, e := strconv.ParseUint(fields[1], 10, 64)
        if e != nil {
            continue
        }
        out[key] = v * 1024 // meminfo 单位是 kB
    }
    return out
}

func hostDiskUsage(path string) (total, used, free uint64, err error) {
    var fs syscall.Statfs_t
    if err = syscall.Statfs(path, &fs); err != nil {
        return 0, 0, 0, err
    }
    blockSize := uint64(fs.Bsize)
    total = fs.Blocks * blockSize
    free = fs.Bavail * blockSize
    if total > free {
        used = total - free
    }
    return total, used, free, nil
}
