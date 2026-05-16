package library

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestParseEpisodeFile(t *testing.T) {
	cases := []struct {
		in           string
		wantS, wantE int
		wantOk       bool
	}{
		{"S01E02", 1, 2, true},
		{"s1e2", 1, 2, true},
		{"Show.S03E12.1080p", 3, 12, true},
		{"3x12", 3, 12, true},
		{"Show 1x02 720p", 1, 2, true},
		{"Season 4 Episode 7", 4, 7, true},
		{"season_2_episode_15", 2, 15, true},
		{"S10E100", 10, 100, true},
		{"NoEpisodeHere", 0, 0, false},
		{"", 0, 0, false},
		{"720p", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			s, e, ok := ParseEpisodeFile(c.in)
			if s != c.wantS || e != c.wantE || ok != c.wantOk {
				t.Errorf("ParseEpisodeFile(%q) = (%d, %d, %v), want (%d, %d, %v)",
					c.in, s, e, ok, c.wantS, c.wantE, c.wantOk)
			}
		})
	}
}

func TestParseSeasonDir(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Season 1", 1},
		{"Season 02", 2},
		{"season 10", 10},
		{"S1", 1},
		{"s01", 1},
		{"Season.1", 1},
		{"season_03", 3},
		{"SeasonX", 0},
		{"Specials", 0},
		{"Season", 0},
		{"", 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := ParseSeasonDir(c.in)
			if got != c.want {
				t.Errorf("ParseSeasonDir(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestScanSeries_FindsShowsAndEpisodes(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Breaking Bad/Season 1/S01E01 Pilot.mkv",
		"Breaking Bad/Season 1/breaking-bad-S01E02.mkv",
		"Breaking Bad/Season 2/S02E01.mp4",
		"Doctor Who (2005)/Season 1/Doctor.Who.S01E01.mkv",
		"NotASeries.mkv", // shallow; should be ignored
	})

	series, episodes, err := ScanSeries(root)
	if err != nil {
		t.Fatalf("ScanSeries: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("series: want 2, got %d (%+v)", len(series), series)
	}
	if len(episodes) != 4 {
		t.Fatalf("episodes: want 4, got %d (%+v)", len(episodes), episodes)
	}

	byTitle := map[string]Series{}
	for _, s := range series {
		byTitle[s.Title] = s
	}
	bb, ok := byTitle["Breaking Bad"]
	if !ok {
		t.Fatalf("missing Breaking Bad: %+v", byTitle)
	}
	dw, ok := byTitle["Doctor Who"]
	if !ok {
		t.Fatalf("missing Doctor Who: %+v", byTitle)
	}
	if dw.Year != 2005 {
		t.Errorf("Doctor Who year: want 2005, got %d", dw.Year)
	}

	var bbEpisodes []Episode
	for _, e := range episodes {
		if e.SeriesID == bb.ID {
			bbEpisodes = append(bbEpisodes, e)
		}
	}
	if len(bbEpisodes) != 3 {
		t.Errorf("BB episodes: want 3, got %d", len(bbEpisodes))
	}
	sort.Slice(bbEpisodes, func(i, j int) bool {
		if bbEpisodes[i].Season != bbEpisodes[j].Season {
			return bbEpisodes[i].Season < bbEpisodes[j].Season
		}
		return bbEpisodes[i].Episode < bbEpisodes[j].Episode
	})
	want := []struct{ s, e int }{{1, 1}, {1, 2}, {2, 1}}
	for i, w := range want {
		if bbEpisodes[i].Season != w.s || bbEpisodes[i].Episode != w.e {
			t.Errorf("[%d] got S%dE%d, want S%dE%d",
				i, bbEpisodes[i].Season, bbEpisodes[i].Episode, w.s, w.e)
		}
	}
}

func TestScanSeries_IgnoresUnparseableFiles(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Some Show/Season 1/RandomFile.mkv",  // no SxxExx
		"Some Show/Season 1/S01E01.mkv",      // valid
		"Some Show/Specials/episode_one.mkv", // unparseable season
	})
	series, episodes, err := ScanSeries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	if len(episodes) != 1 {
		t.Fatalf("want 1 episode, got %d", len(episodes))
	}
}

func TestScanSeries_DerivesSeasonFromFilenameWhenDirIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{
		"Show/Stuff/Show.S03E04.mkv", // "Stuff" is not a season dir; season comes from filename
	})
	_, episodes, err := ScanSeries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("want 1 episode, got %d", len(episodes))
	}
	if episodes[0].Season != 3 || episodes[0].Episode != 4 {
		t.Errorf("got S%dE%d, want S3E4", episodes[0].Season, episodes[0].Episode)
	}
}

func TestScanSeries_MissingRootReturnsEmpty(t *testing.T) {
	series, episodes, err := ScanSeries(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if len(series) != 0 || len(episodes) != 0 {
		t.Errorf("expected empty: %+v / %+v", series, episodes)
	}
}

func TestScanSeries_StableIDs(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, []string{"Show/Season 1/S01E01.mkv"})
	a, _, _ := ScanSeries(root)
	b, _, _ := ScanSeries(root)
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID {
		t.Errorf("series IDs unstable across runs")
	}
}
