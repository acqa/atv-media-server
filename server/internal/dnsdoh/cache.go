package dnsdoh

import (
	"net"
	"sync"
	"time"
)

// Cache TTL clamps. Real-world TMDb CDN records advertise 30–300s; we cap to
// keep stale answers from sticking around when the CDN rotates IPs.
const (
	cacheMinTTL = 30 * time.Second
	cacheMaxTTL = 5 * time.Minute
)

type cacheEntry struct {
	ips     []net.IP
	expires time.Time
}

type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	now     func() time.Time // overridable in tests
}

func newCache() *cache {
	return &cache{
		entries: make(map[string]cacheEntry),
		now:     time.Now,
	}
}

func (c *cache) get(host string) ([]net.IP, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[host]
	if !ok || c.now().After(e.expires) {
		return nil, false
	}
	out := make([]net.IP, len(e.ips))
	copy(out, e.ips)
	return out, true
}

func (c *cache) set(host string, ips []net.IP, ttl time.Duration) {
	if ttl < cacheMinTTL {
		ttl = cacheMinTTL
	}
	if ttl > cacheMaxTTL {
		ttl = cacheMaxTTL
	}
	stored := make([]net.IP, len(ips))
	copy(stored, ips)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[host] = cacheEntry{ips: stored, expires: c.now().Add(ttl)}
}
