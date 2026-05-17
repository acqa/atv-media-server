package server

import (
	"net/http"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/storage"
)

// previewLimit is the number of recent items piped into the home-screen
// <paradePreview>. Kept small so the carousel loads fast and doesn't hammer
// the poster cache.
const previewLimit = 10

// PreviewPage is the payload for preview_movies.xml / preview_series.xml.
// Templates build the poster URL themselves using $.BasePath + their own
// endpoint prefix.
type PreviewPage struct {
	PosterIDs []string
}

// previewMoviesHandler returns a <paradePreview> of the most recently scanned
// movie posters. Rows without any poster/backdrop art are skipped — the
// carousel needs an image to display.
func previewMoviesHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		rows, err := store.ListRecentMovies(previewLimit)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		ids := make([]string, 0, len(rows))
		for _, m := range rows {
			if m.PosterPath == "" && m.BackdropPath == "" {
				continue
			}
			ids = append(ids, m.ID)
		}
		gen.Render(w, r, "preview_movies.xml", PreviewPage{PosterIDs: ids})
	}
}

// previewSeriesHandler returns a <paradePreview> of the most recently scanned
// series posters. Symmetrical to previewMoviesHandler but hits the series
// table and the /series-poster/ endpoint.
func previewSeriesHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		rows, err := store.ListRecentSeries(previewLimit)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		ids := make([]string, 0, len(rows))
		for _, s := range rows {
			if s.PosterPath == "" && s.BackdropPath == "" {
				continue
			}
			ids = append(ids, s.ID)
		}
		gen.Render(w, r, "preview_series.xml", PreviewPage{PosterIDs: ids})
	}
}
