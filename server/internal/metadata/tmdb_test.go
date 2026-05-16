package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient creates a Client pointing at a httptest server.
// handler decides what each path returns.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-key")
	c.BaseURL = srv.URL
	c.BackoffMin = time.Millisecond
	return c
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestSearchMovie_ParsesFirstHit(t *testing.T) {
	var got *url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "search_matrix.json"))
	})

	res, ok, err := c.SearchMovie(context.Background(), "The Matrix", 1999)
	if err != nil {
		t.Fatalf("SearchMovie: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.TMDbID != 603 || res.Title != "The Matrix" || res.OriginalTitle != "The Matrix" {
		t.Errorf("hit fields: %+v", res)
	}
	if res.Year != 1999 {
		t.Errorf("Year: want 1999, got %d", res.Year)
	}
	if res.PosterPath != "/aOIuZEjVc73lBeFXgaWPgC2hG0L.jpg" {
		t.Errorf("PosterPath: %q", res.PosterPath)
	}
	if res.Rating < 8 || res.Rating > 9 {
		t.Errorf("Rating out of range: %v", res.Rating)
	}

	// Verify request URL.
	if got.Path != "/search/movie" {
		t.Errorf("path: %q", got.Path)
	}
	q := got.Query()
	if q.Get("query") != "The Matrix" || q.Get("year") != "1999" || q.Get("api_key") != "test-key" {
		t.Errorf("query: %v", q)
	}
	if q.Get("language") != defaultLanguage {
		t.Errorf("language: want %q, got %q", defaultLanguage, q.Get("language"))
	}
}

func TestSearchMovie_NoResults(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "search_empty.json"))
	})
	_, ok, err := c.SearchMovie(context.Background(), "Nothing", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false for empty results")
	}
}

func TestSearchMovie_NoAPIKey(t *testing.T) {
	c := New("")
	_, _, err := c.SearchMovie(context.Background(), "x", 0)
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("want ErrNoAPIKey, got %v", err)
	}
}

func TestSearchMovie_EmptyQuery(t *testing.T) {
	c := New("k")
	_, _, err := c.SearchMovie(context.Background(), "   ", 0)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestSearchMovie_NonOKReturnsError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, _, err := c.SearchMovie(context.Background(), "x", 0)
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestSearchMovie_RetryOn429(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(loadFixture(t, "search_empty.json"))
	})
	c.MaxRetries = 5

	_, _, err := c.SearchMovie(context.Background(), "x", 0)
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestSearchMovie_RetryExhausted(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.MaxRetries = 2
	_, _, err := c.SearchMovie(context.Background(), "x", 0)
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
}

func TestSearchMovie_ContextCancel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(loadFixture(t, "search_empty.json"))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := c.SearchMovie(ctx, "x", 0)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestPosterURL(t *testing.T) {
	cases := []struct {
		path, size, want string
	}{
		{"/abc.jpg", "w500", "https://image.tmdb.org/t/p/w500/abc.jpg"},
		{"abc.jpg", "w780", "https://image.tmdb.org/t/p/w780/abc.jpg"},
		{"/x.jpg", "", "https://image.tmdb.org/t/p/w500/x.jpg"},
		{"", "w500", ""},
	}
	for _, c := range cases {
		got := PosterURL(c.path, c.size)
		if got != c.want {
			t.Errorf("PosterURL(%q,%q) = %q, want %q", c.path, c.size, got, c.want)
		}
	}
}
