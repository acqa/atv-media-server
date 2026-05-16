package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atv-media-server/server/internal/library"
	"github.com/atv-media-server/server/internal/server"
	"github.com/atv-media-server/server/internal/storage"
)

func newAdmin(t *testing.T) (http.Handler, *storage.Store, *server.ScanState) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	st := server.NewScanState(func(context.Context, library.Logger) (library.ScanResult, error) {
		return library.ScanResult{}, nil
	})
	return buildMux(Config{User: "u", Pass: "p"}, Deps{Store: store, ScanState: st}), store, st
}

func TestAuth_NoCreds_401(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/api/stats")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("missing challenge header: %q", got)
	}
}

func TestAuth_WrongCreds_403(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stats", nil)
	req.SetBasicAuth("u", "bad")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("want 403, got %d", resp.StatusCode)
	}
}

func TestAuth_CorrectCreds_200(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stats", nil)
	req.SetBasicAuth("u", "p")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var s stats
	if err := json.Unmarshal(body, &s); err != nil {
		t.Errorf("invalid json: %v\n%s", err, body)
	}
	if s.Movies != 0 || s.Series != 0 || s.Artists != 0 {
		t.Errorf("counts: %+v", s)
	}
}

func TestStats_ReflectsDatabase(t *testing.T) {
	mux, store, _ := newAdmin(t)
	_ = store.UpsertMedia(storage.MediaRow{ID: "m1", Path: "/m/1", Type: "movie", Title: "M1"})
	_ = store.UpsertSeries(storage.SeriesRow{ID: "s1", Path: "/s/1", Title: "S1"})
	_ = store.UpsertArtist(storage.ArtistRow{ID: "a1", Name: "A1"})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stats", nil)
	req.SetBasicAuth("u", "p")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	var s stats
	_ = json.NewDecoder(resp.Body).Decode(&s)
	if s.Movies != 1 || s.Series != 1 || s.Artists != 1 {
		t.Errorf("counts: %+v", s)
	}
}

func TestScan_KicksOffScanState(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/scan", nil)
	req.SetBasicAuth("u", "p")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("want 202, got %d", resp.StatusCode)
	}
}

func TestScanStatus_ReturnsJSON(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/scan/status", nil)
	req.SetBasicAuth("u", "p")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: %q", ct)
	}
}

func TestStatic_IndexHTMLServed(t *testing.T) {
	mux, _, _ := newAdmin(t)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.SetBasicAuth("u", "p")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ATV3 Media Server") {
		t.Errorf("index.html not served:\n%s", body)
	}
}

func TestServe_DisabledWhenCredsEmpty(t *testing.T) {
	// We don't want to actually bind a port; just verify the no-op path
	// completes synchronously by passing an empty user.
	if err := Serve(Config{User: ""}, Deps{}); err != nil {
		t.Errorf("disabled path should return nil, got %v", err)
	}
}
