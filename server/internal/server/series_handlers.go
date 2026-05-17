package server

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/storage"
)

// SeriesView is the projection of storage.SeriesRow for grid templates.
type SeriesView struct {
	ID        string
	Title     string
	Year      int
	HasPoster bool
}

// SeriesPage is the payload for series.xml.
type SeriesPage struct {
	Series []SeriesView
}

// ShowPage is the payload for show.xml.
type ShowPage struct {
	ID            string
	Title         string
	Year          int
	Description   string
	HasPoster     bool
	HasBackdrop   bool
	Rating        float64
	HasRating     bool
	RatingPercent int
	Seasons       []int
	SeasonColumns int // visible columns in the centerShelf — capped at 5 to keep buttons readable
}

// SeasonPage is the payload for season.xml.
type SeasonPage struct {
	ShowID   string
	Season   int
	Episodes []EpisodeView
}

// EpisodeView projects storage.EpisodeRow for grid templates.
type EpisodeView struct {
	ID          string
	Season      int
	Episode     int
	Title       string
	HasStill    bool
	DurationStr string // "23m"; empty when unknown
}

// EpisodePage is the payload for episode.xml.
type EpisodePage struct {
	ID          string
	ShowTitle   string
	Season      int
	Episode     int
	Title       string
	Description string
	HasStill    bool
	DurationStr string   // "23m"; empty when unknown
	QualityStr  string   // "1080p · H.264 · AC3"; empty when unknown
	Badges      []string // PNG filenames under /assets/badges/; empty when unknown
	AudioTracks []int    // 0..N-1 — drives "Audio N" buttons
}

func seriesViewFrom(r storage.SeriesRow) SeriesView {
	return SeriesView{
		ID: r.ID, Title: r.Title, Year: r.Year,
		HasPoster: r.PosterPath != "" || r.BackdropPath != "",
	}
}

func episodeViewFrom(r storage.EpisodeRow) EpisodeView {
	return EpisodeView{
		ID: r.ID, Season: r.Season, Episode: r.Episode,
		Title: r.Title, HasStill: r.StillPath != "",
		DurationStr: FormatDuration(r.Duration),
	}
}

// seriesHandler renders /series.xml — the show grid.
func seriesHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		rows, err := store.ListSeries()
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		views := make([]SeriesView, len(rows))
		for i, s := range rows {
			views[i] = seriesViewFrom(s)
		}
		gen.Render(w, r, "series.xml", SeriesPage{Series: views})
	}
}

// showHandler renders /show.xml?id=... — series detail + season list.
func showHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		s, err := store.GetSeries(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "series not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		seasons, err := store.ListSeasonsOfSeries(id)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		cols := len(seasons)
		if cols > 5 {
			cols = 5
		}
		if cols < 1 {
			cols = 1 // shelf needs >=1 even if seasons is empty, to keep XML valid
		}
		gen.Render(w, r, "show.xml", ShowPage{
			ID: s.ID, Title: s.Title, Year: s.Year, Description: s.Description,
			HasPoster:     s.PosterPath != "" || s.BackdropPath != "",
			HasBackdrop:   s.BackdropPath != "",
			Rating:        s.Rating,
			HasRating:     s.Rating > 0,
			RatingPercent: RatingPercent(s.Rating),
			Seasons:       seasons,
			SeasonColumns: cols,
		})
	}
}

// seasonHandler renders /season.xml?show=&s=... — episode grid for a season.
func seasonHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		show := r.URL.Query().Get("show")
		season, err := strconv.Atoi(r.URL.Query().Get("s"))
		if err != nil || season <= 0 {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Bad Request", Description: "invalid season"})
			return
		}
		rows, err := store.ListEpisodesBySeason(show, season)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		views := make([]EpisodeView, len(rows))
		for i, e := range rows {
			views[i] = episodeViewFrom(e)
		}
		gen.Render(w, r, "season.xml", SeasonPage{ShowID: show, Season: season, Episodes: views})
	}
}

