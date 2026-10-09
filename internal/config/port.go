package config

import (
	"strconv"
	"strings"
)

// ListenPortRange 解析 listen 里的端口，返回起止端口（单端口时两者相同）。
//
// 支持 ":443"、":20000-50000"（端口跳跃）、"0.0.0.0:443"、"[::]:443"。
// 解析不出来时返回 (0, 0)，调用方据此跳过端口相关处理。
func ListenPortRange(listen string) (int, int) {
	if listen == "" || strings.HasPrefix(listen, "realm://") {
		return 0, 0
	}
	idx := strings.LastIndex(listen, ":")
	if idx < 0 {
		return 0, 0
	}
	spec := listen[idx+1:]
	if start, end, found := strings.Cut(spec, "-"); found {
		a, err1 := strconv.Atoi(start)
		b, err2 := strconv.Atoi(end)
		if err1 != nil || err2 != nil || a <= 0 || b <= 0 {
			return 0, 0
		}
		return a, b
	}
	p, err := strconv.Atoi(spec)
	if err != nil || p <= 0 {
		return 0, 0
	}
	return p, p
}