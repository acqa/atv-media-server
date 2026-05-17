package library

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/atv-media-server/server/internal/metadata"
	"github.com/atv-media-server/server/internal/transcoder"
)

// fakeTVTMDb returns canned TV + episode responses and counts calls so tests
// can assert the cache short-circuits TMDb traffic on reruns.
type fakeTVTMDb struct {
	tv       map[string]metadata.TVResult
	episodes map[int]metadata.EpisodeResult // keyed by season*100+episode for the single show in tests
	tvCalls  atomic.Int32
	epCalls  atomic.Int32
}

func (f *fakeTVTMDb) SearchTV(_ context.Context, query string, _ int) (metadata.TVResult, bool, error) {
	f.tvCalls.Add(1)
	hit, ok := f.tv[query]
	return hit, ok, nil
}

func (f *fakeTVTMDb) GetEpisode(_ context.Context, _, season, episode int) (metadata.EpisodeResult, bool, error) {
	f.epCalls.Add(1)
	ep, ok := f.episodes[season*100+episode]
	return ep, ok, nil
}

func TestScanSeriesAndUpsert_PopulatesSeriesAndEpisodes(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Breaking Bad/Season 1/S01E01.mkv",
		"Breaking Bad/Season 1/S01E02.mkv",
		"Breaking Bad/Season 2/S02E01.mkv",
	})
	store := openStore(t)
	tmdb := &fakeTVTMDb{
		tv: map[string]metadata.TVResult{
			"Breaking Bad": {TMDbID: 1396, Title: "Breaking Bad", Year: 2008, PosterPath: "/p.jpg"},
		},
		episodes: map[int]metadata.EpisodeResult{
			101: {Name: "Pilot", Description: "Walter...", StillPath: "/s1.jpg"},
			102: {Name: "Cat's in the Bag", StillPath: "/s2.jpg"},
			201: {Name: "Seven Thirty-Seven", StillPath: "/s3.jpg"},
		},
	}
	prober := &stubProber{info: transcoder.StreamInfo{VideoCodec: "h264", AudioCodec: "aac", DurationSec: 2700}}

	res, err := ScanSeriesAndUpsert(context.Background(), root, store, tmdb, prober, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Series != 1 || res.Episodes != 3 || res.Matched != 1 || res.Probed != 3 {
		t.Errorf("result: %+v", res)
	}

	all, _ := store.ListSeries()
	if len(all) != 1 || all[0].Title != "Breaking Bad" || all[0].TMDbID != 1396 {
		t.Fatalf("series: %+v", all)
	}
	eps, _ := store.ListEpisodesBySeries(all[0].ID)
	if len(eps) != 3 {
		t.Fatalf("episodes: %d", len(eps))
	}
	// Episode order: S1E1, S1E2, S2E1.
	if eps[0].Title != "Pilot" || eps[0].StillPath != "/s1.jpg" {
		t.Errorf("ep1: %+v", eps[0])
	}
	if eps[2].Season != 2 || eps[2].Episode != 1 || eps[2].Title != "Seven Thirty-Seven" {
		t.Errorf("ep3: %+v", eps[2])
	}
	for _, e := range eps {
		if e.VideoCodec != "h264" || e.AudioCodec != "aac" || e.Duration != 2700 {
			t.Errorf("probe not applied to %s: %+v", e.ID, e)
		}
	}
}

func TestScanSeriesAndUpsert_NoTMDb(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{"Show (2020)/Season 1/S01E01.mkv"})
	store := openStore(t)
	res, err := ScanSeriesAndUpsert(context.Background(), root, store, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Series != 1 || res.Episodes != 1 || res.Matched != 0 {
		t.Errorf("result: %+v", res)
	}
	all, _ := store.ListSeries()
	if all[0].Title != "Show" || all[0].Year != 2020 {
		t.Errorf("series without TMDb: %+v", all)
	}
}

func TestScanSeriesAndUpsert_SkipsUnchanged(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{"Show/Season 1/S01E01.mkv"})
	store := openStore(t)
	tmdb := &fakeTVTMDb{
		tv:       map[string]metadata.TVResult{"Show": {TMDbID: 1, Title: "Show"}},
		episodes: map[int]metadata.EpisodeResult{101: {Name: "Pilot"}},
	}
	prober := &stubProber{info: transcoder.StreamInfo{VideoCodec: "h264", AudioCodec: "aac"}}

	if _, err := ScanSeriesAndUpsert(context.Background(), root, store, tmdb, prober, nil); err != nil {
		t.Fatal(err)
	}
	firstProbes := prober.calls.Load()
	firstTV := tmdb.tvCalls.Load()
	firstEp := tmdb.epCalls.Load()

	// Second run on unchanged tree skips episode re-probe.
	if _, err := ScanSeriesAndUpsert(context.Background(), root, store, tmdb, prober, nil); err != nil {
		t.Fatal(err)
	}
	if prober.calls.Load() != firstProbes {
		t.Errorf("re-probed unchanged: %d -> %d", firstProbes, prober.calls.Load())
	}
	// Cached TMDb rows must short-circuit both SearchTV and GetEpisode so reruns
	// on a network where TMDb is blocked don't keep banging on the API.
	if tmdb.tvCalls.Load() != firstTV {
		t.Errorf("re-queried SearchTV: %d -> %d", firstTV, tmdb.tvCalls.Load())
	}
	if tmdb.epCalls.Load() != firstEp {
		t.Errorf("re-queried GetEpisode: %d -> %d", firstEp, tmdb.epCalls.Load())
	}
}
