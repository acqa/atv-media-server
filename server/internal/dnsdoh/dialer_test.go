package dnsdoh

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startEchoTCP starts a TCP listener on 127.0.0.1 that speaks minimal HTTP/1.1
// and echoes the Host header back in the body. Used to verify the dialer
// preserves the original host (not the resolved IP) on the HTTP layer.
func startEchoTCP(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, r.Host)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	return ln
}

func TestDialer_IPLiteralFastPath(t *testing.T) {
	// A resolver that would error if consulted — proves the fast path.
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusInternalServerError, []byte("should not be called")
	})
	r := NewResolver(Config{Providers: []string{doh.URL()}})

	ln := startEchoTCP(t)
	client := NewHTTPClient(r, 5*time.Second)

	// Issue request against http://<ip>:<port>/ — addr is already a literal IP.
	url := "http://" + ln.Addr().String() + "/"
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if doh.calls.Load() != 0 {
		t.Errorf("DoH was called %d times — IP-literal fast path failed", doh.calls.Load())
	}
}

func TestDialer_ResolvesAndDials(t *testing.T) {
	ln := startEchoTCP(t)
	port := ln.Addr().(*net.TCPAddr).Port

	// DoH endpoint returns 127.0.0.1 for our synthetic hostname.
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, []net.IP{net.IPv4(127, 0, 0, 1)}, 60)
	})
	// Disable loopback filtering for this test: provide a custom Resolver
	// whose answer the filter would normally reject. We pierce that by using
	// a synthetic host that resolves through a *test* DoH that returns a
	// loopback IP, knowing the production filter would skip it.
	//
	// We special-case this in the test by bypassing filterRoutable via a
	// custom Resolver: insert directly into the cache below.
	r := NewResolver(Config{Providers: []string{doh.URL()}})
	r.cache.set("test.example", []net.IP{net.IPv4(127, 0, 0, 1)}, time.Minute)

	client := NewHTTPClient(r, 5*time.Second)

	url := fmt.Sprintf("http://test.example:%d/", port)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	gotHost := string(body)
	wantHost := fmt.Sprintf("test.example:%d", port)
	if gotHost != wantHost {
		t.Errorf("Host header = %q, want %q (dialer must not rewrite Host)", gotHost, wantHost)
	}
}

// Cross-platform "give me an unbound TCP port on 127.0.0.1" — grab a port and
// immediately release it. Subsequent dials to that port will ECONNREFUSED
// quickly on all common Unixes and on macOS.
func reservedFreePort(t *testing.T) int {
	t.Helper()
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := tmp.Addr().(*net.TCPAddr).Port
	_ = tmp.Close()
	return port
}

func TestDialer_DialFailureWrapsHost(t *testing.T) {
	// All-IPs-fail path: single resolver answer of 127.0.0.1, dialed against a
	// port we know is unbound. Verifies the error returned by the transport's
	// DialContext is wrapped with the original hostname (so logs say
	// "dial api.themoviedb.org: ... refused", not just "dial 127.0.0.1: ...").
	port := reservedFreePort(t)

	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, []net.IP{net.IPv4(127, 0, 0, 1)}, 60)
	})
	r := NewResolver(Config{Providers: []string{doh.URL()}})
	r.cache.set("dead.example", []net.IP{net.IPv4(127, 0, 0, 1)}, time.Minute)

	client := NewHTTPClient(r, 3*time.Second)
	_, err := client.Get(fmt.Sprintf("http://dead.example:%d/", port))
	if err == nil {
		t.Fatal("expected error when dial target has no listener")
	}
	if !strings.Contains(err.Error(), "dial dead.example") {
		t.Errorf("error missing host context (want 'dial dead.example: ...'): %v", err)
	}
}

func TestDialer_NilResolverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil resolver")
		}
	}()
	_ = NewHTTPClient(nil, time.Second)
}
