package storage

import (
	"errors"
	"time"
)

// SeriesRow is one row in the series table.
type SeriesRow struct {
	ID           string
	Path         string
	Title        string
	Year         int
	Description  string
	PosterPath   string
	BackdropPath string
	Rating       float64
	TMDbID       int
	UpdatedAt    time.Time
}

// EpisodeRow is one row in the episodes table.
type EpisodeRow struct {
	ID             string
	SeriesID       string
	Season         int
	Episode        int
	Path           string
	Title          string
	Description    string
	StillPath      string
	Duration       int
	VideoCodec     string
	AudioCodec     string
	VideoHeight    int // pixels (1080/720/...); 0 = unknown
	AudioChannels  int // stream channels (2/6/8); 0 = unknown
	AudioCount     int
	NeedsTranscode bool
	UpdatedAt      time.Time
}

const seriesColumns = `id, path, title, year, description, poster_path, backdrop_path,
	rating, tmdb_id, updated_at`

const episodeColumns = `id, series_id, season, episode, path, title, description,
	still_path, duration, video_codec, audio_codec, video_height, audio_channels,
	audio_count, needs_transcode, updated_at`

// UpsertSeries inserts or updates a series row.
func (s *Store) UpsertSeries(r SeriesRow) error {
	if r.ID == "" || r.Path == "" || r.Title == "" {
		return errors.New("SeriesRow: ID, Path, Title required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO series (`+seriesColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path = excluded.path,
			title = excluded.title,
			year = excluded.year,
			description = excluded.description,
			poster_path = excluded.poster_path,
			backdrop_path = excluded.backdrop_path,
			rating = excluded.rating,
			tmdb_id = excluded.tmdb_id,
			updated_at = excluded.updated_at
	`, r.ID, r.Path, r.Title, r.Year, r.Description, r.PosterPath, r.BackdropPath,
		r.Rating, r.TMDbID, r.UpdatedAt)
	return err
}

// GetSeries returns one series by id.
func (s *Store) GetSeries(id string) (SeriesRow, error) {
	row := s.db.QueryRow(`SELECT `+seriesColumns+` FROM series WHERE id = ?`, id)
	return scanSeries(row)
}

// ListSeries returns all series ordered by title (case-insensitive).
func (s *Store) ListSeries() ([]SeriesRow, error) {
	rows, err := s.db.Query(`SELECT ` + seriesColumns + ` FROM series ORDER BY title COLLATE NOCASE, year`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SeriesRow
	for rows.Next() {
		r, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRecentSeries returns up to n series ordered by updated_at DESC, then id
// (stable tiebreak). Used by the home-screen preview carousel.
func (s *Store) ListRecentSeries(n int) ([]SeriesRow, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT `+seriesColumns+` FROM series ORDER BY updated_at DESC, id LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SeriesRow
	for rows.Next() {
		r, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertEpisode inserts or updates an episode row.
func (s *Store) UpsertEpisode(r EpisodeRow) error {
	if r.ID == "" || r.SeriesID == "" || r.Path == "" || r.Season <= 0 || r.Episode <= 0 {
		return errors.New("EpisodeRow: ID, SeriesID, Path required; Season/Episode > 0")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	if r.AudioCount < 1 {
		r.AudioCount = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO episodes (`+episodeColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			series_id = excluded.series_id,
			season = excluded.season,
			episode = excluded.episode,
			path = excluded.path,
			title = excluded.title,
			description = excluded.description,
			still_path = excluded.still_path,
			duration = excluded.duration,
			video_codec = excluded.video_codec,
			audio_codec = excluded.audio_codec,
			video_height = excluded.video_height,
			audio_channels = excluded.audio_channels,
			audio_count = excluded.audio_count,
			needs_transcode = excluded.needs_transcode,
			updated_at = excluded.updated_at
	`, r.ID, r.SeriesID, r.Season, r.Episode, r.Path, r.Title, r.Description,
		r.StillPath, r.Duration, r.VideoCodec, r.AudioCodec, r.VideoHeight, r.AudioChannels,
		r.AudioCount, r.NeedsTranscode, r.UpdatedAt)
	return err
}

// GetEpisode returns one episode by id.
func (s *Store) GetEpisode(id string) (EpisodeRow, error) {
	row := s.db.QueryRow(`SELECT `+episodeColumns+` FROM episodes WHERE id = ?`, id)
	return scanEpisode(row)
}

// ListEpisodesBySeries returns all episodes for a show, sorted by (season, episode).
func (s *Store) ListEpisodesBySeries(seriesID string) ([]EpisodeRow, error) {
	rows, err := s.db.Query(`SELECT `+episodeColumns+` FROM episodes WHERE series_id = ? ORDER BY season, episode`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EpisodeRow
	for rows.Next() {
		r, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListEpisodesBySeason returns episodes for a (series_id, season) pair.
func (s *Store) ListEpisodesBySeason(seriesID string, season int) ([]EpisodeRow, error) {
	rows, err := s.db.Query(`SELECT `+episodeColumns+` FROM episodes WHERE series_id = ? AND season = ? ORDER BY episode`, seriesID, season)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EpisodeRow
	for rows.Next() {
		r, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListSeasonsOfSeries returns the distinct season numbers for a show, ascending.
func (s *Store) ListSeasonsOfSeries(seriesID string) ([]int, error) {
	rows, err := s.db.Query(`SELECT DISTINCT season FROM episodes WHERE series_id = ? ORDER BY season`, seriesID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanSeries(row rowScanner) (SeriesRow, error) {
	var r SeriesRow
	err := row.Scan(&r.ID, &r.Path, &r.Title, &r.Year, &r.Description, &r.PosterPath,
		&r.BackdropPath, &r.Rating, &r.TMDbID, &r.UpdatedAt)
	return r, err
}

func scanEpisode(row rowScanner) (EpisodeRow, error) {
	var r EpisodeRow
	err := row.Scan(&r.ID, &r.SeriesID, &r.Season, &r.Episode, &r.Path, &r.Title,
		&r.Description, &r.StillPath, &r.Duration, &r.VideoCodec, &r.AudioCodec,
		&r.VideoHeight, &r.AudioChannels,
		&r.AudioCount, &r.NeedsTranscode, &r.UpdatedAt)
	return r, err
}
