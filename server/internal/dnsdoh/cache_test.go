package dnsdoh

import (
	"net"
	"testing"
	"time"
)

func TestCache_HitAndMiss(t *testing.T) {
	c := newCache()
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	ip := net.IPv4(1, 2, 3, 4)
	c.set("example.org", []net.IP{ip}, time.Minute)

	got, ok := c.get("example.org")
	if !ok {
		t.Fatal("expected hit immediately after set")
	}
	if len(got) != 1 || !got[0].Equal(ip) {
		t.Errorf("get returned %v, want [%s]", got, ip)
	}

	if _, ok := c.get("other.example"); ok {
		t.Error("unexpected hit for unset host")
	}
}

func TestCache_Expiry(t *testing.T) {
	c := newCache()
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	c.set("example.org", []net.IP{net.IPv4(1, 2, 3, 4)}, time.Minute)
	now = now.Add(31 * time.Second)
	if _, ok := c.get("example.org"); !ok {
		t.Error("entry should still be live at 31s")
	}
	now = now.Add(60 * time.Second)
	if _, ok := c.get("example.org"); ok {
		t.Error("entry should be expired past TTL")
	}
}

func TestCache_TTLClamp(t *testing.T) {
	c := newCache()
	base := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return base }

	// Below the floor → clamped up to cacheMinTTL.
	c.set("low.org", []net.IP{net.IPv4(1, 1, 1, 1)}, time.Second)
	low := c.entries["low.org"].expires.Sub(base)
	if low < cacheMinTTL {
		t.Errorf("low-TTL clamp: got %s, want ≥ %s", low, cacheMinTTL)
	}

	// Above the ceiling → clamped down to cacheMaxTTL.
	c.set("high.org", []net.IP{net.IPv4(1, 1, 1, 1)}, 24*time.Hour)
	high := c.entries["high.org"].expires.Sub(base)
	if high > cacheMaxTTL {
		t.Errorf("high-TTL clamp: got %s, want ≤ %s", high, cacheMaxTTL)
	}
}
