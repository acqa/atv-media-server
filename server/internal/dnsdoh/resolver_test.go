package dnsdoh

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResolver_HappyPath(t *testing.T) {
	want := []net.IP{net.IPv4(138, 199, 36, 11)}
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, want, 300)
	})

	r := NewResolver(Config{Providers: []string{doh.URL()}})
	ips, err := r.LookupIP(context.Background(), "image.tmdb.org")
	if err != nil {
		t.Fatalf("LookupIP: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(want[0]) {
		t.Errorf("got %v, want %v", ips, want)
	}
}

func TestResolver_FallsThroughOnHTTPError(t *testing.T) {
	primary := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusInternalServerError, []byte("upstream broken")
	})
	want := []net.IP{net.IPv4(9, 9, 9, 9)}
	secondary := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, want, 60)
	})

	r := NewResolver(Config{Providers: []string{primary.URL(), secondary.URL()}})
	ips, err := r.LookupIP(context.Background(), "example.org")
	if err != nil {
		t.Fatalf("LookupIP: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(want[0]) {
		t.Errorf("got %v, want %v", ips, want)
	}
	if primary.calls.Load() != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls.Load())
	}
	if secondary.calls.Load() != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls.Load())
	}
}

func TestResolver_FallsThroughOnLoopbackSinkhole(t *testing.T) {
	// First provider returns only loopback (sinkhole), second returns a real IP.
	sink := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, []net.IP{net.IPv4(127, 0, 0, 1)}, 60)
	})
	clean := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, []net.IP{net.IPv4(65, 9, 62, 20)}, 60)
	})

	r := NewResolver(Config{Providers: []string{sink.URL(), clean.URL()}})
	ips, err := r.LookupIP(context.Background(), "api.themoviedb.org")
	if err != nil {
		t.Fatalf("LookupIP: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(65, 9, 62, 20)) {
		t.Errorf("got %v, want [65.9.62.20]", ips)
	}
}

func TestResolver_AllProvidersFail(t *testing.T) {
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusBadGateway, []byte("nope")
	})

	r := NewResolver(Config{Providers: []string{doh.URL(), doh.URL()}})
	_, err := r.LookupIP(context.Background(), "example.org")
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "dnsdoh: lookup example.org") {
		t.Errorf("error missing context: %v", err)
	}
}

func TestResolver_CacheHitSkipsHTTP(t *testing.T) {
	want := []net.IP{net.IPv4(1, 2, 3, 4)}
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		return http.StatusOK, answer(t, q, want, 300)
	})

	r := NewResolver(Config{Providers: []string{doh.URL()}})
	if _, err := r.LookupIP(context.Background(), "example.org"); err != nil {
		t.Fatalf("first LookupIP: %v", err)
	}
	if _, err := r.LookupIP(context.Background(), "example.org"); err != nil {
		t.Fatalf("second LookupIP: %v", err)
	}
	if got := doh.calls.Load(); got != 1 {
		t.Errorf("DoH calls = %d, want 1 (second lookup should be a cache hit)", got)
	}
}

func TestResolver_CtxCancellation(t *testing.T) {
	// A DoH endpoint that never responds — we expect ctx cancellation to
	// unblock the lookup promptly.
	doh := newFakeDoH(t, func(q []byte) (int, []byte) {
		time.Sleep(5 * time.Second) // longer than the test's tolerance
		return http.StatusOK, q
	})

	r := NewResolver(Config{Providers: []string{doh.URL()}, LookupTimeout: 100 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := r.LookupIP(ctx, "example.org")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if elapsed > time.Second {
		t.Errorf("cancellation took %s, want < 1s", elapsed)
	}
	if !isTimeoutLike(err) {
		t.Errorf("error doesn't look timeout-related: %v", err)
	}
}

func isTimeoutLike(err error) bool {
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "context") || strings.Contains(s, "deadline") || strings.Contains(s, "Timeout")
}
