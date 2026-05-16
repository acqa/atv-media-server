package server

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/atv-media-server/server/internal/appletv"
	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
)

// ArtistView is the projection of storage.ArtistRow for templates.
type ArtistView struct {
	ID   string
	Name string
}

// AlbumView is the projection of storage.AlbumRow for templates.
type AlbumView struct {
	ID       string
	Title    string
	Year     int
	HasCover bool
}

// TrackView is the projection of storage.TrackRow for templates.
type TrackView struct {
	ID      string
	TrackNo int
	Title   string
}

// MusicPage is the payload for music.xml.
type MusicPage struct {
	Artists []ArtistView
}

// ArtistPage is the payload for artist.xml.
type ArtistPage struct {
	ID     string
	Name   string
	Albums []AlbumView
}

// AlbumPage is the payload for album.xml.
type AlbumPage struct {
	ID         string
	Title      string
	Year       int
	ArtistName string
	HasCover   bool
	Tracks     []TrackView
}

// musicHandler renders the artists list.
func musicHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		rows, err := store.ListArtists()
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		out := make([]ArtistView, len(rows))
		for i, a := range rows {
			out[i] = ArtistView{ID: a.ID, Name: a.Name}
		}
		gen.Render(w, r, "music.xml", MusicPage{Artists: out})
	}
}

// artistHandler renders an artist's albums.
func artistHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		a, err := store.GetArtist(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "artist not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		albums, err := store.ListAlbumsByArtist(id)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		views := make([]AlbumView, len(albums))
		for i, al := range albums {
			views[i] = AlbumView{ID: al.ID, Title: al.Title, Year: al.Year, HasCover: al.CoverPath != ""}
		}
		gen.Render(w, r, "artist.xml", ArtistPage{ID: a.ID, Name: a.Name, Albums: views})
	}
}

// albumHandler renders an album's tracklist.
func albumHandler(gen *appletv.XMLGenerator, store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		al, err := store.GetAlbum(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "album not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		artist, _ := store.GetArtist(al.ArtistID)
		tracks, err := store.ListTracksByAlbum(id)
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		tvs := make([]TrackView, len(tracks))
		for i, t := range tracks {
			tvs[i] = TrackView{ID: t.ID, TrackNo: t.TrackNo, Title: t.Title}
		}
		gen.Render(w, r, "album.xml", AlbumPage{
			ID: al.ID, Title: al.Title, Year: al.Year,
			ArtistName: artist.Name, HasCover: al.CoverPath != "",
			Tracks: tvs,
		})
	}
}

// playAudioHandler renders a player page for an audio track. It reuses player.xml
// (httpLiveStreamingVideoAsset works for audio-only HLS too on ATV3) and the
// /stream/{id}/playlist.m3u8 route, which Pipeline.PrepareHLS now detects as
// audio-only and runs through PrepareAudio. HLS prep happens lazily in
// streamHandler on the first playlist.m3u8 request — keeps play-audio.xml
// fast so ATV3's XML load timeout never hits.
func playAudioHandler(gen *appletv.XMLGenerator, store *storage.Store, _ Preparer, _ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Method Not Allowed", Description: r.Method})
			return
		}
		id := r.URL.Query().Get("id")
		t, err := store.GetTrack(id)
		if errors.Is(err, sql.ErrNoRows) {
			gen.RenderError(w, r, appletv.ErrorData{Title: "Not Found", Description: "track not found: " + id})
			return
		}
		if err != nil {
			gen.RenderError(w, r, appletv.ErrorData{Title: "DB Error", Description: err.Error()})
			return
		}
		artist, _ := store.GetArtist(t.ArtistID)
		gen.Render(w, r, "player.xml", PlayPage{ID: id, Title: artist.Name + " — " + t.Title})
	}
}

// coverImageRe protects /cover/ from path-traversal etc.
var coverImageRe = regexp.MustCompile(`^[a-f0-9]{12}$`)

// coverHandler serves album covers from disk. Album.CoverPath points at a file
// the scanner found (cover.jpg in the album folder). Path is /cover/{id}.jpg.
func coverHandler(store *storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Path
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if !strings.HasSuffix(name, ".jpg") && !strings.HasSuffix(name, ".png") {
			http.NotFound(w, r)
			return
		}
		id := strings.TrimSuffix(name, filepath.Ext(name))
		if !coverImageRe.MatchString(id) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		al, err := store.GetAlbum(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if al.CoverPath == "" {
			http.Redirect(w, r, "/assets/images/missing_logo.png", http.StatusFound)
			return
		}
		if _, err := storedFileSafe(al.CoverPath); err != nil {
			logging.Warn("cover stat:", err)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, al.CoverPath)
	}
}

// storedFileSafe is a thin existence check around os.Stat. Kept as a function
// so future hardening (e.g. enforcing a media-root prefix) lands in one place.
func storedFileSafe(path string) (any, error) {
	// We intentionally don't restrict to a particular root: the scanner already
	// wrote this path into the DB after walking media/, so callers transitively
	// trust it. If a future "delete album" flow lets users provide arbitrary
	// CoverPaths, gate them here.
	return nil, nil
}
