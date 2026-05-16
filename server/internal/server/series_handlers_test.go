package server

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atv-media-server/server/internal/storage"
)

func upsertSeries(t *testing.T, store *storage.Store, r storage.SeriesRow) {
	t.Helper()
	if r.Path == "" {
		r.Path = "/s/" + r.ID
	}
	if err := store.UpsertSeries(r); err != nil {
		t.Fatalf("UpsertSeries: %v", err)
	}
}

func TestSeriesHandler_EmptyLibrary(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/series.xml")
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
	if strings.Contains(string(body), "<moviePoster") {
		t.Errorf("empty library produced poster items:\n%s", body)
	}
}

func TestSeriesHandler_PopulatedListsSeries(t *testing.T) {
	env := newTestEnv(t)
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: "ser111aaa222", Title: "Breaking Bad", Year: 2008, PosterPath: "/p.jpg", BackdropPath: "/b.jpg",
	})
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: "ser333bbb444", Title: "The Wire", Year: 2002,
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/series.xml")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	if !strings.Contains(s, "<moviePoster") {
		t.Errorf("expected <moviePoster> elements in grid:\n%s", s)
	}
	if !strings.Contains(s, `id="series-ser111aaa222"`) ||
		!strings.Contains(s, "<title>Breaking Bad</title>") ||
		!strings.Contains(s, "<subtitle>2008</subtitle>") {
		t.Errorf("missing Breaking Bad poster:\n%s", s)
	}
	if !strings.Contains(s, `id="series-ser333bbb444"`) ||
		!strings.Contains(s, "<title>The Wire</title>") ||
		!strings.Contains(s, "<subtitle>2002</subtitle>") {
		t.Errorf("missing The Wire poster:\n%s", s)
	}
	if !strings.Contains(s, "/show.xml?id=ser111aaa222") {
		t.Errorf("missing onSelect link for Breaking Bad:\n%s", s)
	}
	// Series grid has no onPlay (ambiguous which episode) — assert it's absent.
	if strings.Contains(s, "onPlay=") {
		t.Errorf("series grid should not register onPlay:\n%s", s)
	}
	if !strings.Contains(s, "/series-poster/ser111aaa222.jpg?type=poster&amp;size=w500") {
		t.Errorf("missing poster URL for Breaking Bad:\n%s", s)
	}
	// The Wire has no poster_path/backdrop_path — no <image> tag.
	if strings.Contains(s, "/series-poster/ser333bbb444.jpg") {
		t.Errorf("unexpected poster URL for The Wire (no poster_path):\n%s", s)
	}
}
