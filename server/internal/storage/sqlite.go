// Package storage persists library metadata in SQLite. The media table is the
// single source of truth from Phase 2 onward — the in-memory library.Library
// becomes a read-through view (or is removed) and handlers read from here.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps a sql.DB connected to a SQLite database. It is safe for concurrent
// use; the underlying SQLite pool serialises writes.
type Store struct {
	db *sql.DB
}

// MediaRow is one row in the media table. Phase 2 only writes type="movie";
// Phase 4 will add "episode" and "track".
type MediaRow struct {
	ID             string
	Path           string
	Type           string
	Title          string
	Year           int
	Description    string
	PosterPath     string // TMDb relative path, e.g. "/abc.jpg" — not absolute URL
	BackdropPath   string
	Rating         float64
	TMDbID         int
	Duration       int    // seconds; 0 = unknown
	VideoCodec     string // populated in Phase 3
	AudioCodec     string // populated in Phase 3
	AudioCount     int    // number of audio streams in the source (default 1)
	NeedsTranscode bool   // Phase 3: false means PrepareHLS uses remux
	UpdatedAt      time.Time
}

// Open opens (and creates if missing) a SQLite database at path and applies
// migrations. The returned Store must be Closed.
func Open(path string) (*Store, error) {
	// _journal=WAL gives concurrent readers during writes; _busy_timeout avoids
	// "database is locked" under brief contention.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error { return s.db.Close() }

// migrate creates schema on a fresh DB and is a no-op on subsequent runs.
// Migrations are tracked in a tiny schema_version table.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}
	for i, m := range migrations {
		v := i + 1
		if v <= current {
			continue
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

var migrations = []string{
	// 001 — media catalogue. series/episodes/etc. ride on the type column from
	// Phase 4; for Phase 2 only "movie" is written.
	`CREATE TABLE media (
		id            TEXT PRIMARY KEY,
		path          TEXT NOT NULL UNIQUE,
		type          TEXT NOT NULL,
		title         TEXT NOT NULL,
		year          INTEGER NOT NULL DEFAULT 0,
		description   TEXT NOT NULL DEFAULT '',
		poster_path   TEXT NOT NULL DEFAULT '',
		backdrop_path TEXT NOT NULL DEFAULT '',
		rating        REAL NOT NULL DEFAULT 0,
		tmdb_id       INTEGER NOT NULL DEFAULT 0,
		duration      INTEGER NOT NULL DEFAULT 0,
		video_codec   TEXT NOT NULL DEFAULT '',
		audio_codec   TEXT NOT NULL DEFAULT '',
		updated_at    DATETIME NOT NULL
	);
	CREATE INDEX idx_media_type_title ON media(type, title COLLATE NOCASE);`,
	// 002 — flag rows that need full transcode. Cheap to derive at scan time
	// via ffprobe; expensive to compute on the fly when the user hits Play.
	`ALTER TABLE media ADD COLUMN needs_transcode INTEGER NOT NULL DEFAULT 0`,
	// 003 — TV series + episodes. Episodes reference series via series_id.
	// Per-episode metadata (still_url, description) lives on the episode row;
	// per-show metadata (poster, description) on the series row.
	`CREATE TABLE series (
		id            TEXT PRIMARY KEY,
		path          TEXT NOT NULL UNIQUE,
		title         TEXT NOT NULL,
		year          INTEGER NOT NULL DEFAULT 0,
		description   TEXT NOT NULL DEFAULT '',
		poster_path   TEXT NOT NULL DEFAULT '',
		backdrop_path TEXT NOT NULL DEFAULT '',
		rating        REAL NOT NULL DEFAULT 0,
		tmdb_id       INTEGER NOT NULL DEFAULT 0,
		updated_at    DATETIME NOT NULL
	);
	CREATE INDEX idx_series_title ON series(title COLLATE NOCASE);

	CREATE TABLE episodes (
		id              TEXT PRIMARY KEY,
		series_id       TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
		season          INTEGER NOT NULL,
		episode         INTEGER NOT NULL,
		path            TEXT NOT NULL UNIQUE,
		title           TEXT NOT NULL DEFAULT '',
		description     TEXT NOT NULL DEFAULT '',
		still_path      TEXT NOT NULL DEFAULT '',
		duration        INTEGER NOT NULL DEFAULT 0,
		video_codec     TEXT NOT NULL DEFAULT '',
		audio_codec     TEXT NOT NULL DEFAULT '',
		needs_transcode INTEGER NOT NULL DEFAULT 0,
		updated_at      DATETIME NOT NULL,
		UNIQUE(series_id, season, episode)
	);
	CREATE INDEX idx_episodes_series_season ON episodes(series_id, season, episode);`,
	// 004 — music tables. Artists ⇐ Albums ⇐ Tracks.
	`CREATE TABLE artists (
		id    TEXT PRIMARY KEY,
		name  TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);
	CREATE INDEX idx_artists_name ON artists(name COLLATE NOCASE);

	CREATE TABLE albums (
		id        TEXT PRIMARY KEY,
		artist_id TEXT NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
		title     TEXT NOT NULL,
		year      INTEGER NOT NULL DEFAULT 0,
		cover_path TEXT NOT NULL DEFAULT '',
		updated_at DATETIME NOT NULL,
		UNIQUE(artist_id, title)
	);
	CREATE INDEX idx_albums_title ON albums(title COLLATE NOCASE);

	CREATE TABLE tracks (
		id        TEXT PRIMARY KEY,
		album_id  TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
		artist_id TEXT NOT NULL,
		track_no  INTEGER NOT NULL DEFAULT 0,
		title     TEXT NOT NULL,
		path      TEXT NOT NULL UNIQUE,
		duration  INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL
	);
	CREATE INDEX idx_tracks_album ON tracks(album_id, track_no);`,
	// 005 — audio track count for movies and episodes. Drives the multi-dub
	// "Audio N" buttons in the UI; defaults to 1 for legacy rows.
	`ALTER TABLE media ADD COLUMN audio_count INTEGER NOT NULL DEFAULT 1;
	 ALTER TABLE episodes ADD COLUMN audio_count INTEGER NOT NULL DEFAULT 1;`,
}

// UpsertMedia inserts or replaces a row keyed by id. UpdatedAt is set to now
// unless the caller has already populated it (non-zero).
func (s *Store) UpsertMedia(m MediaRow) error {
	if m.ID == "" || m.Path == "" || m.Type == "" || m.Title == "" {
		return errors.New("MediaRow: ID, Path, Type, Title are required")
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}
	if m.AudioCount < 1 {
		m.AudioCount = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO media (id, path, type, title, year, description, poster_path, backdrop_path,
			rating, tmdb_id, duration, video_codec, audio_codec, audio_count, needs_transcode, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path = excluded.path,
			type = excluded.type,
			title = excluded.title,
			year = excluded.year,
			description = excluded.description,
			poster_path = excluded.poster_path,
			backdrop_path = excluded.backdrop_path,
			rating = excluded.rating,
			tmdb_id = excluded.tmdb_id,
			duration = excluded.duration,
			video_codec = excluded.video_codec,
			audio_codec = excluded.audio_codec,
			audio_count = excluded.audio_count,
			needs_transcode = excluded.needs_transcode,
			updated_at = excluded.updated_at
	`, m.ID, m.Path, m.Type, m.Title, m.Year, m.Description, m.PosterPath, m.BackdropPath,
		m.Rating, m.TMDbID, m.Duration, m.VideoCodec, m.AudioCodec, m.AudioCount, m.NeedsTranscode, m.UpdatedAt)
	return err
}

// GetMedia returns a row by id. Returns (zero, sql.ErrNoRows) when absent.
func (s *Store) GetMedia(id string) (MediaRow, error) {
	return scanOne(s.db.QueryRow(`SELECT `+mediaColumns+` FROM media WHERE id = ?`, id))
}

// ListMovies returns all rows with type='movie' ordered by title (case-insensitive), year.
func (s *Store) ListMovies() ([]MediaRow, error) {
	return queryRows(s.db, `SELECT `+mediaColumns+` FROM media WHERE type = 'movie' ORDER BY title COLLATE NOCASE, year`)
}

// SearchMovies returns movies whose title contains term (case-insensitive).
// Empty term returns all movies (equivalent to ListMovies).
func (s *Store) SearchMovies(term string) ([]MediaRow, error) {
	if term == "" {
		return s.ListMovies()
	}
	pattern := "%" + strings.ToLower(term) + "%"
	return queryRows(s.db,
		`SELECT `+mediaColumns+` FROM media WHERE type = 'movie' AND LOWER(title) LIKE ? ORDER BY title COLLATE NOCASE, year`,
		pattern)
}

// CountMedia returns the number of rows for a given type.
func (s *Store) CountMedia(mediaType string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM media WHERE type = ?`, mediaType).Scan(&n)
	return n, err
}

// DeleteMedia removes a single row by id. Used by the scanner to evict
// movies whose source file is no longer present on disk.
func (s *Store) DeleteMedia(id string) error {
	_, err := s.db.Exec(`DELETE FROM media WHERE id = ?`, id)
	return err
}

const mediaColumns = `id, path, type, title, year, description, poster_path, backdrop_path,
	rating, tmdb_id, duration, video_codec, audio_codec, audio_count, needs_transcode, updated_at`

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanOne(row rowScanner) (MediaRow, error) {
	var m MediaRow
	err := row.Scan(&m.ID, &m.Path, &m.Type, &m.Title, &m.Year, &m.Description,
		&m.PosterPath, &m.BackdropPath, &m.Rating, &m.TMDbID, &m.Duration,
		&m.VideoCodec, &m.AudioCodec, &m.AudioCount, &m.NeedsTranscode, &m.UpdatedAt)
	return m, err
}

func queryRows(db *sql.DB, q string, args ...interface{}) ([]MediaRow, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaRow
	for rows.Next() {
		m, err := scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
