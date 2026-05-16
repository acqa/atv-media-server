package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/atv-media-server/server/internal/admin"
	"github.com/atv-media-server/server/internal/certs"
	"github.com/atv-media-server/server/internal/config"
	"github.com/atv-media-server/server/internal/library"
	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/metadata"
	"github.com/atv-media-server/server/internal/server"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

func main() {
	showVersion := flag.Bool("v", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(config.Version)
		return
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}

	if cfg.LogToFile {
		if err := logging.EnableFile(cfg.LoggingPath); err != nil {
			fmt.Fprintln(os.Stderr, "log file:", err)
			os.Exit(2)
		}
	}

	logging.Info("starting atv-media-server", config.Version)

	if generated, err := certs.EnsureCertificate(cfg.CertDir, cfg.BaseHost); err != nil {
		logging.Fatal("ensure cert:", err)
	} else if generated {
		logging.Info("generated self-signed certificate for", cfg.BaseHost, "in", cfg.CertDir)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logging.Fatal("mkdir data dir:", err)
	}
	store, err := storage.Open(filepath.Join(cfg.DataDir, "metadata.db"))
	if err != nil {
		logging.Fatal("open store:", err)
	}
	defer store.Close()

	var tmdb *metadata.Client
	if cfg.TMDbAPIKey != "" {
		tmdb = metadata.New(cfg.TMDbAPIKey)
		logging.Info("TMDb metadata enrichment enabled")
	} else {
		logging.Info("TMDB_API_KEY empty — metadata enrichment disabled")
	}

	prober := transcoder.NewExecProber()
	pipeline := transcoder.NewPipeline()

	moviesRoot := filepath.Join(cfg.MediaPath, "movies")
	seriesRoot := filepath.Join(cfg.MediaPath, "series")
	musicRoot := filepath.Join(cfg.MediaPath, "music")

	logFn := func(f string, a ...interface{}) { logging.Info(fmt.Sprintf(f, a...)) }

	// Cast tmdb to interfaces only when non-nil; passing a typed-nil to interface
	// variables would let the pipeline call SearchMovie on a nil receiver.
	var movieTMDb library.TMDbSearcher
	var tvTMDb library.TMDbTVSearcher
	var fullTMDb server.FullTMDb
	if tmdb != nil {
		movieTMDb = tmdb
		tvTMDb = tmdb
		fullTMDb = tmdb
	}
	scanState := server.NewScanStateForAll(moviesRoot, seriesRoot, musicRoot, store, fullTMDb, prober)

	// Initial synchronous scan on startup so the catalogue is populated when
	// HTTPS comes up.
	if res, err := library.ScanAndUpsert(context.Background(), moviesRoot, store, movieTMDb, prober, logFn); err != nil {
		logging.Warn("initial movie scan failed:", err)
	} else {
		logging.Info(fmt.Sprintf("initial movie scan: total=%d matched=%d skipped=%d probed=%d",
			res.Total, res.Matched, res.Skipped, res.Probed))
	}
	if sres, err := library.ScanSeriesAndUpsert(context.Background(), seriesRoot, store, tvTMDb, prober, logFn); err != nil {
		logging.Warn("initial series scan failed:", err)
	} else {
		logging.Info(fmt.Sprintf("initial series scan: series=%d episodes=%d matched=%d probed=%d",
			sres.Series, sres.Episodes, sres.Matched, sres.Probed))
	}
	if mres, err := library.ScanMusicAndUpsert(context.Background(), musicRoot, store, logFn); err != nil {
		logging.Warn("initial music scan failed:", err)
	} else {
		logging.Info(fmt.Sprintf("initial music scan: artists=%d albums=%d tracks=%d",
			mres.Artists, mres.Albums, mres.Tracks))
	}

	posters := server.NewPosterCache(filepath.Join(cfg.DataDir, "posters"), store)
	seriesPosters := server.NewSeriesPosterCache(filepath.Join(cfg.DataDir, "posters"), store)
	episodeStills := server.NewEpisodeStillCache(filepath.Join(cfg.DataDir, "posters"), store)
	transcodedDir := filepath.Join(cfg.DataDir, "transcoded")
	maxBytes := int64(cfg.TranscodeCacheMaxGB) * 1024 * 1024 * 1024
	go startCacheGC(transcodedDir, maxBytes)

	deps := server.Deps{
		Store:         store,
		Preparer:      pipeline,
		Posters:       posters,
		SeriesPosters: seriesPosters,
		EpisodeStills: episodeStills,
		ScanState:     scanState,
	}

	go func() {
		err := admin.Serve(admin.Config{
			User: cfg.AdminUser, Pass: cfg.AdminPass, Port: cfg.AdminPort,
		}, admin.Deps{Store: store, ScanState: scanState})
		if err != nil {
			logging.Warn("admin:", err)
		}
	}()

	if err := server.Serve(cfg, deps); err != nil {
		logging.Fatal(err)
	}
}

// startCacheGC runs an LRU sweep on transcodedDir every hour. Best-effort:
// errors are logged but never propagate. The first sweep happens immediately
// so a contained restart shrinks the cache promptly.
func startCacheGC(transcodedDir string, maxBytes int64) {
	tick := func() {
		removed, err := transcoder.GC(transcodedDir, maxBytes)
		if err != nil {
			logging.Warn("transcode GC:", err)
			return
		}
		if len(removed) > 0 {
			logging.Info(fmt.Sprintf("transcode GC: evicted %d entries", len(removed)))
		}
	}
	tick()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		tick()
	}
}
