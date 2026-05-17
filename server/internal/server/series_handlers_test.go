package server

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func upsertEpisode(t *testing.T, store *storage.Store, r storage.EpisodeRow) {
	t.Helper()
	if r.Path == "" {
		r.Path = "/e/" + r.ID + ".mkv"
	}
	if err := store.UpsertEpisode(r); err != nil {
		t.Fatalf("UpsertEpisode: %v", err)
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

func TestShowHandler_NotFound(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/show.xml?id=nope")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Not Found") {
		t.Errorf("expected Not Found dialog:\n%s", body)
	}
}

func TestShowHandler_RendersFullDetails(t *testing.T) {
	env := newTestEnv(t)
	const id = "showabc12345"
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: id, Title: "Breaking Bad", Year: 2008,
		Description: "Chemistry teacher turns drug lord",
		PosterPath:  "/p.jpg", BackdropPath: "/b.jpg", Rating: 9.5,
	})
	upsertEpisode(t, env.store, storage.EpisodeRow{ID: "e1aaa", SeriesID: id, Season: 1, Episode: 1, Title: "Pilot"})
	upsertEpisode(t, env.store, storage.EpisodeRow{ID: "e2aaa", SeriesID: id, Season: 2, Episode: 1, Title: "Seven Thirty-Seven"})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/show.xml?id=" + id)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{
		"<itemDetailWithImageHeader",
		"<imageHeader>",
		"/series-poster/" + id + ".jpg?type=backdrop&amp;size=w1280",
		`<image style="moviePoster">`,
		"/series-poster/" + id + ".jpg?type=poster&amp;size=w780",
		"Breaking Bad",
		"(2008)",
		"<summary>Chemistry teacher turns drug lord</summary>",
		"<starRating>",
		"<percentage>95</percentage>",
		"<centerShelf>",
		`columnCount="2"`, // 2 seasons → 2 columns
		`id="season-` + id + `-1"`,
		`id="season-` + id + `-2"`,
		"/season.xml?show=" + id + "&amp;s=1",
		"/season.xml?show=" + id + "&amp;s=2",
		"<title>Season 1</title>",
		"<title>Season 2</title>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in body:\n%s", want, s)
		}
	}
}

func TestShowHandler_WithoutBackdropSkipsHeader(t *testing.T) {
	env := newTestEnv(t)
	const id = "shownobkdrp1"
	upsertSeries(t, env.store, storage.SeriesRow{
		ID: id, Title: "Plain", PosterPath: "/p.jpg", // poster only, no backdrop
	})
	upsertEpisode(t, env.store, storage.EpisodeRow{ID: "e9aaa", SeriesID: id, Season: 1, Episode: 1})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/show.xml?id=" + id)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if strings.Contains(s, "<imageHeader>") {
		t.Errorf("expected no <imageHeader> when HasBackdrop=false:\n%s", s)
	}
	if strings.Contains(s, "type=backdrop") {
		t.Errorf("expected no backdrop URL when HasBackdrop=false:\n%s", s)
	}
	// Poster <image> still rendered (poster_path is set).
	if !strings.Contains(s, `<image style="moviePoster">`) {
		t.Errorf("expected poster <image> when PosterPath is set:\n%s", s)
	}
}

func TestShowHandler_SeasonColumnsCappedAtFive(t *testing.T) {
	env := newTestEnv(t)
	const id = "longshow1234"
	upsertSeries(t, env.store, storage.SeriesRow{ID: id, Title: "Long Run"})
	// 8 seasons — shelf should still render columnCount="5" for readability.
	for i := 1; i <= 8; i++ {
		upsertEpisode(t, env.store, storage.EpisodeRow{
			ID: "epis" + string(rune('a'+i-1)) + "000000", SeriesID: id, Season: i, Episode: 1,
		})
	}
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/show.xml?id=" + id)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, `columnCount="5"`) {
		t.Errorf("expected columnCount=5 when seasons>5:\n%s", s)
	}
	// All 8 season buttons must still be present (shelf scrolls horizontally).
	for i := 1; i <= 8; i++ {
		want := `id="season-` + id + `-` + strconv.Itoa(i) + `"`
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
}
