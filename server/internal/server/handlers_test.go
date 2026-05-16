package server

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/config"
	"github.com/atv-media-server/server/internal/storage"
)

// stubPreparer simulates a successful remux by dropping a playlist on disk.
type stubPreparer struct {
	dataDir   string
	failNext  bool
	callsByID map[string]int
}

func (s *stubPreparer) PrepareHLS(_ context.Context, _, outDir string, _ int) error {
	if s.callsByID == nil {
		s.callsByID = make(map[string]int)
	}
	s.callsByID[filepath.Base(outDir)]++
	if s.failNext {
		return io.ErrUnexpectedEOF
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "playlist.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "000.ts"), []byte("seg"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, ".done"), nil, 0o644); err != nil {
		return err
	}
	return nil
}

type testEnv struct {
	mux   *http.ServeMux
	store *storage.Store
	prep  *stubPreparer
	cfg   *config.Config
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dataDir := t.TempDir()
	cfg := &config.Config{
		HTTPPort: "80", HTTPSPort: "443", CertDir: "/certs",
		BaseHost: "appletv.redbull.tv", DataDir: dataDir,
	}
	store, err := storage.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	gen := appletv.New(cfg.BaseHost)
	prep := &stubPreparer{dataDir: dataDir}
	mux := buildMux(cfg, gen, Deps{Store: store, Preparer: prep})
	return &testEnv{mux: mux, store: store, prep: prep, cfg: cfg}
}

func upsertMovie(t *testing.T, store *storage.Store, m storage.MediaRow) {
	t.Helper()
	if m.Type == "" {
		m.Type = "movie"
	}
	if m.Path == "" {
		m.Path = "/m/" + m.ID + ".mkv"
	}
	if err := store.UpsertMedia(m); err != nil {
		t.Fatalf("UpsertMedia: %v", err)
	}
}

func TestMoviesHandler_EmptyLibrary(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/movies.xml")
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

func TestMoviesHandler_PopulatedListsMovies(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{ID: "abc123def456", Title: "The Matrix", Year: 1999, BackdropPath: "/b.jpg", PosterPath: "/p.jpg"})
	upsertMovie(t, env.store, storage.MediaRow{ID: "fedcba987654", Title: "Inception", Year: 2010})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/movies.xml")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	if !strings.Contains(s, "<moviePoster") {
		t.Errorf("expected <moviePoster> elements in grid:\n%s", s)
	}
	if !strings.Contains(s, `id="movie-abc123def456"`) ||
		!strings.Contains(s, "<title>The Matrix</title>") ||
		!strings.Contains(s, "<subtitle>1999</subtitle>") {
		t.Errorf("missing Matrix poster:\n%s", s)
	}
	if !strings.Contains(s, `id="movie-fedcba987654"`) ||
		!strings.Contains(s, "<title>Inception</title>") ||
		!strings.Contains(s, "<subtitle>2010</subtitle>") {
		t.Errorf("missing Inception poster:\n%s", s)
	}
	// onSelect drives navigation to /movie.xml; onPlay starts playback directly.
	if !strings.Contains(s, "/movie.xml?id=abc123def456") {
		t.Errorf("missing onSelect link for Matrix:\n%s", s)
	}
	if !strings.Contains(s, "/play.xml?id=abc123def456") {
		t.Errorf("missing onPlay link for Matrix:\n%s", s)
	}
	// Poster URL only rendered when the row has poster/backdrop set.
	if !strings.Contains(s, "/poster/abc123def456.jpg?type=poster&amp;size=w500") {
		t.Errorf("missing poster URL for Matrix (which has poster_path set):\n%s", s)
	}
	// Inception has no poster_path/backdrop_path — no <image> tag, only <defaultImage>.
	if strings.Contains(s, "/poster/fedcba987654.jpg") {
		t.Errorf("unexpected poster URL for Inception (no poster_path):\n%s", s)
	}
}

func TestMovieHandler_NotFound(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/movie.xml?id=nope")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Not Found") {
		t.Errorf("expected Not Found dialog:\n%s", body)
	}
}

func TestMovieHandler_RendersFullDetails(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "xyz000abc111", Title: "Blade Runner", Year: 1982,
		Description: "Detective hunts replicants", PosterPath: "/p.jpg", Rating: 8.1,
		Duration: 7020, VideoCodec: "h264", AudioCodec: "ac3",
	})
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/movie.xml?id=xyz000abc111")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{
		"<itemDetail",
		"Blade Runner",
		"(1982)",
		"<summary>Detective hunts replicants</summary>",
		`<image style="moviePoster">`,
		"/poster/xyz000abc111.jpg?type=poster&amp;size=w780",
		"<label>1h 57m</label>",
		"<label>H.264 · AC3</label>",
		"<starRating>",
		"<percentage>81</percentage>",
		`<actionButton`,
		`id="play-xyz000abc111-a0"`,
		"/play.xml?id=xyz000abc111&amp;audio=0",
		"<title>Play</title>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in body:\n%s", want, s)
		}
	}
}

func TestMovieHandler_NoDescriptionFallback(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{ID: "abcabcabc111", Title: "Bare"})
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/movie.xml?id=abcabcabc111")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "Description not found") {
		t.Errorf("missing fallback description:\n%s", body)
	}
	// Bare row: no poster, no duration, no codecs, no rating — table and image must be skipped.
	if strings.Contains(s, "<table>") {
		t.Errorf("expected no <table> when no duration/quality/rating:\n%s", s)
	}
	if strings.Contains(s, `<image style="moviePoster">`) {
		t.Errorf("expected no poster <image> for row without poster_path:\n%s", s)
	}
	if !strings.Contains(s, "<defaultImage>resource://Poster.png</defaultImage>") {
		t.Errorf("expected <defaultImage> fallback:\n%s", s)
	}
}

