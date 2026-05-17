package dnsdoh

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	dialTimeout         = 10 * time.Second
	tlsHandshakeTimeout = 10 * time.Second
)

// NewHTTPClient returns a ready-to-use *http.Client whose DialContext resolves
// the hostname via the provided Resolver and then dials the returned IPs
// directly, bypassing the system resolver.
//
// requestTimeout sets the resulting client's overall request timeout (passed
// straight to http.Client.Timeout). Use 15s for TMDb API calls; 30s for image
// CDN fetches (Phase 0 observed a ~15s body-transfer tail on the target
// network — a tighter timeout would falsely abort slow-but-successful warms).
//
// If the input address is already a literal IP (no DNS work needed), the
// dialer takes a fast path and skips the DoH lookup entirely.
func NewHTTPClient(r *Resolver, requestTimeout time.Duration) *http.Client {
	if r == nil {
		panic("dnsdoh.NewHTTPClient: resolver is nil")
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := r.LookupIP(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = errors.New("no routable IPs to dial")
			}
			return nil, fmt.Errorf("dnsdoh: dial %s: %w", host, lastErr)
		},
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: 15 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{},
	}
	return &http.Client{Timeout: requestTimeout, Transport: transport}
}
