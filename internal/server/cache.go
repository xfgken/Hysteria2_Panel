package server

import (
	"sync"
	"time"
)

// ttlCache 是一个极简的带过期时间的缓存。
//
// 用途：仪表盘会以秒级频率刷新，而「官方 Core 版本」与「服务运行状态」
// 分别需要启动一次子进程（hysteria version / systemctl is-active）。
// 若每次请求都重新执行，会产生大量无谓的进程创建，因此这里做短时缓存。
type ttlCache struct {
	mu      sync.Mutex
	entries map[string]ttlEntry
}

type ttlEntry struct {
	value   any
	expires time.Time
}

func newTTLCache() *ttlCache {
	return &ttlCache{entries: make(map[string]ttlEntry)}
}

// get 取值；未命中或已过期返回 ok=false。
func (c *ttlCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return e.value, true
}

// set 写入值并设定存活时间。
func (c *ttlCache) set(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = ttlEntry{value: value, expires: time.Now().Add(ttl)}
}

// invalidate 主动失效（例如配置变更后）。
func (c *ttlCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// 缓存键与存活时间
const (
	cacheKeyCoreVersion   = "core_version"
	cacheKeyServiceActive = "service_active"

	// 版本只在更新时变化，可以缓存久一点
	ttlCoreVersion = 5 * time.Minute
	// 服务状态变化较快，但秒级缓存足以让界面保持「实时感」
	ttlServiceActive = 3 * time.Second
)

// coreVersion 带缓存地读取官方 Core 版本。
func (s *Server) coreVersion() string {
	if v, ok := s.cache.get(cacheKeyCoreVersion); ok {
		return v.(string)
	}
	v, err := coreVersion(s.mgr.Binary())
	if err != nil {
		v = ""
	}
	s.cache.set(cacheKeyCoreVersion, v, ttlCoreVersion)
	return v
}

// serviceActive 带缓存地判断官方服务是否在运行。
func (s *Server) serviceActive() bool {
	if v, ok := s.cache.get(cacheKeyServiceActive); ok {
		return v.(bool)
	}
	active, _ := serviceActive(s.mgr.ServiceUnit())
	s.cache.set(cacheKeyServiceActive, active, ttlServiceActive)
	return active
}