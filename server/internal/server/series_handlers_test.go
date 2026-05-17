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
	if !strings.Contains(s, "/art-series/ser111aaa222.jpg?type=poster&amp;size=w500") {
		t.Errorf("missing poster URL for Breaking Bad:\n%s", s)
	}
	// The Wire has no poster_path/backdrop_path — no <image> tag.
	if strings.Contains(s, "/art-series/ser333bbb444.jpg") {
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
		"<itemDetail",
		`<image style="moviePoster">`,
		"/art-series/" + id + ".jpg?type=poster&amp;size=w780",
		"Breaking Bad",
		"(2008)",
		"<summary>Chemistry teacher turns drug lord</summary>",
		"<starRating>",
		"<percentage>95</percentage>",
		"<bottomShelf>",
		`columnCount="5"`, // fixed grid: 2 seasons still get 5-column shelf, tiles 1/5 wide
		"<actionButton",
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
	// <itemDetailWithImageHeader> + <imageHeader> are intentionally NOT used
	// on shows: ATV3 firmware 7.9 mis-renders the header, overlaying the
	// moviePoster <image> on top of itself. Backdrop is dropped on shows;
	// only the poster <image> is shown. See [show.xml] for the rationale.
	if strings.Contains(s, "<itemDetailWithImageHeader") {
		t.Errorf("show.xml must use <itemDetail>, not <itemDetailWithImageHeader>:\n%s", s)
	}
	if strings.Contains(s, "<imageHeader>") {
		t.Errorf("show.xml must not emit <imageHeader>:\n%s", s)
	}
	if strings.Contains(s, "type=backdrop") {
		t.Errorf("show.xml must not request the series backdrop:\n%s", s)
	}
	// Seasons go into <bottomShelf> with <actionButton> tiles. <centerShelf
	// center="true"> mis-renders when items overflow columnCount: with 5+
	// seasons the row started at the screen edge and overlapped the poster on
	// the left. <bottomShelf> handles overflow with horizontal scroll cleanly,
	// while <actionButton> keeps the compact "label inside the button" look
	// (vs. <moviePoster>'s stretched rectangle with label below).
	if strings.Contains(s, "<centerShelf>") {
		t.Errorf("show.xml must use <bottomShelf> for seasons, not <centerShelf>:\n%s", s)
	}
}

func TestSeasonHandler_RendersGridOfStills(t *testing.T) {
	env := newTestEnv(t)
	const showID = "showbb111111"
	upsertSeries(t, env.store, storage.SeriesRow{ID: showID, Title: "Breaking Bad"})
	upsertEpisode(t, env.store, storage.EpisodeRow{
		ID: "e1aaa111aaa", SeriesID: showID, Season: 1, Episode: 1,
		Title: "Pilot", StillPath: "/still1.jpg", Duration: 3000,
	})
	upsertEpisode(t, env.store, storage.EpisodeRow{
		ID: "e2bbb222bbb", SeriesID: showID, Season: 1, Episode: 2,
		Title: "Cat's in the Bag",
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/season.xml?show=" + showID + "&s=1")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{
		"<sixteenByNinePoster",
		`id="episode-e1aaa111aaa"`,
		`id="episode-e2bbb222bbb"`,
		"<title>1. Pilot</title>",
		"<title>2. Cat's in the Bag</title>",
		"<subtitle>50m</subtitle>", // Duration 3000s = 50m
		"/art-still/e1aaa111aaa.jpg?size=w780",
		"/episode.xml?id=e1aaa111aaa",
		"/play.xml?id=e1aaa111aaa",
		"<defaultImage>resource://16x9.png</defaultImage>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in body:\n%s", want, s)
		}
	}
	// Episode 2 has no still — must not emit an <image> URL for it.
	if strings.Contains(s, "/art-still/e2bbb222bbb.jpg") {
		t.Errorf("unexpected still URL for episode without StillPath:\n%s", s)
	}
}

func TestEpisodeHandler_NotFound(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/episode.xml?id=nope")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Not Found") {
		t.Errorf("expected Not Found dialog:\n%s", body)
	}
}

func TestEpisodeHandler_RendersFullDetails(t *testing.T) {
	env := newTestEnv(t)
	const showID = "showcc222222"
	const epID = "eeeppp1111aa"
	upsertSeries(t, env.store, storage.SeriesRow{ID: showID, Title: "The Wire"})
	upsertEpisode(t, env.store, storage.EpisodeRow{
		ID: epID, SeriesID: showID, Season: 1, Episode: 3,
		Title: "The Buys", Description: "Detectives plan a sting",
		StillPath: "/still.jpg", Duration: 3600,
		VideoCodec: "h264", AudioCodec: "ac3",
		VideoHeight: 1080, AudioChannels: 6,
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/episode.xml?id=" + epID)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{
		"<itemDetail",
		"The Wire — S01E03: The Buys",
		"<summary>Detectives plan a sting</summary>",
		`<image style="sixteenByNinePoster">`,
		"/art-still/" + epID + ".jpg?size=w780",
		"<label>1h 0m</label>",
		"<mediaBadges>",
		`src="https://appletv.redbull.tv/assets/badges/1080.png"`,
		`src="https://appletv.redbull.tv/assets/badges/h264.png"`,
		`src="https://appletv.redbull.tv/assets/badges/ac3.png"`,
		`src="https://appletv.redbull.tv/assets/badges/6.png"`,
		"<actionButton",
		`id="play-` + epID + `-a0"`,
		"/play.xml?id=" + epID + "&amp;audio=0",
		"<title>Play</title>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in body:\n%s", want, s)
		}
	}
}

func TestEpisodeHandler_NoDescriptionFallback(t *testing.T) {
	env := newTestEnv(t)
	const showID = "showdd333333"
	const epID = "barebareepis"
	upsertSeries(t, env.store, storage.SeriesRow{ID: showID, Title: "Plain"})
	upsertEpisode(t, env.store, storage.EpisodeRow{
		ID: epID, SeriesID: showID, Season: 2, Episode: 5,
	})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/episode.xml?id=" + epID)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "Description not found") {
		t.Errorf("missing fallback description:\n%s", s)
	}
	// No still / no codecs / no duration — <table> is skipped, but
	// <image style="sixteenByNinePoster"> MUST still be emitted (with a
	// resource:// fallback). ATV3 rejects <itemDetail> outright when the
	// <image> tag is missing.
	if !strings.Contains(s, `<image style="sixteenByNinePoster">resource://16x9.png</image>`) {
		t.Errorf("expected <image> fallback to resource://16x9.png:\n%s", s)
	}
	if strings.Contains(s, "<table>") {
		t.Errorf("unexpected <table> when no duration/quality:\n%s", s)
	}
	// Title without episode-title falls back to plain SxxExx form (no colon-suffix).
	if !strings.Contains(s, "<title>Plain — S02E05</title>") {
		t.Errorf("expected title without ': <Title>' suffix:\n%s", s)
	}
}

func TestShowHandler_SingleSeasonCentred(t *testing.T) {
	env := newTestEnv(t)
	const id = "oneseason111"
	upsertSeries(t, env.store, storage.SeriesRow{ID: id, Title: "Mini"})
	upsertEpisode(t, env.store, storage.EpisodeRow{
		ID: "epm1aaa11111", SeriesID: id, Season: 1, Episode: 1,
	})
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/show.xml?id=" + id)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	// Single-season shows keep columnCount=1 so the lone tile renders centred
	// rather than crammed into the left 1/5 of the row.
	if !strings.Contains(s, `columnCount="1"`) {
		t.Errorf("expected columnCount=1 for single-season show:\n%s", s)
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
