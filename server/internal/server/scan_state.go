package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/atv-media-server/server/internal/library"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

// ScanState exposes /api/library/scan and /api/library/status: a single global
// background scan with progress accounting. Only one scan runs at a time;
// concurrent POSTs are rejected with 409.
type ScanState struct {
	runFn func(ctx context.Context, log library.Logger) (library.ScanResult, error)

	mu       sync.Mutex
	running  bool
	started  time.Time
	finished time.Time
	result   library.ScanResult
	errMsg   string
	cancel   context.CancelFunc
}

// ScanFunc is what ScanState actually runs to perform a scan. Decoupling lets
// tests inject a fake; production wires this to library.ScanAndUpsert.
type ScanFunc func(ctx context.Context, log library.Logger) (library.ScanResult, error)

// NewScanState wires a runner. The runner must be context-aware so /api/library
// callers can interrupt a long scan (Phase 4 admin will use this).
func NewScanState(run ScanFunc) *ScanState {
	return &ScanState{runFn: run}
}

// FullTMDb is what NewScanStateForAll expects: movies + TV in one interface.
// Kept here (not in library) because only ScanState needs it composed.
type FullTMDb interface {
	library.TMDbSearcher
	library.TMDbTVSearcher
}

// NewScanStateForAll runs movies, series and music scans sequentially. The
// returned ScanResult aggregates movie counts; series/music numbers go to logs.
//
// After each scan succeeds the matching poster cache is warmed: every poster
// referenced by a row gets pre-downloaded so the server can later run in a
// network with no TMDb access. Warmers may be nil — in which case the warm
// step is silently skipped (kept for tests that don't wire up image caches).
func NewScanStateForAll(
	moviesRoot, seriesRoot, musicRoot string,
	store *storage.Store, tmdb FullTMDb, prober transcoder.Prober,
	posters *PosterCache, seriesPosters *SeriesPosterCache, episodeStills *EpisodeStillCache,
) *ScanState {
	var movieTMDb library.TMDbSearcher
	var tvTMDb library.TMDbTVSearcher
	if tmdb != nil {
		movieTMDb = tmdb
		tvTMDb = tmdb
	}
	return NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		res, err := library.ScanAndUpsert(ctx, moviesRoot, store, movieTMDb, prober, log)
		if err != nil {
			return res, err
		}
		if posters != nil {
			if rows, err := store.ListMovies(); err == nil {
				posters.WarmMovies(ctx, rows)
			}
		}
		if _, err := library.ScanSeriesAndUpsert(ctx, seriesRoot, store, tvTMDb, prober, log); err != nil {
			return res, err
		}
		if seriesPosters != nil || episodeStills != nil {
			seriesList, err := store.ListSeries()
			if err == nil {
				if seriesPosters != nil {
					seriesPosters.WarmSeries(ctx, seriesList)
				}
				if episodeStills != nil {
					var allEpisodes []storage.EpisodeRow
					for _, sr := range seriesList {
						if eps, err := store.ListEpisodesBySeries(sr.ID); err == nil {
							allEpisodes = append(allEpisodes, eps...)
						}
					}
					episodeStills.WarmEpisodes(ctx, allEpisodes)
				}
			}
		}
		if _, err := library.ScanMusicAndUpsert(ctx, musicRoot, store, log); err != nil {
			return res, err
		}
		return res, nil
	})
}

// StatusSnapshot is the payload of /api/library/status. Times are RFC3339 or
// empty strings when the scan has not yet started or finished.
type StatusSnapshot struct {
	Running  bool               `json:"running"`
	Started  string             `json:"started,omitempty"`
	Finished string             `json:"finished,omitempty"`
	Result   library.ScanResult `json:"result"`
	Error    string             `json:"error,omitempty"`
}

// Snapshot returns the current state for status reporting.
func (s *ScanState) Snapshot() StatusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := StatusSnapshot{
		Running: s.running,
		Result:  s.result,
		Error:   s.errMsg,
	}
	if !s.started.IsZero() {
		snap.Started = s.started.UTC().Format(time.RFC3339)
	}
	if !s.finished.IsZero() {
		snap.Finished = s.finished.UTC().Format(time.RFC3339)
	}
	return snap
}

// Trigger starts a scan unless one is already running. Returns false if a
// scan was already in progress (caller should respond with 409).
func (s *ScanState) Trigger() bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.started = time.Now()
	s.finished = time.Time{}
	s.result = library.ScanResult{}
	s.errMsg = ""
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		res, err := s.runFn(ctx, nil)
		s.mu.Lock()
		s.running = false
		s.finished = time.Now()
		s.result = res
		if err != nil {
			s.errMsg = err.Error()
		}
		s.cancel = nil
		s.mu.Unlock()
	}()
	return true
}

// TriggerAndWait is a test helper that triggers a scan and blocks until it
// finishes. Production callers should use Trigger.
func (s *ScanState) TriggerAndWait() StatusSnapshot {
	if !s.Trigger() {
		return s.Snapshot()
	}
	for {
		snap := s.Snapshot()
		if !snap.Running {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ScanHandler returns /api/library/scan (POST → 202 / 409).
func (s *ScanState) ScanHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.Trigger() {
			http.Error(w, "scan already running", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"started"}` + "\n"))
	}
}

// StatusHandler returns /api/library/status (GET → JSON).
func (s *ScanState) StatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	}
}
