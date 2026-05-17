package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
)

// warmConcurrency caps in-flight downloads during a warm pass. TMDb has
// started returning 403 under heavy parallelism in field logs, so keep it
// modest. ensureCached() already coalesces duplicate requests inside this
// process.
const warmConcurrency = 8

// warmTask is one prefetch unit: download `sourceURL` into `cacheFile` unless
// the file already exists and is non-empty.
type warmTask struct {
	cacheFile string
	sourceURL string
}

// WarmMovies prefetches the poster sizes that movies.xml (w500) and movie.xml
// (w780) request. Rows with no PosterPath are skipped — the templates fall
// back to <defaultImage> for those.
//
// Errors are logged as warnings and never abort the pass; this is best-effort
// prefetching for offline-after-first-run usage.
func (p *PosterCache) WarmMovies(ctx context.Context, rows []storage.MediaRow) {
	if len(rows) == 0 {
		return
	}
	var tasks []warmTask
	for _, m := range rows {
		if m.PosterPath == "" {
			continue
		}
		for _, sz := range []string{"w500", "w780"} {
			tasks = append(tasks, p.task(m.ID, m.PosterPath, "poster", sz))
		}
	}
	p.runWarm(ctx, tasks, "movies")
}

// WarmSeries prefetches poster sizes that series.xml (w500) and show.xml (w780)
// request, plus the w1280 backdrop used as the fanart header in show.xml.
func (s *SeriesPosterCache) WarmSeries(ctx context.Context, rows []storage.SeriesRow) {
	if len(rows) == 0 {
		return
	}
	var tasks []warmTask
	for _, r := range rows {
		if r.PosterPath != "" {
			for _, sz := range []string{"w500", "w780"} {
				tasks = append(tasks, s.task(r.ID, r.PosterPath, "poster", sz))
			}
		}
		if r.BackdropPath != "" {
			tasks = append(tasks, s.task(r.ID, r.BackdropPath, "backdrop", "w1280"))
		}
	}
	s.runWarm(ctx, tasks, "series")
}

// WarmEpisodes prefetches the still size used by season.xml and episode.xml
// (w780). Episode stills don't distinguish poster/backdrop in our resolver:
// the endpoint defaults to type=backdrop, so cache files are written with the
// _backdrop_ kind. Mirror that here to keep on-disk names aligned.
func (s *EpisodeStillCache) WarmEpisodes(ctx context.Context, rows []storage.EpisodeRow) {
	if len(rows) == 0 {
		return
	}
	var tasks []warmTask
	for _, e := range rows {
		if e.StillPath == "" {
			continue
		}
		tasks = append(tasks, s.task(e.ID, e.StillPath, "backdrop", "w780"))
	}
	s.runWarm(ctx, tasks, "episodes")
}

// task builds a warmTask for a single (id, kind, size) tuple. Kept as a method
// on PosterCache so the embedded SeriesPosterCache/EpisodeStillCache inherit
// it for free.
func (p *PosterCache) task(id, relPath, kind, size string) warmTask {
	if !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}
	return warmTask{
		cacheFile: filepath.Join(p.root, id+"_"+kind+"_"+size+".jpg"),
		sourceURL: p.imageBaseURL + "/" + size + relPath,
	}
}

// runWarm fans tasks out to warmConcurrency goroutines. Already-cached files
// are short-circuited via os.Stat before the goroutine is even spawned, so a
// no-op warm over a fully-cached library is cheap.
func (p *PosterCache) runWarm(ctx context.Context, tasks []warmTask, label string) {
	if len(tasks) == 0 {
		return
	}
	var (
		skipped int
		queued  int
		failed  int
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, warmConcurrency)
	)
	for _, t := range tasks {
		if info, err := os.Stat(t.cacheFile); err == nil && info.Size() > 0 {
			skipped++
			continue
		}
		queued++
		wg.Add(1)
		sem <- struct{}{}
		go func(t warmTask) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if err := p.ensureCached(ctx, t.cacheFile, t.sourceURL); err != nil {
				logging.Warn("warm "+label+":", err)
				mu.Lock()
				failed++
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	logging.Info(fmt.Sprintf("warm %s: skipped=%d fetched=%d failed=%d", label, skipped, queued-failed, failed))
}
