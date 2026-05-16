package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/atv-media-server/server/internal/metadata"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

// TMDbSearcher is the subset of metadata.Client used by the scanner.
// Decoupling lets tests substitute a fake without an HTTP server.
type TMDbSearcher interface {
	SearchMovie(ctx context.Context, query string, year int) (metadata.MovieResult, bool, error)
}

// TMDbTVSearcher is the TV-shaped half of the TMDb client.
type TMDbTVSearcher interface {
	SearchTV(ctx context.Context, query string, year int) (metadata.TVResult, bool, error)
	GetEpisode(ctx context.Context, tvID, season, episode int) (metadata.EpisodeResult, bool, error)
}

// Logger is the minimal logging surface the pipeline uses. nil is safe.
type Logger func(format string, args ...interface{})

// ScanResult summarises one pipeline run.
type ScanResult struct {
	Total   int // files seen
	Matched int // rows with a TMDb hit
	Skipped int // rows whose mtime predated the existing row's updated_at (no work)
	Probed  int // rows for which we ran ffprobe to derive codecs/duration
	Deleted int // rows evicted because their source file is gone
}

// ScanAndUpsert walks root, parses names, optionally enriches via TMDb and
// ffprobe, and upserts each movie into store. The pipeline is idempotent:
// a movie whose file is older than the stored row is left untouched, so
// reruns are cheap.
//
// If tmdb is nil or returns ErrNoAPIKey, files are stored with only the title
// and year derived from filenames. If prober is nil, codec/duration columns
// stay empty and NeedsTranscode defaults to false (remux will likely fail
// for incompatible files until a probe-enabled rescan happens).
func ScanAndUpsert(ctx context.Context, root string, store *storage.Store, tmdb TMDbSearcher, prober transcoder.Prober, log Logger) (ScanResult, error) {
	if log == nil {
		log = func(string, ...interface{}) {}
	}
	movies, err := Scan(root)
	if err != nil {
		return ScanResult{}, err
	}
	res := ScanResult{Total: len(movies)}

	for _, m := range movies {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		mtime, err := fileMTime(m.Path)
		if err != nil {
			log("stat %s: %v", m.Path, err)
			continue
		}
		existing, err := store.GetMedia(m.ID)
		hasRow := err == nil
		if !hasRow && !errors.Is(err, sql.ErrNoRows) {
			log("get %s: %v", m.ID, err)
			continue
		}
		// Skip when stored row is post-mtime AND already has both TMDb match
		// and a probe result (VideoCodec populated). This way Phase 2 rows
		// without codec info get re-processed once on the first Phase 3 run.
		if hasRow && existing.UpdatedAt.After(mtime) && existing.TMDbID != 0 && existing.VideoCodec != "" {
			res.Skipped++
			continue
		}

		row := storage.MediaRow{
			ID:        m.ID,
			Path:      m.Path,
			Type:      "movie",
			Title:     m.Title,
			Year:      m.Year,
			UpdatedAt: time.Now().UTC(),
		}
		if tmdb != nil {
			hit, ok, terr := tmdb.SearchMovie(ctx, m.Title, m.Year)
			switch {
			case terr != nil && !errors.Is(terr, metadata.ErrNoAPIKey):
				log("TMDb %q: %v", m.Title, terr)
			case ok:
				row.Title = hit.Title
				if hit.Year != 0 {
					row.Year = hit.Year
				}
				row.Description = hit.Description
				row.PosterPath = hit.PosterPath
				row.BackdropPath = hit.BackdropPath
				row.Rating = hit.Rating
				row.TMDbID = hit.TMDbID
				res.Matched++
			}
		}
		if prober != nil {
			info, perr := prober.Probe(ctx, m.Path)
			if perr != nil {
				// Don't fail the whole row — the file might still play through
				// transcode, we just don't know its codecs yet.
				log("ffprobe %s: %v", m.Path, perr)
			} else {
				row.VideoCodec = info.VideoCodec
				row.AudioCodec = info.AudioCodec
				row.AudioCount = info.AudioCount
				row.Duration = info.DurationSec
				row.NeedsTranscode = transcoder.ChooseMode(info) != transcoder.ModeRemux
				res.Probed++
			}
		}
		if err := store.UpsertMedia(row); err != nil {
			log("upsert %s: %v", m.ID, err)
			continue
		}
	}
	// Evict rows whose source file is gone — keeps the catalog in sync with
	// disk so users don't see ghosts after they delete files from media/.
	existingRows, err := store.ListMovies()
	if err != nil {
		log("list for gc: %v", err)
	} else {
		for _, row := range existingRows {
			if _, err := os.Stat(row.Path); errors.Is(err, os.ErrNotExist) {
				if derr := store.DeleteMedia(row.ID); derr != nil {
					log("delete stale %s (%s): %v", row.ID, row.Path, derr)
					continue
				}
				res.Deleted++
			}
		}
	}
	log("scan: total=%d matched=%d skipped=%d probed=%d deleted=%d",
		res.Total, res.Matched, res.Skipped, res.Probed, res.Deleted)
	return res, nil
}

func fileMTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime().UTC(), nil
}