func TestMovieHandler_MultipleAudioTracks(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{
		ID: "aud123aud456", Title: "Multi", AudioCount: 3,
	})
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/movie.xml?id=aud123aud456")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	s := string(body)
	for _, want := range []string{
		`columnCount="3"`,
		`id="play-aud123aud456-a0"`,
		`id="play-aud123aud456-a1"`,
		`id="play-aud123aud456-a2"`,
		"/play.xml?id=aud123aud456&amp;audio=0",
		"/play.xml?id=aud123aud456&amp;audio=1",
		"/play.xml?id=aud123aud456&amp;audio=2",
		"<title>Audio 0</title>",
		"<title>Audio 1</title>",
		"<title>Audio 2</title>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in body:\n%s", want, s)
		}
	}
	// With multiple tracks, the single-track "Play" label must not appear.
	if strings.Contains(s, "<title>Play</title>") {
		t.Errorf("unexpected single-track Play label with multiple audio:\n%s", s)
	}
}

func TestPlayHandler_RendersPlayerWithoutPreparing(t *testing.T) {
	env := newTestEnv(t)
	const id = "a1b2c3d4e5f6"
	upsertMovie(t, env.store, storage.MediaRow{ID: id, Title: "Sample"})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/play.xml?id=" + id)
	if err != nil {
		t.Fatalf("GET play.xml: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, new(interface{})); err != nil {
		t.Fatalf("invalid XML: %v\n%s", err, body)
	}
	// HLS prep is deferred to streamHandler so play.xml stays under ATV3's
	// XML load timeout (~30s).
	if env.prep.callsByID[id] != 0 {
		t.Errorf("expected zero prepare calls from play.xml, got %d", env.prep.callsByID[id])
	}
	if !strings.Contains(string(body), "/stream/"+id+"/playlist.m3u8") {
		t.Errorf("body missing stream URL:\n%s", body)
	}
}

func TestStreamHandler_PlaylistOK(t *testing.T) {
	env := newTestEnv(t)
	const id = "1234567890ab"
	upsertMovie(t, env.store, storage.MediaRow{ID: id, Title: "S"})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/stream/" + id + "/playlist.m3u8")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	// outDir now carries an __aN audio suffix (default __a0 when ?a is absent).
	if env.prep.callsByID[id+"__a0"] != 1 {
		t.Errorf("expected one prepare call for %s__a0, got %d", id, env.prep.callsByID[id+"__a0"])
	}
}

func TestStreamHandler_SegmentOK(t *testing.T) {
	env := newTestEnv(t)
	const id = "abcdef012345"
	upsertMovie(t, env.store, storage.MediaRow{ID: id, Title: "S"})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	if _, err := http.Get(srv.URL + "/stream/" + id + "/playlist.m3u8"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/stream/" + id + "/000.ts")
	if err != nil {
		t.Fatalf("GET seg: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seg status: want 200, got %d", resp.StatusCode)
	}
}

func TestStreamHandler_UnknownID(t *testing.T) {
	env := newTestEnv(t)
	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	resp, _ := http.Get(srv.URL + "/stream/aaaaaaaaaaaa/playlist.m3u8")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestStreamHandler_RejectsBadInputs(t *testing.T) {
	env := newTestEnv(t)
	const id = "feeddeadbeef"
	upsertMovie(t, env.store, storage.MediaRow{ID: id, Title: "S"})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)
	cases := []struct {
		path     string
		wantCode int
	}{
		{"/stream/SHOUTING1234/playlist.m3u8", http.StatusBadRequest},
		{"/stream/short/playlist.m3u8", http.StatusBadRequest},
		{"/stream/" + id + "/playlist.M3U8", http.StatusBadRequest},
		{"/stream/" + id + "/segment.txt", http.StatusBadRequest},
		{"/stream/" + id + "/000.ts/foo", http.StatusBadRequest},
		{"/stream/" + id + "/0000.ts", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + c.path)
			if err != nil {
				t.Fatalf("GET %s: %v", c.path, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != c.wantCode {
				t.Errorf("path %s: want %d, got %d", c.path, c.wantCode, resp.StatusCode)
			}
		})
	}
}

func TestStreamHandler_TraversalBlockedAtHandler(t *testing.T) {
	env := newTestEnv(t)
	const id = "feeddeadbeef"
	upsertMovie(t, env.store, storage.MediaRow{ID: id, Title: "S"})

	for _, raw := range []string{
		"/stream/" + id + "/../../etc/passwd",
		"/stream/" + id + "/..%2f..%2fetc%2fpasswd",
		"/stream/" + id + "/000.ts/../001.ts",
	} {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://x"+raw, nil)
			req.URL.Path = raw
			req.URL.RawPath = raw
			rec := httptest.NewRecorder()
			env.mux.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				t.Errorf("expected non-200 for %q, got %d", raw, rec.Code)
			}
		})
	}
}

func TestSearchResultsHandler_FiltersByTerm(t *testing.T) {
	env := newTestEnv(t)
	upsertMovie(t, env.store, storage.MediaRow{ID: "aaaaaaaaaaaa", Title: "The Matrix", Year: 1999})
	upsertMovie(t, env.store, storage.MediaRow{ID: "bbbbbbbbbbbb", Title: "Inception", Year: 2010})

	srv := httptest.NewServer(env.mux)
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL + "/search-results.xml?term=matrix")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "The Matrix") {
		t.Errorf("missing matched movie:\n%s", s)
	}
	if strings.Contains(s, "Inception") {
		t.Errorf("term should have excluded Inception:\n%s", s)
	}
}
