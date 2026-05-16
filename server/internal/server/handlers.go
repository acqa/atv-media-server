package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
)

// MovieView is the projection of storage.MediaRow passed to XML templates.
type MovieView struct {
	ID        string
	Title     string
	Year      int
	HasPoster bool
	Rating    float64
	HasRating bool
}

// MoviesPage is the payload for movies.xml.
type MoviesPage struct {
	Movies []MovieView
}

// MoviePage is the payload for movie.xml.
type MoviePage struct {
	ID            string
	Title         string
	Year          int
	Description   string
	HasPoster     bool
	HasBackdrop   bool
	Rating        float64
	HasRating     bool
	RatingPercent int      // 0..100 for the <starRating> widget
	DurationStr   string   // "1h 47m"; empty when unknown
	QualityStr    string   // "1080p · H.264 · AC3"; empty when unknown
	Badges        []string // PNG filenames under /assets/badges/; empty when unknown
	AudioTracks   []int    // 0..N-1 — drives the "Audio N" buttons in the detail UI
}

// PlayPage is the payload for player.xml.
type PlayPage struct {
	ID       string
	Title    string
	AudioIdx int // 0-based audio stream selector; appended to stream URL as ?a=N
}

// audioRange returns [0, 1, ..., n-1] for use as the AudioTracks slice on
// detail-page payloads. Returns [0] (the default first track) when n < 2 so
// the UI still renders one play button.
func audioRange(n int) []int {
	if n < 2 {
		return []int{0}
	}
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// viewFromRow converts a stored row into the projection used by movies.xml.
func viewFromRow(r storage.MediaRow) MovieView {
	return MovieView{
		ID:        r.ID,
		Title:     r.Title,
		Year:      r.Year,
		HasPoster: r.BackdropPath != "" || r.PosterPath != "",
		Rating:    r.Rating,
		HasRating: r.Rating > 0,
	}
}

// moviesHandler returns the catalogue grid for /movies.xml.
func moviesHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		rows, err := store.ListMovies()
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		views := make([]MovieView, len(rows))
		for i, m := range rows {
			views[i] = viewFromRow(m)
		}
		gen.Render(w, r, "movies.xml", MoviesPage{Movies: views})
	}
}

// movieHandler returns the detail card for /movie.xml?id=...
func movieHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		m, err := store.GetMedia(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "movie not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		gen.Render(w, r, "movie.xml", MoviePage{
			ID:            m.ID,
			Title:         m.Title,
			Year:          m.Year,
			Description:   m.Description,
			HasPoster:     m.PosterPath != "" || m.BackdropPath != "",
			HasBackdrop:   m.BackdropPath != "",
			Rating:        m.Rating,
			HasRating:     m.Rating > 0,
			RatingPercent: RatingPercent(m.Rating),
			DurationStr:   FormatDuration(m.Duration),
			QualityStr:    FormatQuality(m.VideoHeight, m.VideoCodec, m.AudioCodec, m.AudioChannels),
			Badges:        BadgeFilenames(m.VideoHeight, m.VideoCodec, m.AudioCodec, m.AudioChannels),
			AudioTracks:   audioRange(m.AudioCount),
		})
	}
}

// playHandler renders the player template — actual HLS prep happens lazily
// in streamHandler on the first playlist.m3u8 request. Keeping this fast
// matters because ATV3 enforces a ~30s timeout on XML page loads.
// Resolves the id against both movies and episodes — IDs never collide because
// they're sha1-hashed with different prefixes.
func playHandler(gen *appletv.XMLGenerator, store *storage.Store, _ Preparer, _ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		path, title, err := resolveByID(store, id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "media not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		audioIdx := 0
		if a := r.URL.Query().Get("audio"); a != "" {
			if n, err := strconv.Atoi(a); err == nil && n >= 0 {
				audioIdx = n
			}
		}
		logging.Info(fmt.Sprintf("play.xml: id=%s audio=%d title=%q path=%q", id, audioIdx, title, path))
		// HLS prep happens lazily in streamHandler on the first /stream/<id>/playlist.m3u8
		// request — keeping play.xml fast so ATV3's XML load timeout (~30s) never hits.
		gen.Render(w, r, "player.xml", PlayPage{ID: id, Title: title, AudioIdx: audioIdx})
	}
}

// searchHandler renders the search input form (no data required).
func searchHandler(gen *appletv.XMLGenerator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		gen.Render(w, r, "search.xml", nil)
	}
}

// searchResultsHandler runs a LIKE search and renders results as a grid.
func searchResultsHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		term := r.URL.Query().Get("term")
		rows, err := store.SearchMovies(term)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		views := make([]MovieView, len(rows))
		for i, m := range rows {
			views[i] = viewFromRow(m)
		}
		gen.Render(w, r, "movies.xml", MoviesPage{Movies: views})
	}
}
