package storage

import (
	"errors"
	"time"
)

// ArtistRow / AlbumRow / TrackRow are the three music tables.
type ArtistRow struct {
	ID        string
	Name      string
	UpdatedAt time.Time
}

type AlbumRow struct {
	ID        string
	ArtistID  string
	Title     string
	Year      int
	CoverPath string
	UpdatedAt time.Time
}

type TrackRow struct {
	ID        string
	AlbumID   string
	ArtistID  string
	TrackNo   int
	Title     string
	Path      string
	Duration  int
	UpdatedAt time.Time
}

// UpsertArtist inserts or updates an artist.
func (s *Store) UpsertArtist(r ArtistRow) error {
	if r.ID == "" || r.Name == "" {
		return errors.New("ArtistRow: ID, Name required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO artists (id, name, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, updated_at = excluded.updated_at
	`, r.ID, r.Name, r.UpdatedAt)
	return err
}

// UpsertAlbum inserts or updates an album.
func (s *Store) UpsertAlbum(r AlbumRow) error {
	if r.ID == "" || r.ArtistID == "" || r.Title == "" {
		return errors.New("AlbumRow: ID, ArtistID, Title required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO albums (id, artist_id, title, year, cover_path, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			artist_id = excluded.artist_id,
			title = excluded.title,
			year = excluded.year,
			cover_path = excluded.cover_path,
			updated_at = excluded.updated_at
	`, r.ID, r.ArtistID, r.Title, r.Year, r.CoverPath, r.UpdatedAt)
	return err
}

// UpsertTrack inserts or updates a track.
func (s *Store) UpsertTrack(r TrackRow) error {
	if r.ID == "" || r.AlbumID == "" || r.ArtistID == "" || r.Path == "" {
		return errors.New("TrackRow: ID, AlbumID, ArtistID, Path required")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO tracks (id, album_id, artist_id, track_no, title, path, duration, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			album_id = excluded.album_id,
			artist_id = excluded.artist_id,
			track_no = excluded.track_no,
			title = excluded.title,
			path = excluded.path,
			duration = excluded.duration,
			updated_at = excluded.updated_at
	`, r.ID, r.AlbumID, r.ArtistID, r.TrackNo, r.Title, r.Path, r.Duration, r.UpdatedAt)
	return err
}

// ListArtists returns all artists alphabetically.
func (s *Store) ListArtists() ([]ArtistRow, error) {
	rows, err := s.db.Query(`SELECT id, name, updated_at FROM artists ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ArtistRow
	for rows.Next() {
		var r ArtistRow
		if err := rows.Scan(&r.ID, &r.Name, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAlbumsByArtist returns albums of one artist, sorted by year.
func (s *Store) ListAlbumsByArtist(artistID string) ([]AlbumRow, error) {
	rows, err := s.db.Query(`SELECT id, artist_id, title, year, cover_path, updated_at
		FROM albums WHERE artist_id = ? ORDER BY year, title COLLATE NOCASE`, artistID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AlbumRow
	for rows.Next() {
		var r AlbumRow
		if err := rows.Scan(&r.ID, &r.ArtistID, &r.Title, &r.Year, &r.CoverPath, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetAlbum returns a single album by id.
func (s *Store) GetAlbum(id string) (AlbumRow, error) {
	row := s.db.QueryRow(`SELECT id, artist_id, title, year, cover_path, updated_at FROM albums WHERE id = ?`, id)
	var r AlbumRow
	err := row.Scan(&r.ID, &r.ArtistID, &r.Title, &r.Year, &r.CoverPath, &r.UpdatedAt)
	return r, err
}

// GetArtist returns a single artist by id.
func (s *Store) GetArtist(id string) (ArtistRow, error) {
	row := s.db.QueryRow(`SELECT id, name, updated_at FROM artists WHERE id = ?`, id)
	var r ArtistRow
	err := row.Scan(&r.ID, &r.Name, &r.UpdatedAt)
	return r, err
}

// ListTracksByAlbum returns tracks of one album, sorted by track_no.
func (s *Store) ListTracksByAlbum(albumID string) ([]TrackRow, error) {
	rows, err := s.db.Query(`SELECT id, album_id, artist_id, track_no, title, path, duration, updated_at
		FROM tracks WHERE album_id = ? ORDER BY track_no, title COLLATE NOCASE`, albumID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TrackRow
	for rows.Next() {
		var r TrackRow
		if err := rows.Scan(&r.ID, &r.AlbumID, &r.ArtistID, &r.TrackNo, &r.Title, &r.Path, &r.Duration, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountTracks returns the total number of tracks in the library.
func (s *Store) CountTracks() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&n)
	return n, err
}

// GetTrack returns a single track by id.
func (s *Store) GetTrack(id string) (TrackRow, error) {
	row := s.db.QueryRow(`SELECT id, album_id, artist_id, track_no, title, path, duration, updated_at
		FROM tracks WHERE id = ?`, id)
	var r TrackRow
	err := row.Scan(&r.ID, &r.AlbumID, &r.ArtistID, &r.TrackNo, &r.Title, &r.Path, &r.Duration, &r.UpdatedAt)
	return r, err
}