// episodeHandler renders /episode.xml?id=... — single-episode detail card.
func episodeHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		e, err := store.GetEpisode(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "episode not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		show, _ := store.GetSeries(e.SeriesID)
		gen.Render(w, r, "episode.xml", EpisodePage{
			ID: e.ID, ShowTitle: show.Title, Season: e.Season, Episode: e.Episode,
			Title: e.Title, Description: e.Description, HasStill: e.StillPath != "",
			DurationStr: FormatDuration(e.Duration),
			QualityStr:  FormatQuality(e.VideoHeight, e.VideoCodec, e.AudioCodec, e.AudioChannels),
			Badges:      BadgeFilenames(e.VideoHeight, e.VideoCodec, e.AudioCodec, e.AudioChannels),
			AudioTracks: audioRange(e.AudioCount),
		})
	}
}

// Unified play handler is in handlers.go. It currently only looks in the media
// table. We need a separate /play.xml handling for episodes — but since IDs
// for movies and episodes never collide (different sha1 prefixes), we can
// extend play to try both tables. The cleanest place is a helper here.

// resolveByID returns the playable (path, title) for an id, checking movies,
// episodes and tracks in turn. Returns ("", "", sql.ErrNoRows) if missing.
func resolveByID(store *storage.Store, id string) (path, title string, err error) {
	if m, err := store.GetMedia(id); err == nil {
		return m.Path, m.Title, nil
	}
	if e, err := store.GetEpisode(id); err == nil {
		show, _ := store.GetSeries(e.SeriesID)
		title = show.Title
		if e.Title != "" {
			title = show.Title + " — S" + leadingZero(e.Season) + "E" + leadingZero(e.Episode) + " — " + e.Title
		}
		return e.Path, title, nil
	}
	t, err := store.GetTrack(id)
	if err != nil {
		return "", "", err
	}
	artist, _ := store.GetArtist(t.ArtistID)
	return t.Path, artist.Name + " — " + t.Title, nil
}

func leadingZero(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// SeriesPosterHandler proxies series posters/backdrops, mirroring PosterCache
// for movies but reading from the series table.
type SeriesPosterCache struct {
	*PosterCache
	store *storage.Store
}

// NewSeriesPosterCache returns a poster cache that resolves IDs against the
// series table. It reuses the PosterCache directory/HTTP/coalescing logic via
// composition.
func NewSeriesPosterCache(root string, store *storage.Store) *SeriesPosterCache {
	c := NewPosterCache(filepath.Join(root, "series"), store)
	return &SeriesPosterCache{PosterCache: c, store: store}
}

// Handler returns /art-series/{id}.jpg. Same query semantics as movie posters.
func (s *SeriesPosterCache) Handler() http.HandlerFunc {
	return s.posterHandlerFor(func(id string) (poster, backdrop string, ok bool) {
		row, err := s.store.GetSeries(id)
		if err != nil {
			return "", "", false
		}
		return row.PosterPath, row.BackdropPath, true
	}, "series")
}

// EpisodeStillCache proxies episode stills (single-image only, no poster/backdrop split).
type EpisodeStillCache struct {
	*PosterCache
	store *storage.Store
}

// NewEpisodeStillCache returns a cache for episode stills.
func NewEpisodeStillCache(root string, store *storage.Store) *EpisodeStillCache {
	c := NewPosterCache(filepath.Join(root, "episodes"), store)
	return &EpisodeStillCache{PosterCache: c, store: store}
}

// Handler returns /art-still/{id}.jpg. Only accepts size=, no type.
func (s *EpisodeStillCache) Handler() http.HandlerFunc {
	return s.posterHandlerFor(func(id string) (poster, backdrop string, ok bool) {
		row, err := s.store.GetEpisode(id)
		if err != nil {
			return "", "", false
		}
		return row.StillPath, row.StillPath, true
	}, "episodes")
}
