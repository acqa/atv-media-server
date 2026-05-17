package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
)

const defaultImageBaseURL = "https://image.tmdb.org/t/p"

// PosterCache serves TMDb posters and backdrops through our HTTPS host so
// ATV3 doesn't need to trust image.tmdb.org. Images are downloaded once and
// cached on disk; concurrent requests for the same image coalesce.
type PosterCache struct {
	root         string // base directory on disk, e.g. /data/posters
	store        *storage.Store
	http         *http.Client
	imageBaseURL string

	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// NewPosterCache returns a cache that persists files under root and looks
// up TMDb paths via store. root is created if missing.
func NewPosterCache(root string, store *storage.Store) *PosterCache {
	_ = os.MkdirAll(root, 0o755)
	return &PosterCache{
		root:         root,
		store:        store,
		http:         &http.Client{Timeout: 30 * time.Second},
		imageBaseURL: defaultImageBaseURL,
		inflight:     make(map[string]chan struct{}),
	}
}

// SetImageBaseURL overrides the upstream base URL — used by tests.
func (p *PosterCache) SetImageBaseURL(u string) { p.imageBaseURL = u }

// SetHTTP replaces the http.Client used for upstream fetches. main.go uses
// this to swap in a DoH-aware client on networks that DNS-sinkhole
// image.tmdb.org; the embedded *PosterCache in SeriesPosterCache and
// EpisodeStillCache inherits the swap via field promotion.
func (p *PosterCache) SetHTTP(c *http.Client) { p.http = c }

// allowedSizes is the closed set of TMDb sizes we proxy.
var allowedSizes = map[string]struct{}{
	"w300":     {},
	"w500":     {},
	"w780":     {},
	"w1280":    {},
	"original": {},
}

// PathResolver returns the TMDb-relative poster and backdrop paths for an id.
// ok=false signals "id not found" → 404.
type PathResolver func(id string) (poster, backdrop string, ok bool)

// Handler returns the /art/{id}.jpg handler for movies.
// Query: type=backdrop|poster (default backdrop), size=w300|w500|w780|w1280|original (default w780).
func (p *PosterCache) Handler() http.HandlerFunc {
	return p.posterHandlerFor(func(id string) (string, string, bool) {
		row, err := p.store.GetMedia(id)
		if err != nil {
			return "", "", false
		}
		return row.PosterPath, row.BackdropPath, true
	}, "movies")
}

// posterHandlerFor is the generic image proxy used by movie/series/episode
// caches. urlPrefix is used only to keep cache file names disjoint between
// kinds (movie poster vs series poster).
func (p *PosterCache) posterHandlerFor(resolve PathResolver, urlPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Path is /<prefix>/{id}.jpg — strip everything before the last segment.
		name := r.URL.Path
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if !strings.HasSuffix(name, ".jpg") {
			http.NotFound(w, r)
			return
		}
		id := strings.TrimSuffix(name, ".jpg")
		if !idRe.MatchString(id) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		kind := r.URL.Query().Get("type")
		if kind == "" {
			kind = "backdrop"
		}
		if kind != "backdrop" && kind != "poster" {
			http.Error(w, "bad type", http.StatusBadRequest)
			return
		}
		size := r.URL.Query().Get("size")
		if size == "" {
			size = "w780"
		}
		if _, ok := allowedSizes[size]; !ok {
			http.Error(w, "bad size", http.StatusBadRequest)
			return
		}

		poster, backdrop, ok := resolve(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		relPath := backdrop
		if kind == "poster" {
			relPath = poster
		}
		if relPath == "" {
			redirectMissing(w, r)
			return
		}

		cacheFile := filepath.Join(p.root, id+"_"+kind+"_"+size+".jpg")
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}
		sourceURL := p.imageBaseURL + "/" + size + relPath
		if err := p.ensureCached(r.Context(), cacheFile, sourceURL); err != nil {
			logging.Warn(urlPrefix+" fetch:", err)
			redirectMissing(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, cacheFile)
	}
}

// redirectMissing sends a 302 to the bundled placeholder. We disable client
// caching of the redirect itself so that once a poster is warmed (or the row
// later gets a poster_path), the next request actually re-resolves instead of
// silently re-using the cached placeholder. ATV3's CFNetwork was observed
// holding 302→missing_logo entries for an entire session otherwise.
func redirectMissing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/assets/images/missing_logo.png", http.StatusFound)
}

// ensureCached returns nil if cacheFile already exists (size > 0) or has been
// successfully downloaded from sourceURL. Concurrent calls for the same
// cacheFile coalesce on a single download.
func (p *PosterCache) ensureCached(ctx context.Context, cacheFile, sourceURL string) error {
	if info, err := os.Stat(cacheFile); err == nil && info.Size() > 0 {
		return nil
	}

	// Coalesce concurrent fetches.
	p.mu.Lock()
	if ch, ok := p.inflight[cacheFile]; ok {
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
		if info, err := os.Stat(cacheFile); err == nil && info.Size() > 0 {
			return nil
		}
		return errors.New("poster: concurrent fetch failed")
	}
	ch := make(chan struct{})
	p.inflight[cacheFile] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.inflight, cacheFile)
		close(ch)
		p.mu.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("poster: source returned " + resp.Status)
	}

	tmp := cacheFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, cacheFile)
}
