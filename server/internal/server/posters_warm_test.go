package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atv-media-server/server/internal/storage"
)

// newWarmEnv stands up a counted fake upstream and three cache types backed by
// the same temp dir. Returns the caches plus a counter of HTTP hits so tests
// can assert "was upstream actually queried".
type warmEnv struct {
	store       *storage.Store
	posters     *PosterCache
	series      *SeriesPosterCache
	episodes    *EpisodeStillCache
	upstreamURL string
	hits        *atomic.Int32
	root        string
}

func newWarmEnv(t *testing.T) *warmEnv {
	t.Helper()
	dataDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dataDir, "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := filepath.Join(dataDir, "posters")
	posters := NewPosterCache(root, store)
	series := NewSeriesPosterCache(root, store)
	episodes := NewEpisodeStillCache(root, store)

	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0fake"))
	}))
	t.Cleanup(upstream.Close)

	posters.SetImageBaseURL(upstream.URL)
	series.SetImageBaseURL(upstream.URL)
	episodes.SetImageBaseURL(upstream.URL)

	return &warmEnv{
		store: store, posters: posters, series: series, episodes: episodes,
		upstreamURL: upstream.URL, hits: &hits, root: root,
	}
}

func TestWarmMovies_FetchesTwoSizesPerPoster(t *testing.T) {
	env := newWarmEnv(t)
	env.posters.WarmMovies(context.Background(), []storage.MediaRow{
		{ID: "abc111abc111", PosterPath: "/p.jpg"},
		{ID: "def222def222", PosterPath: "/q.jpg"},
	})
	// 2 movies × 2 sizes (w500, w780) = 4 fetches.
	if got := env.hits.Load(); got != 4 {
		t.Errorf("upstream hits: want 4, got %d", got)
	}
	for _, want := range []string{
		"abc111abc111_poster_w500.jpg",
		"abc111abc111_poster_w780.jpg",
		"def222def222_poster_w500.jpg",
		"def222def222_poster_w780.jpg",
	} {
		if _, err := os.Stat(filepath.Join(env.root, want)); err != nil {
			t.Errorf("expected cache file %s: %v", want, err)
		}
	}
}

func TestWarmMovies_SkipsRowsWithoutPoster(t *testing.T) {
	env := newWarmEnv(t)
	env.posters.WarmMovies(context.Background(), []storage.MediaRow{
		{ID: "noart0000111"}, // no poster_path
		{ID: "hasart000111", PosterPath: "/p.jpg"},
	})
	// Only the row with PosterPath fetches → 2 sizes = 2 hits.
	if got := env.hits.Load(); got != 2 {
		t.Errorf("upstream hits: want 2, got %d", got)
	}
}

func TestWarmMovies_SkipsAlreadyCached(t *testing.T) {
	env := newWarmEnv(t)
	// First pass downloads everything.
	env.posters.WarmMovies(context.Background(), []storage.MediaRow{
		{ID: "abc111abc111", PosterPath: "/p.jpg"},
	})
	first := env.hits.Load()
	if first != 2 {
		t.Fatalf("first pass: want 2 hits, got %d", first)
	}
	// Second pass over the same rows must not touch upstream.
	env.posters.WarmMovies(context.Background(), []storage.MediaRow{
		{ID: "abc111abc111", PosterPath: "/p.jpg"},
	})
	if got := env.hits.Load(); got != first {
		t.Errorf("second pass added %d hits; want 0", got-first)
	}
}

func TestWarmMovies_BestEffortOnUpstreamError(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Upstream that always 500s — warm should not panic, should not abort the
	// pass, and should leave no cache file behind for the failed download.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	root := filepath.Join(t.TempDir(), "posters")
	cache := NewPosterCache(root, store)
	cache.SetImageBaseURL(failing.URL)

	cache.WarmMovies(context.Background(), []storage.MediaRow{
		{ID: "boombooombo1", PosterPath: "/p.jpg"},
	})
	// No cache file expected; the failed fetch must not crash the test.
	if _, err := os.Stat(filepath.Join(root, "boombooombo1_poster_w500.jpg")); !os.IsNotExist(err) {
		t.Errorf("expected no cache file after upstream 500, got err=%v", err)
	}
}

func TestWarmSeries_FetchesPosterAndBackdrop(t *testing.T) {
	env := newWarmEnv(t)
	env.series.WarmSeries(context.Background(), []storage.SeriesRow{
		{ID: "ser111ser111", PosterPath: "/p.jpg", BackdropPath: "/b.jpg"},
	})
	// poster: w500 + w780 (2) + backdrop: w1280 (1) = 3 hits.
	if got := env.hits.Load(); got != 3 {
		t.Errorf("upstream hits: want 3, got %d", got)
	}
	// SeriesPosterCache stores under root/series/.
	seriesRoot := filepath.Join(env.root, "series")
	for _, want := range []string{
		"ser111ser111_poster_w500.jpg",
		"ser111ser111_poster_w780.jpg",
		"ser111ser111_backdrop_w1280.jpg",
	} {
		if _, err := os.Stat(filepath.Join(seriesRoot, want)); err != nil {
			t.Errorf("expected cache file %s: %v", want, err)
		}
	}
}

func TestWarmEpisodes_FetchesStillUnderBackdropKind(t *testing.T) {
	env := newWarmEnv(t)
	env.episodes.WarmEpisodes(context.Background(), []storage.EpisodeRow{
		{ID: "eps111eps111", StillPath: "/s.jpg"},
		{ID: "epsnostilo1" /* no still */},
	})
	// Only the row with StillPath fetches → 1 hit (w780 only).
	if got := env.hits.Load(); got != 1 {
		t.Errorf("upstream hits: want 1, got %d", got)
	}
	// EpisodeStillCache stores under root/episodes/; cache file uses "backdrop"
	// kind because endpoint defaults to type=backdrop.
	wantFile := filepath.Join(env.root, "episodes", "eps111eps111_backdrop_w780.jpg")
	if _, err := os.Stat(wantFile); err != nil {
		t.Errorf("expected cache file %s: %v", wantFile, err)
	}
}

func TestWarmMovies_EmptyInputIsNoOp(t *testing.T) {
	env := newWarmEnv(t)
	env.posters.WarmMovies(context.Background(), nil)
	if got := env.hits.Load(); got != 0 {
		t.Errorf("upstream hits: want 0, got %d", got)
	}
}
