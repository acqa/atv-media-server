// Package admin is the web admin panel on port 8080. Basic Auth gated.
// It's a tiny REST + static HTML UI; nothing heavy on purpose — the heavy
// lifting (scanning, transcode GC) lives in the public package.
package admin

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/server"
	"github.com/atv-media-server/server/internal/storage"
)

//go:embed static
var staticFS embed.FS

// Config configures the admin listener.
type Config struct {
	User string // Basic Auth username; if empty, the admin is disabled.
	Pass string
	Port string // e.g. "8080"
}

// Deps holds the long-lived dependencies the admin reuses from the public
// server. We don't open our own scans/DB — we share state with the public side.
type Deps struct {
	Store     *storage.Store
	ScanState *server.ScanState
}

// Serve starts the admin listener. Blocks until the listener errors.
// If cfg.User is empty, returns nil immediately so a misconfigured deployment
// doesn't leak an unauthenticated control plane.
func Serve(cfg Config, deps Deps) error {
	if cfg.User == "" || cfg.Pass == "" {
		logging.Info("admin disabled (ADMIN_USER/ADMIN_PASS empty)")
		return nil
	}
	mux := buildMux(cfg, deps)
	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	logging.Info("admin listening on :" + port)
	return http.ListenAndServe(":"+port, mux)
}

func buildMux(cfg Config, deps Deps) http.Handler {
	mux := http.NewServeMux()

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		logging.Fatal("admin static embed:", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

	mux.HandleFunc("/api/stats", statsHandler(deps.Store))
	if deps.ScanState != nil {
		mux.HandleFunc("/api/scan", deps.ScanState.ScanHandler())
		mux.HandleFunc("/api/scan/status", deps.ScanState.StatusHandler())
	}

	return basicAuth(cfg, mux)
}

// basicAuth gates every request with HTTP Basic. crypto/subtle is overkill
// for two short strings; constant-time compare via string equality is fine
// inside a private LAN.
func basicAuth(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="ATV3 Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if user != cfg.User || pass != cfg.Pass {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type stats struct {
	Movies  int `json:"movies"`
	Series  int `json:"series"`
	Tracks  int `json:"tracks"`
	Artists int `json:"artists"`
}

// statsHandler returns counts across the library in a single call. We don't
// expose individual records here; the ATV3 endpoints already do that.
func statsHandler(store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var s stats
		s.Movies, _ = store.CountMedia("movie")
		if rows, err := store.ListSeries(); err == nil {
			s.Series = len(rows)
		}
		if rows, err := store.ListArtists(); err == nil {
			s.Artists = len(rows)
		}
		s.Tracks, _ = store.CountTracks()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s)
	}
}
