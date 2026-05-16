package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atv-media-server/server/internal/metadata"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

// fakeTMDb returns canned responses by title and counts invocations.
type fakeTMDb struct {
	calls   atomic.Int32
	results map[string]metadata.MovieResult
	err     error
}

func (f *fakeTMDb) SearchMovie(ctx context.Context, query string, year int) (metadata.MovieResult, bool, error) {
	f.calls.Add(1)
	if f.err != nil {
		return metadata.MovieResult{}, false, f.err
	}
	hit, ok := f.results[query]
	return hit, ok, nil
}

// stubProber returns a fixed StreamInfo and counts calls.
type stubProber struct {
	calls atomic.Int32
	info  transcoder.StreamInfo
}

func (s *stubProber) Probe(ctx context.Context, path string) (transcoder.StreamInfo, error) {
	s.calls.Add(1)
	return s.info, nil
}

func openStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mkVideos(t *testing.T, root string, files []string) {
	t.Helper()
	for _, rel := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanAndUpsert_PopulatesStore(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{
		"The Matrix (1999)/m.mkv",
		"Inception (2010)/i.mp4",
	})
	store := openStore(t)
	tmdb := &fakeTMDb{results: map[string]metadata.MovieResult{
		"The Matrix": {TMDbID: 603, Title: "The Matrix", Year: 1999, Description: "Life", PosterPath: "/p.jpg", Rating: 8.2},
	}}

	res, err := ScanAndUpsert(context.Background(), root, store, tmdb, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndUpsert: %v", err)
	}
	if res.Total != 2 || res.Matched != 1 {
		t.Errorf("result: %+v", res)
	}

	// Matched row uses TMDb fields.
	all, _ := store.ListMovies()
	if len(all) != 2 {
		t.Fatalf("want 2 rows, got %d", len(all))
	}
	var matrix *storage.MediaRow
	for i, r := range all {
		if r.TMDbID == 603 {
			matrix = &all[i]
		}
	}
	if matrix == nil {
		t.Fatal("Matrix row missing TMDbID")
	}
	if matrix.Title != "The Matrix" || matrix.PosterPath != "/p.jpg" || matrix.Description != "Life" {
		t.Errorf("Matrix metadata not applied: %+v", matrix)
	}
}

func TestScanAndUpsert_NoTMDbStillStores(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{"X (2020)/x.mp4"})
	store := openStore(t)

	res, err := ScanAndUpsert(context.Background(), root, store, nil, nil, nil)
	if err != nil {
		t.Fatalf("ScanAndUpsert: %v", err)
	}
	if res.Total != 1 || res.Matched != 0 {
		t.Errorf("result: %+v", res)
	}
	all, _ := store.ListMovies()
	if len(all) != 1 || all[0].Title != "X" || all[0].Year != 2020 || all[0].TMDbID != 0 {
		t.Errorf("row: %+v", all)
	}
}

func TestScanAndUpsert_IdempotentSkipsKnown(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{"The Matrix (1999)/m.mkv"})
	store := openStore(t)
	tmdb := &fakeTMDb{results: map[string]metadata.MovieResult{
		"The Matrix": {TMDbID: 603, Title: "The Matrix", Year: 1999, PosterPath: "/p.jpg"},
	}}
	prober := &stubProber{info: transcoder.StreamInfo{VideoCodec: "h264", AudioCodec: "aac", DurationSec: 9000}}

	// Ensure the file's mtime is well in the past so the first run yields
	// updated_at > mtime, then the second run skips it.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "The Matrix (1999)/m.mkv"), past, past); err != nil {
		t.Fatal(err)
	}

	if _, err := ScanAndUpsert(context.Background(), root, store, tmdb, prober, nil); err != nil {
		t.Fatal(err)
	}
	firstCalls := tmdb.calls.Load()
	if firstCalls != 1 {
		t.Fatalf("first run: expected 1 TMDb call, got %d", firstCalls)
	}
	if prober.calls.Load() != 1 {
		t.Fatalf("first run: expected 1 probe call, got %d", prober.calls.Load())
	}

	// Second run on unchanged tree.
	res, err := ScanAndUpsert(context.Background(), root, store, tmdb, prober, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Errorf("expected 1 skipped on second run, got %+v", res)
	}
	if tmdb.calls.Load() != firstCalls {
		t.Errorf("second run hit TMDb again: %d total calls", tmdb.calls.Load())
	}
	if prober.calls.Load() != 1 {
		t.Errorf("second run hit probe again: %d total calls", prober.calls.Load())
	}
}

