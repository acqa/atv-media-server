// Package dnsdoh resolves DNS over HTTPS (RFC 8484) and exposes an
// http.Client whose DialContext bypasses the system resolver entirely.
//
// Motivation. On networks that DNS-sinkhole TMDb (system resolver returns
// 127.0.0.1 / ::1 for image.tmdb.org and api.themoviedb.org), the metadata
// fetch and poster warm fail with "dial tcp [::1]:443: connect: connection
// refused". This package resolves those names through a list of DoH endpoints
// instead, filters out loopback / unspecified answers (which are sinkhole
// signals, not real records), and dials the returned IPs directly.
//
// See .notes/task/1/PLAN.md for the diagnostic that led to this design and
// .notes/task/1/PROGRESS.md for verbatim verification output.
package dnsdoh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultProviders is the data-backed provider list (Phase 0 results, target
// network). Cloudflare is primary; Quad9 on port 443 is the fallback. Google
// DoH is intentionally absent — its RU-edge recursors honour the local
// blocklist and return 127.0.0.1 for the same names.
var DefaultProviders = []string{
	"https://cloudflare-dns.com/dns-query",
	"https://dns.quad9.net/dns-query",
}

const (
	defaultLookupTimeout = 8 * time.Second
	maxDoHResponseBytes  = 64 * 1024
)

// Config controls the DoH resolver.
type Config struct {
	// Providers is the list of DoH endpoint URLs, tried in order until one
	// returns at least one routable (non-loopback) A record. Empty falls back
	// to DefaultProviders.
	Providers []string

	// LookupTimeout bounds a single DoH HTTP request. Default 8s.
	LookupTimeout time.Duration

	// HTTPClient is used to talk to the DoH endpoints themselves. Production
	// callers should leave this nil; tests inject a custom client to reach a
	// httptest.Server. The system resolver is fine for the DoH endpoints —
	// they aren't sinkholed on any tested network.
	HTTPClient *http.Client
}

// Resolver looks up A records via a list of DoH endpoints with an in-memory
// TTL cache. Safe for concurrent use.
type Resolver struct {
	providers     []string
	lookupTimeout time.Duration
	httpClient    *http.Client
	cache         *cache
}

// NewResolver constructs a Resolver. Most callers want NewHTTPClient instead.
func NewResolver(cfg Config) *Resolver {
	providers := cfg.Providers
	if len(providers) == 0 {
		providers = DefaultProviders
	}
	timeout := cfg.LookupTimeout
	if timeout <= 0 {
		timeout = defaultLookupTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout + 2*time.Second}
	}
	return &Resolver{
		providers:     providers,
		lookupTimeout: timeout,
		httpClient:    httpClient,
		cache:         newCache(),
	}
}

// LookupIP resolves host to a non-empty list of routable IPv4 addresses by
// trying each configured DoH provider in order. Loopback and unspecified
// answers are treated as sinkholes and cause the resolver to fall through to
// the next provider.
func (r *Resolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return nil, errors.New("dnsdoh: empty host")
	}
	if ips, ok := r.cache.get(host); ok {
		return ips, nil
	}

	var lastErr error
	for _, p := range r.providers {
		ips, ttl, err := r.queryProvider(ctx, p, host)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", p, err)
			continue
		}
		routable := filterRoutable(ips)
		if len(routable) == 0 {
			lastErr = fmt.Errorf("%s: only loopback answers (sinkhole)", p)
			continue
		}
		r.cache.set(host, routable, ttl)
		return routable, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no providers configured")
	}
	return nil, fmt.Errorf("dnsdoh: lookup %s: %w", host, lastErr)
}

func (r *Resolver) queryProvider(ctx context.Context, url, name string) ([]net.IP, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, r.lookupTimeout)
	defer cancel()

	q, err := buildQuery(name)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(q))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDoHResponseBytes))
	if err != nil {
		return nil, 0, err
	}
	return parseAnswers(body)
}

// filterRoutable removes loopback and unspecified addresses. Both are
// well-known sinkhole responses (127.0.0.1, ::1, 0.0.0.0, ::) and never
// belong in a real A/AAAA record for a public CDN host.
func filterRoutable(ips []net.IP) []net.IP {
	out := ips[:0:0]
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		out = append(out, ip)
	}
	return out
}
