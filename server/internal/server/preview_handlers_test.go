package server

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/atv-media-server/server/internal/storage"
)

func TestPreviewMoviesHandler_EmptyLibrary(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/preview-movies.xml")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	// Skeleton must render even when empty — ATV3 needs a valid <paradePreview>.
	if !strings.Contains(s, "<paradePreview") {
		t.Errorf("expected <paradePreview> skeleton:\n%s", s)
	}
	if strings.Contains(s, "<image>") {
		t.Errorf("expected no <image> elements with empty library:\n%s", s)
	}
}

func TestPreviewMoviesHandler_OrdersByUpdatedAtDesc(t *testing.T) {
	env := newTestEnv(t)
	// Three movies, updated_at in known order; oldest must come last.
	now := time.Now().UTC()
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "oldold000111", Title: "Old", PosterPath: "/p.jpg",
		UpdatedAt: now.Add(-2 * time.Hour),
	})
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "midmid000111", Title: "Mid", PosterPath: "/p.jpg",
		UpdatedAt: now.Add(-1 * time.Hour),
	})
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "newnew000111", Title: "New", PosterPath: "/p.jpg",
		UpdatedAt: now,
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/preview-movies.xml")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	// All three must be present.
	for _, id := range []string{"newnew000111", "midmid000111", "oldold000111"} {
		if !strings.Contains(s, "/poster/"+id+".jpg?type=poster&amp;size=w500") {
			t.Errorf("missing poster URL for %s:\n%s", id, s)
		}
	}
	// Verify ordering: newer ID must appear before older one in raw body.
	iNew := strings.Index(s, "newnew000111")
	iMid := strings.Index(s, "midmid000111")
	iOld := strings.Index(s, "oldold000111")
	if iNew >= iMid || iMid >= iOld {
		t.Errorf("expected newest-first order, got positions new=%d mid=%d old=%d", iNew, iMid, iOld)
	}
}

func TestPreviewMoviesHandler_SkipsRowsWithoutArt(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "withart00111", Title: "With art", PosterPath: "/p.jpg",
	})
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "noart0000111", Title: "No art", // no poster/backdrop
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/preview-movies.xml")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "/poster/withart00111.jpg") {
		t.Errorf("expected poster URL for row with art:\n%s", s)
	}
	if strings.Contains(s, "/poster/noart0000111.jpg") {
		t.Errorf("did not expect URL for row without poster/backdrop:\n%s", s)
	}
}

func TestPreviewSeriesHandler_OrdersByUpdatedAtDesc(t *testing.T) {
	env := newTestEnv(t)
	now := time.Now().UTC()
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: "oldshowabc12", Title: "Old", PosterPath: "/p.jpg",
		UpdatedAt: now.Add(-1 * time.Hour),
	})
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: "newshowabc12", Title: "New", PosterPath: "/p.jpg",
		UpdatedAt: now,
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/preview-series.xml")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	if !strings.Contains(s, "/series-poster/newshowabc12.jpg?type=poster&amp;size=w500") {
		t.Errorf("missing series poster URL:\n%s", s)
	}
	iNew := strings.Index(s, "newshowabc12")
	iOld := strings.Index(s, "oldshowabc12")
	if iNew >= iOld {
		t.Errorf("expected newest series first; got new=%d old=%d", iNew, iOld)
	}
}

func TestMainHandler_LinksToPreviewEndpoints(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{
		"<preview>",
		"<link>https://appletv.redbull.tv/preview-movies.xml</link>",
		"<link>https://appletv.redbull.tv/preview-series.xml</link>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in main.xml:\n%s", want, s)
		}
	}
}
