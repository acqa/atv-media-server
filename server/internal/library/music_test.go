package library

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
)

func TestParseTrackFilename(t *testing.T) {
	cases := []struct {
		in       string
		wantNo   int
		wantName string
	}{
		{"01 - Speak to Me", 1, "Speak to Me"},
		{"02_Breathe", 2, "Breathe"},
		{"3.Time", 3, "Time"},
		{"012 - The Great Gig", 12, "The Great Gig"},
		{"NoNumber", 0, "NoNumber"},
		{"", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			n, name := ParseTrackFilename(c.in)
			if n != c.wantNo || name != c.wantName {
				t.Errorf("ParseTrackFilename(%q) = (%d, %q), want (%d, %q)",
					c.in, n, name, c.wantNo, c.wantName)
			}
		})
	}
}

func TestScanMusic_DirectoryStructure(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Pink Floyd/The Dark Side of the Moon/01 - Speak to Me.mp3",
		"Pink Floyd/The Dark Side of the Moon/02 - Breathe.mp3",
		"Pink Floyd/The Dark Side of the Moon/cover.jpg",
		"Daft Punk/Discovery/01 - One More Time.flac",
		"Loose.mp3", // shallow — should be ignored
	})
	artists, albums, tracks, err := ScanMusic(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 2 || len(albums) != 2 || len(tracks) != 3 {
		t.Fatalf("counts: artists=%d albums=%d tracks=%d (%+v, %+v)",
			len(artists), len(albums), len(tracks), artists, albums)
	}

	// Pink Floyd album should pick up cover.jpg.
	for _, a := range albums {
		if a.Title == "The Dark Side of the Moon" {
			if a.CoverPath == "" {
				t.Errorf("expected cover.jpg detection, got empty")
			}
		}
	}

	// Track parsing: the two PF tracks should have track_no 1 and 2.
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].Title < tracks[j].Title })
	for _, tr := range tracks {
		if tr.TrackNo == 0 {
			t.Errorf("track_no not parsed for %s", tr.Title)
		}
	}
}

func TestScanMusic_MissingRoot(t *testing.T) {
	artists, albums, tracks, err := ScanMusic(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artists)+len(albums)+len(tracks) != 0 {
		t.Errorf("expected empty for missing root")
	}
}

func TestScanMusicAndUpsert_PopulatesStore(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Artist A/Album X/01 - Song.mp3",
		"Artist A/Album X/02 - Other.flac",
		"Artist B/Album Y/01 - Hit.m4a",
	})
	store := openStore(t)
	res, err := ScanMusicAndUpsert(context.Background(), root, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Artists != 2 || res.Albums != 2 || res.Tracks != 3 {
		t.Errorf("result: %+v", res)
	}
	artists, _ := store.ListArtists()
	if len(artists) != 2 {
		t.Errorf("artists: %d", len(artists))
	}
}

func TestScanMusicAndUpsert_DeduplicatesArtists(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Same Artist/Album One/01 - A.mp3",
		"Same Artist/Album Two/01 - B.mp3",
	})
	store := openStore(t)
	if _, err := ScanMusicAndUpsert(context.Background(), root, store, nil); err != nil {
		t.Fatal(err)
	}
	artists, _ := store.ListArtists()
	if len(artists) != 1 {
		t.Errorf("expected 1 artist, got %d", len(artists))
	}
	albums, _ := store.ListAlbumsByArtist(artists[0].ID)
	if len(albums) != 2 {
		t.Errorf("expected 2 albums, got %d", len(albums))
	}
}
