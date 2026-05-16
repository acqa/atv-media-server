package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func sampleSeries(id, title string, year int) SeriesRow {
	return SeriesRow{
		ID: id, Path: "/series/" + id, Title: title, Year: year,
		PosterPath: "/poster.jpg", TMDbID: 1396,
	}
}

func sampleEpisode(id, sid string, season, ep int) EpisodeRow {
	return EpisodeRow{
		ID: id, SeriesID: sid, Season: season, Episode: ep,
		Path: "/series/" + sid + "/" + id + ".mkv",
	}
}

func TestSeries_UpsertRoundTrip(t *testing.T) {
	s := openTemp(t)
	in := SeriesRow{
		ID: "a", Path: "/p", Title: "Breaking Bad", Year: 2008,
		Description: "desc", PosterPath: "/p.jpg", BackdropPath: "/b.jpg",
		Rating: 8.9, TMDbID: 1396,
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err := s.UpsertSeries(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSeries("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != in.Title || got.Year != in.Year || got.TMDbID != in.TMDbID || got.Rating != in.Rating {
		t.Errorf("round-trip mismatch:\n  in:  %+v\n  got: %+v", in, got)
	}
}

func TestSeries_GetMissing(t *testing.T) {
	s := openTemp(t)
	_, err := s.GetSeries("nope")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want ErrNoRows, got %v", err)
	}
}

func TestSeries_ListSortedCaseInsensitive(t *testing.T) {
	s := openTemp(t)
	for _, sr := range []SeriesRow{
		sampleSeries("z", "Zorro", 1998),
		sampleSeries("a", "Alpha", 2000),
		sampleSeries("b", "beta", 2005),
	} {
		if err := s.UpsertSeries(sr); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.ListSeries()
	want := []string{"Alpha", "beta", "Zorro"}
	for i, w := range want {
		if got[i].Title != w {
			t.Errorf("[%d] %q != %q", i, got[i].Title, w)
		}
	}
}

func TestEpisode_UpsertAndListBySeries(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertSeries(sampleSeries("show1", "Show", 2020)); err != nil {
		t.Fatal(err)
	}
	for _, e := range []EpisodeRow{
		sampleEpisode("e2", "show1", 2, 1),
		sampleEpisode("e1a", "show1", 1, 1),
		sampleEpisode("e1b", "show1", 1, 2),
	} {
		if err := s.UpsertEpisode(e); err != nil {
			t.Fatalf("upsert %s: %v", e.ID, err)
		}
	}
	all, err := s.ListEpisodesBySeries("show1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 episodes, got %d", len(all))
	}
	want := []struct{ s, e int }{{1, 1}, {1, 2}, {2, 1}}
	for i, w := range want {
		if all[i].Season != w.s || all[i].Episode != w.e {
			t.Errorf("[%d] got S%dE%d, want S%dE%d",
				i, all[i].Season, all[i].Episode, w.s, w.e)
		}
	}
}

func TestEpisode_ListBySeason(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertSeries(sampleSeries("sh", "Show", 0))
	_ = s.UpsertEpisode(sampleEpisode("e1", "sh", 1, 1))
	_ = s.UpsertEpisode(sampleEpisode("e2", "sh", 1, 2))
	_ = s.UpsertEpisode(sampleEpisode("e3", "sh", 2, 1))

	s1, _ := s.ListEpisodesBySeason("sh", 1)
	if len(s1) != 2 {
		t.Errorf("season 1: want 2, got %d", len(s1))
	}
	s2, _ := s.ListEpisodesBySeason("sh", 2)
	if len(s2) != 1 {
		t.Errorf("season 2: want 1, got %d", len(s2))
	}
}

func TestEpisode_ListSeasons(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertSeries(sampleSeries("sh", "Show", 0))
	_ = s.UpsertEpisode(sampleEpisode("e1", "sh", 1, 1))
	_ = s.UpsertEpisode(sampleEpisode("e2", "sh", 2, 1))
	_ = s.UpsertEpisode(sampleEpisode("e3", "sh", 5, 1))
	got, _ := s.ListSeasonsOfSeries("sh")
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 5 {
		t.Errorf("seasons: %v", got)
	}
}

func TestEpisode_RejectsBadInputs(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertSeries(sampleSeries("sh", "Show", 0))
	cases := []EpisodeRow{
		{SeriesID: "sh", Season: 1, Episode: 1, Path: "/p"},
		{ID: "x", Season: 1, Episode: 1, Path: "/p"},
		{ID: "x", SeriesID: "sh", Season: 1, Episode: 1},
		{ID: "x", SeriesID: "sh", Season: 0, Episode: 1, Path: "/p"},
		{ID: "x", SeriesID: "sh", Season: 1, Episode: 0, Path: "/p"},
	}
	for _, c := range cases {
		if err := s.UpsertEpisode(c); err == nil {
			t.Errorf("expected error for %+v", c)
		}
	}
}

func TestEpisode_CascadeOnSeriesDelete(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertSeries(sampleSeries("sh", "Show", 0))
	_ = s.UpsertEpisode(sampleEpisode("e1", "sh", 1, 1))
	// Manual delete since we don't expose a DeleteSeries method yet.
	if _, err := s.db.Exec(`DELETE FROM series WHERE id = ?`, "sh"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListEpisodesBySeries("sh")
	if len(got) != 0 {
		t.Errorf("cascade did not delete episodes: %+v", got)
	}
}

func TestEpisode_UniqueOnSeriesIDSeasonEpisode(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertSeries(sampleSeries("sh", "Show", 0))
	_ = s.UpsertEpisode(sampleEpisode("e1", "sh", 1, 1))
	// Inserting a different ID but the same (series_id, season, episode) violates UNIQUE.
	err := s.UpsertEpisode(EpisodeRow{
		ID: "e2", SeriesID: "sh", Season: 1, Episode: 1, Path: "/other.mkv",
	})
	if err == nil {
		t.Error("expected UNIQUE constraint violation")
	}
}
