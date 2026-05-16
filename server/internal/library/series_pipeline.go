package library

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/atv-media-server/server/internal/metadata"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

// SeriesScanResult summarises a series-tree pass.
type SeriesScanResult struct {
	Series   int
	Episodes int
	Probed   int
	Matched  int // series with a TMDb hit
}

// ScanSeriesAndUpsert walks root (typically <media>/series), parses the
// hierarchy and upserts a series row + episode rows for every show found.
// tmdb and prober are both optional. TMDb episode lookups happen only when
// the series has a TMDb match — saves a round trip per file.
func ScanSeriesAndUpsert(ctx context.Context, root string, store *storage.Store, tmdb TMDbTVSearcher, prober transcoder.Prober, log Logger) (SeriesScanResult, error) {
	if log == nil {
		log = func(string, ...interface{}) {}
	}
	series, episodes, err := ScanSeries(root)
	if err != nil {
		return SeriesScanResult{}, err
	}
	res := SeriesScanResult{Series: len(series), Episodes: len(episodes)}

	tvByID := map[string]metadata.TVResult{}
	for _, s := range series {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		row := storage.SeriesRow{
			ID:        s.ID,
			Path:      s.Path,
			Title:     s.Title,
			Year:      s.Year,
			UpdatedAt: time.Now().UTC(),
		}
		if tmdb != nil {
			hit, ok, terr := tmdb.SearchTV(ctx, s.Title, s.Year)
			switch {
			case terr != nil && !errors.Is(terr, metadata.ErrNoAPIKey):
				log("TMDb tv %q: %v", s.Title, terr)
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
				tvByID[s.ID] = hit
				res.Matched++
			}
		}
		if err := store.UpsertSeries(row); err != nil {
			log("upsert series %s: %v", s.ID, err)
			continue
		}
	}

	for _, e := range episodes {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		existing, err := store.GetEpisode(e.ID)
		hasRow := err == nil
		if !hasRow && !errors.Is(err, sql.ErrNoRows) {
			log("get episode %s: %v", e.ID, err)
			continue
		}
		mtime, err := fileMTime(e.Path)
		if err != nil {
			log("stat %s: %v", e.Path, err)
			continue
		}
		// Skip if row is post-mtime AND has a probe result (codecs filled).
		if hasRow && existing.UpdatedAt.After(mtime) && existing.VideoCodec != "" {
			continue
		}

		row := storage.EpisodeRow{
			ID:        e.ID,
			SeriesID:  e.SeriesID,
			Season:    e.Season,
			Episode:   e.Episode,
			Path:      e.Path,
			UpdatedAt: time.Now().UTC(),
		}
		if tv, ok := tvByID[e.SeriesID]; ok && tmdb != nil {
			ep, found, eerr := tmdb.GetEpisode(ctx, tv.TMDbID, e.Season, e.Episode)
			if eerr != nil && !errors.Is(eerr, metadata.ErrNoAPIKey) {
				log("TMDb episode S%dE%d: %v", e.Season, e.Episode, eerr)
			} else if found {
				row.Title = ep.Name
				row.Description = ep.Description
				row.StillPath = ep.StillPath
			}
		}
		if prober != nil {
			info, perr := prober.Probe(ctx, e.Path)
			if perr != nil {
				log("ffprobe %s: %v", e.Path, perr)
			} else {
				row.VideoCodec = info.VideoCodec
				row.AudioCodec = info.AudioCodec
				row.AudioCount = info.AudioCount
				row.Duration = info.DurationSec
				row.NeedsTranscode = transcoder.ChooseMode(info) != transcoder.ModeRemux
				res.Probed++
			}
		}
		if err := store.UpsertEpisode(row); err != nil {
			log("upsert episode %s: %v", e.ID, err)
			continue
		}
	}
	log("scan series: series=%d episodes=%d matched=%d probed=%d", res.Series, res.Episodes, res.Matched, res.Probed)
	return res, nil
}