func TestScanAndUpsert_RescansWhenFileNewer(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "M (2000)/m.mkv")
	mkVideos(t, root, []string{"M (2000)/m.mkv"})
	store := openStore(t)
	tmdb := &fakeTMDb{results: map[string]metadata.MovieResult{
		"M": {TMDbID: 1, Title: "M"},
	}}
	prober := &stubProber{info: transcoder.StreamInfo{VideoCodec: "h264", AudioCodec: "aac"}}

	past := time.Now().Add(-time.Hour)
	os.Chtimes(file, past, past)
	if _, err := ScanAndUpsert(context.Background(), root, store, tmdb, prober, nil); err != nil {
		t.Fatal(err)
	}

	// Touch the file forward in time.
	future := time.Now().Add(time.Hour)
	os.Chtimes(file, future, future)

	res, err := ScanAndUpsert(context.Background(), root, store, tmdb, prober, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 0 {
		t.Errorf("expected rescan, got skipped=%d", res.Skipped)
	}
	if tmdb.calls.Load() != 2 {
		t.Errorf("expected 2 TMDb calls (rescan), got %d", tmdb.calls.Load())
	}
}

func TestScanAndUpsert_ProbeFillsCodecsAndNeedsTranscode(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{"hevc-movie/h.mkv", "h264-movie/x.mp4"})
	store := openStore(t)
	probers := map[string]transcoder.StreamInfo{
		"hevc-movie": {VideoCodec: "hevc", VideoProfile: "Main 10", VideoLevel: 51, AudioCodec: "dts", DurationSec: 3600},
		"h264-movie": {VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: "aac", DurationSec: 1800},
	}
	prober := proberFunc(func(ctx context.Context, path string) (transcoder.StreamInfo, error) {
		base := filepath.Base(filepath.Dir(path))
		return probers[base], nil
	})

	res, err := ScanAndUpsert(context.Background(), root, store, nil, prober, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Probed != 2 {
		t.Errorf("Probed: want 2, got %d", res.Probed)
	}

	all, _ := store.ListMovies()
	byTitle := map[string]storage.MediaRow{}
	for _, r := range all {
		byTitle[r.Title] = r
	}
	hevc := byTitle["hevc-movie"]
	if hevc.VideoCodec != "hevc" || hevc.AudioCodec != "dts" || !hevc.NeedsTranscode || hevc.Duration != 3600 {
		t.Errorf("hevc row: %+v", hevc)
	}
	h264 := byTitle["h264-movie"]
	if h264.VideoCodec != "h264" || h264.AudioCodec != "aac" || h264.NeedsTranscode || h264.Duration != 1800 {
		t.Errorf("h264 row: %+v", h264)
	}
}

// proberFunc adapts a function to transcoder.Prober for table-driven tests.
type proberFunc func(ctx context.Context, path string) (transcoder.StreamInfo, error)

func (f proberFunc) Probe(ctx context.Context, path string) (transcoder.StreamInfo, error) {
	return f(ctx, path)
}

func TestScanAndUpsert_ContextCancel(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{"A/a.mp4", "B/b.mp4", "C/c.mp4"})
	store := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ScanAndUpsert(ctx, root, store, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

func TestScanAndUpsert_TMDbErrorDoesNotStopPipeline(t *testing.T) {
	root := t.TempDir()
	mkVideos(t, root, []string{"X/x.mp4"})
	store := openStore(t)
	tmdb := &fakeTMDb{err: errors.New("network")}

	res, err := ScanAndUpsert(context.Background(), root, store, tmdb, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Matched != 0 {
		t.Errorf("result: %+v", res)
	}
	all, _ := store.ListMovies()
	if len(all) != 1 {
		t.Errorf("row not written on TMDb error: %+v", all)
	}
}
