package library

import (
	"context"
	"time"

	"github.com/atv-media-server/server/internal/storage"
)

// MusicScanResult summarises a music tree pass.
type MusicScanResult struct {
	Artists int
	Albums  int
	Tracks  int
}

// ScanMusicAndUpsert walks root and upserts artists/albums/tracks. No TMDb —
// music metadata stays local (Phase 4 plan: MusicBrainz could be a follow-up).
func ScanMusicAndUpsert(ctx context.Context, root string, store *storage.Store, log Logger) (MusicScanResult, error) {
	if log == nil {
		log = func(string, ...interface{}) {}
	}
	artists, albums, tracks, err := ScanMusic(root)
	if err != nil {
		return MusicScanResult{}, err
	}
	res := MusicScanResult{Artists: len(artists), Albums: len(albums), Tracks: len(tracks)}

	now := time.Now().UTC()
	for _, a := range artists {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := store.UpsertArtist(storage.ArtistRow{ID: a.ID, Name: a.Name, UpdatedAt: now}); err != nil {
			log("upsert artist %s: %v", a.ID, err)
		}
	}
	for _, al := range albums {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := store.UpsertAlbum(storage.AlbumRow{
			ID: al.ID, ArtistID: al.ArtistID, Title: al.Title, Year: al.Year,
			CoverPath: al.CoverPath, UpdatedAt: now,
		}); err != nil {
			log("upsert album %s: %v", al.ID, err)
		}
	}
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := store.UpsertTrack(storage.TrackRow{
			ID: t.ID, AlbumID: t.AlbumID, ArtistID: t.ArtistID,
			TrackNo: t.TrackNo, Title: t.Title, Path: t.Path,
			Duration: t.Duration, UpdatedAt: now,
		}); err != nil {
			log("upsert track %s: %v", t.ID, err)
		}
	}
	log("scan music: artists=%d albums=%d tracks=%d", res.Artists, res.Albums, res.Tracks)
	return res, nil
}
