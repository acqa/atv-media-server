package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atv-media-server/server/internal/storage"
)

func newPosterEnv(t *testing.T) (*storage.Store, *PosterCache, *atomic.Int32, string) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cache := NewPosterCache(filepath.Join(dataDir, "posters"), store)

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// fake JPEG bytes (just any non-empty payload)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0fake-jpeg-bytes"))
	}))
	t.Cleanup(upstream.Close)

	cache.SetImageBaseURL(upstream.URL)
	return store, cache, &hits, dataDir
}

func TestPosterCache_DownloadsAndServes(t *testing.T) {
	store, cache, hits, _ := newPosterEnv(t)
	const id = "abc123abc123"
	if err := store.UpsertMedia(storage.MediaRow{
		ID: id, Path: "/m/x.mkv", Type: "movie", Title: "X",
		BackdropPath: "/back.jpg",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cache.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/poster/" + id + ".jpg?type=backdrop&size=w500")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("empty body")
	}
	if hits.Load() != 1 {
		t.Errorf("want 1 upstream hit, got %d", hits.Load())
	}

	// Second request uses the cache, no upstream hit.
	resp2, _ := http.Get(srv.URL + "/poster/" + id + ".jpg?type=backdrop&size=w500")
	_ = resp2.Body.Close()
	if hits.Load() != 1 {
		t.Errorf("second request hit upstream: %d total", hits.Load())
	}
}

func TestPosterCache_FallbackForMissingPath(t *testing.T) {
	store, cache, hits, _ := newPosterEnv(t)
	const id = "deadbeef0000"
	_ = store.UpsertMedia(storage.MediaRow{ID: id, Path: "/m/x.mkv", Type: "movie", Title: "X"}) // no poster_path

	srv := httptest.NewServer(cache.Handler())
	t.Cleanup(srv.Close)

	// Disable redirect following so we can inspect the response.
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/poster/" + id + ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("want 302 redirect, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/assets/images/missing_logo.png" {
		t.Errorf("redirect to %q", loc)
	}
	if hits.Load() != 0 {
		t.Errorf("upstream hit despite missing path: %d", hits.Load())
	}
}

func TestPosterCache_UnknownMovieReturns404(t *testing.T) {
	_, cache, _, _ := newPosterEnv(t)
	srv := httptest.NewServer(cache.Handler())
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/poster/aaaaaaaaaaaa.jpg")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestPosterCache_RejectsBadInputs(t *testing.T) {
	_, cache, _, _ := newPosterEnv(t)
	srv := httptest.NewServer(cache.Handler())
	t.Cleanup(srv.Close)
	cases := []struct {
		path string
		want int
	}{
		{"/poster/abc.jpg", http.StatusBadRequest},                               // bad id
		{"/poster/abc123abc123.png", http.StatusNotFound},                        // wrong extension
		{"/poster/abc123abc123.jpg?type=fake", http.StatusBadRequest},            // bad type
		{"/poster/abc123abc123.jpg?type=poster&size=w42", http.StatusBadRequest}, // bad size
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			resp, _ := http.Get(srv.URL + c.path)
			_ = resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Errorf("%s: want %d, got %d", c.path, c.want, resp.StatusCode)
			}
		})
	}
}

func TestPosterCache_UpstreamErrorFallsBackToPlaceholder(t *testing.T) {
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	cache := NewPosterCache(filepath.Join(dataDir, "posters"), store)
	cache.SetImageBaseURL(upstream.URL)

	const id = "abcabcabcabc"
	_ = store.UpsertMedia(storage.MediaRow{ID: id, Path: "/x", Type: "movie", Title: "X", BackdropPath: "/b.jpg"})

	srv := httptest.NewServer(cache.Handler())
	t.Cleanup(srv.Close)

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(srv.URL + "/poster/" + id + ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("want 302 redirect on upstream error, got %d", resp.StatusCode)
	}

	// cache directory must not contain a partial file.
	entries, _ := os.ReadDir(filepath.Join(dataDir, "posters"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jpg" && !filepath.IsAbs(e.Name()) {
			t.Errorf("unexpected cached file after error: %s", e.Name())
		}
	}
}
