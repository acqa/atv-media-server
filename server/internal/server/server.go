package server

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/config"
	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

//go:embed assets
var assets embed.FS

// Deps bundles long-lived dependencies the HTTP layer needs.
type Deps struct {
	Store         *storage.Store
	Preparer      Preparer
	Posters       *PosterCache
	SeriesPosters *SeriesPosterCache
	EpisodeStills *EpisodeStillCache
	ScanState     *ScanState
}

// Serve starts HTTP and HTTPS listeners. Blocks until either fails.
func Serve(cfg *config.Config, deps Deps) error {
	gen := appletv.New(cfg.BaseHost)
	if deps.Preparer == nil {
		deps.Preparer = transcoder.NewPipeline()
	}
	handler := accessLog(buildMux(cfg, gen, deps))

	errs := make(chan error, 2)
	go func() {
		logging.Info("HTTP listening on :" + cfg.HTTPPort)
		errs <- http.ListenAndServe(":"+cfg.HTTPPort, handler)
	}()
	go func() {
		logging.Info("HTTPS listening on :" + cfg.HTTPSPort)
		errs <- http.ListenAndServeTLS(
			":"+cfg.HTTPSPort,
			cfg.CertPath("redbulltv.pem"),
			cfg.CertPath("redbulltv.key"),
			handler,
		)
	}()
	return <-errs
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		// User-Agent distinguishes ATV3's UIWebView ("Mozilla/...") from its
		// media stack ("AppleCoreMedia/..."). When debugging playback issues
		// we need to know which client is fetching /play.xml vs /stream/.
		ua := r.Header.Get("User-Agent")
		if len(ua) > 80 {
			ua = ua[:80] + "..."
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		logging.Info(fmt.Sprintf("%s %s %s %s %d %db %s ua=%q",
			r.RemoteAddr, scheme, r.Method, r.URL.RequestURI(),
			rec.status, rec.bytes, time.Since(start).Round(time.Millisecond), ua))
	})
}

// buildMux assembles the route table. Separated for testability.
func buildMux(cfg *config.Config, gen *appletv.XMLGenerator, deps Deps) *http.ServeMux {
	mux := http.NewServeMux()

	assetsFS, err := fs.Sub(assets, "assets")
	if err != nil {
		logging.Fatal("embed assets:", err)
	}
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))

	mux.HandleFunc("/redbulltv.cer", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, cfg.CertPath("redbulltv.cer"))
	})

	mux.HandleFunc("/movies.xml", moviesHandler(gen, deps.Store))
	mux.HandleFunc("/movie.xml", movieHandler(gen, deps.Store))
	mux.HandleFunc("/preview-movies.xml", previewMoviesHandler(gen, deps.Store))
	mux.HandleFunc("/preview-series.xml", previewSeriesHandler(gen, deps.Store))
	mux.HandleFunc("/series.xml", seriesHandler(gen, deps.Store))
	mux.HandleFunc("/show.xml", showHandler(gen, deps.Store))
	mux.HandleFunc("/season.xml", seasonHandler(gen, deps.Store))
	mux.HandleFunc("/episode.xml", episodeHandler(gen, deps.Store))
	mux.HandleFunc("/music.xml", musicHandler(gen, deps.Store))
	mux.HandleFunc("/artist.xml", artistHandler(gen, deps.Store))
	mux.HandleFunc("/album.xml", albumHandler(gen, deps.Store))
	mux.HandleFunc("/play.xml", playHandler(gen, deps.Store, deps.Preparer, cfg.DataDir))
	mux.HandleFunc("/play-audio.xml", playAudioHandler(gen, deps.Store, deps.Preparer, cfg.DataDir))
	mux.HandleFunc("/cover/", coverHandler(deps.Store))
	mux.HandleFunc("/search.xml", searchHandler(gen))
	mux.HandleFunc("/search-results.xml", searchResultsHandler(gen, deps.Store))
	mux.HandleFunc("/stream/", streamHandler(deps.Store, deps.Preparer, cfg.DataDir))
	if deps.Posters != nil {
		mux.HandleFunc("/poster/", deps.Posters.Handler())
	}
	if deps.SeriesPosters != nil {
		mux.HandleFunc("/series-poster/", deps.SeriesPosters.Handler())
	}
	if deps.EpisodeStills != nil {
		mux.HandleFunc("/episode-still/", deps.EpisodeStills.Handler())
	}
	if deps.ScanState != nil {
		mux.HandleFunc("/api/library/scan", deps.ScanState.ScanHandler())
		mux.HandleFunc("/api/library/status", deps.ScanState.StatusHandler())
	}

	mux.HandleFunc("/", gen.MainHandler)
	return mux
}
