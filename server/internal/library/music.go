package library

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// audioExts is the set of file extensions considered as music.
var audioExts = map[string]struct{}{
	".mp3":  {},
	".flac": {},
	".m4a":  {},
	".ogg":  {},
	".wav":  {},
}

// MusicArtist / MusicAlbum / MusicTrack are the scanner's output types,
// parallel to storage.* but without the SQL fields.
type MusicArtist struct {
	ID   string
	Name string
}

type MusicAlbum struct {
	ID        string
	ArtistID  string
	Title     string
	Year      int
	CoverPath string // local path to a cover image if present
}

type MusicTrack struct {
	ID       string
	AlbumID  string
	ArtistID string
	TrackNo  int
	Title    string
	Path     string
	Duration int // best-effort; 0 if tag library didn't report it
}

// trackFilenameRe captures track number and title from "01 - Title.ext" patterns.
var trackFilenameRe = regexp.MustCompile(`^\s*(\d{1,3})\s*[-._]\s*(.+)$`)

// ParseTrackFilename extracts (track_no, title) from a filename without extension.
// Falls back to (0, name) when the pattern doesn't match.
func ParseTrackFilename(name string) (int, string) {
	if m := trackFilenameRe.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, strings.TrimSpace(m[2])
	}
	return 0, strings.TrimSpace(name)
}

// ScanMusic walks <root>/<Artist>/<Album>/<track files>, reading ID3-style
// metadata via dhowden/tag. When tags are missing, falls back to directory
// names and filename track-number pattern.
//
// Returns (artists, albums, tracks) deduplicated by stable IDs.
func ScanMusic(root string) ([]MusicArtist, []MusicAlbum, []MusicTrack, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := os.Stat(absRoot); err != nil {
		return nil, nil, nil, nil
	}

	artists := map[string]MusicArtist{}
	albums := map[string]MusicAlbum{}
	var tracks []MusicTrack

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if name != "." && strings.HasPrefix(name, ".") {
			if d.IsDir() && path != absRoot {
				return fs.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := audioExts[ext]; !ok {
			return nil
		}

		// Expect <root>/<Artist>/<Album>/<track file>.
		albumDir := filepath.Dir(path)
		artistDir := filepath.Dir(albumDir)
		if artistDir == absRoot || filepath.Dir(artistDir) != absRoot {
			// Track sits at wrong depth — ignore. (We don't try to handle
			// "loose" tracks without an album.)
			return nil
		}
		artistDirName := filepath.Base(artistDir)
		albumDirName := filepath.Base(albumDir)
		baseName := strings.TrimSuffix(name, filepath.Ext(name))

		// Tags > directory names. tag.ReadFrom needs a Seeker, so open the file.
		var artistName, albumTitle, trackTitle string
		var trackNo, albumYear int
		if f, err := os.Open(path); err == nil {
			if m, err := tag.ReadFrom(f); err == nil {
				artistName = m.AlbumArtist()
				if artistName == "" {
					artistName = m.Artist()
				}
				albumTitle = m.Album()
				trackTitle = m.Title()
				trackNo, _ = m.Track()
				albumYear = m.Year()
			}
			_ = f.Close()
		}
		if artistName == "" {
			artistName = artistDirName
		}
		if albumTitle == "" {
			albumTitle = albumDirName
		}
		if trackTitle == "" || trackNo == 0 {
			fnNo, fnTitle := ParseTrackFilename(baseName)
			if trackNo == 0 {
				trackNo = fnNo
			}
			if trackTitle == "" {
				trackTitle = fnTitle
			}
		}

		artistID := musicArtistID(artistName)
		albumID := musicAlbumID(artistID, albumTitle)
		trackID := musicTrackID(path)

		if _, ok := artists[artistID]; !ok {
			artists[artistID] = MusicArtist{ID: artistID, Name: artistName}
		}
		if _, ok := albums[albumID]; !ok {
			al := MusicAlbum{ID: albumID, ArtistID: artistID, Title: albumTitle, Year: albumYear}
			// Look for cover.jpg / folder.jpg next to the tracks.
			for _, candidate := range []string{"cover.jpg", "folder.jpg", "cover.png", "folder.png"} {
				cp := filepath.Join(albumDir, candidate)
				if _, err := os.Stat(cp); err == nil {
					al.CoverPath = cp
					break
				}
			}
			albums[albumID] = al
		}
		tracks = append(tracks, MusicTrack{
			ID:       trackID,
			AlbumID:  albumID,
			ArtistID: artistID,
			TrackNo:  trackNo,
			Title:    trackTitle,
			Path:     path,
		})
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	artistsOut := make([]MusicArtist, 0, len(artists))
	for _, a := range artists {
		artistsOut = append(artistsOut, a)
	}
	albumsOut := make([]MusicAlbum, 0, len(albums))
	for _, a := range albums {
		albumsOut = append(albumsOut, a)
	}
	return artistsOut, albumsOut, tracks, nil
}

func musicArtistID(name string) string {
	sum := sha1.Sum([]byte("artist:" + strings.ToLower(strings.TrimSpace(name))))
	return hex.EncodeToString(sum[:])[:12]
}

func musicAlbumID(artistID, title string) string {
	sum := sha1.Sum([]byte("album:" + artistID + ":" + strings.ToLower(strings.TrimSpace(title))))
	return hex.EncodeToString(sum[:])[:12]
}

func musicTrackID(path string) string {
	sum := sha1.Sum([]byte("track:" + path))
	return hex.EncodeToString(sum[:])[:12]
}
