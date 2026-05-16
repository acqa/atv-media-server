package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleMovie(id, title string, year int) MediaRow {
	return MediaRow{
		ID:    id,
		Path:  "/m/" + id + ".mkv",
		Type:  "movie",
		Title: title,
		Year:  year,
	}
}

func TestOpen_FreshDBAppliesMigrations(t *testing.T) {
	s := openTemp(t)
	// schema_version should be populated and media table queryable.
	var v int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("schema_version: %v", err)
	}
	if v < 1 {
		t.Errorf("expected schema_version >= 1, got %d", v)
	}
	if _, err := s.db.Exec(`INSERT INTO media (id, path, type, title, updated_at) VALUES ('x','y','movie','t',?)`, time.Now()); err != nil {
		t.Fatalf("media table not usable: %v", err)
	}
}

func TestOpen_Reopen_IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestUpsertAndGetMedia_RoundTrip(t *testing.T) {
	s := openTemp(t)
	in := MediaRow{
		ID: "abc", Path: "/m/a.mkv", Type: "movie", Title: "Alpha",
		Year: 1999, Description: "desc", PosterPath: "/p.jpg",
		BackdropPath: "/b.jpg", Rating: 7.5, TMDbID: 42, Duration: 6000,
		VideoCodec: "h264", AudioCodec: "aac", NeedsTranscode: true,
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err := s.UpsertMedia(in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.GetMedia("abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != in.Title || got.Year != in.Year || got.Description != in.Description ||
		got.PosterPath != in.PosterPath || got.BackdropPath != in.BackdropPath ||
		got.Rating != in.Rating || got.TMDbID != in.TMDbID || got.Duration != in.Duration ||
		got.VideoCodec != in.VideoCodec || got.AudioCodec != in.AudioCodec ||
		got.NeedsTranscode != in.NeedsTranscode {
		t.Errorf("round-trip mismatch:\n  in:  %+v\n  got: %+v", in, got)
	}
	if !got.UpdatedAt.Equal(in.UpdatedAt) {
		t.Errorf("updated_at: want %v, got %v", in.UpdatedAt, got.UpdatedAt)
	}
}

func TestMigration002_NeedsTranscodeDefaultsFalse(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertMedia(MediaRow{
		ID: "x", Path: "/x.mkv", Type: "movie", Title: "X",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMedia("x")
	if err != nil {
		t.Fatal(err)
	}
	if got.NeedsTranscode {
		t.Errorf("NeedsTranscode default: want false, got true")
	}
}

func TestUpsertMedia_UpdatesExisting(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertMedia(sampleMovie("abc", "Old Title", 2000)); err != nil {
		t.Fatal(err)
	}
	upd := sampleMovie("abc", "New Title", 2001)
	upd.Description = "new"
	if err := s.UpsertMedia(upd); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMedia("abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Title" || got.Year != 2001 || got.Description != "new" {
		t.Errorf("update did not apply: %+v", got)
	}
	n, _ := s.CountMedia("movie")
	if n != 1 {
		t.Errorf("expected 1 row after upsert, got %d", n)
	}
}

func TestGetMedia_Missing_ReturnsErrNoRows(t *testing.T) {
	s := openTemp(t)
	_, err := s.GetMedia("missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("want ErrNoRows, got %v", err)
	}
}

func TestUpsertMedia_RejectsEmptyKeyFields(t *testing.T) {
	s := openTemp(t)
	cases := []MediaRow{
		{Path: "/x", Type: "movie", Title: "t"},
		{ID: "x", Type: "movie", Title: "t"},
		{ID: "x", Path: "/x", Title: "t"},
		{ID: "x", Path: "/x", Type: "movie"},
	}
	for _, c := range cases {
		if err := s.UpsertMedia(c); err == nil {
			t.Errorf("expected error for %+v", c)
		}
	}
}

func TestListMovies_SortedCaseInsensitive(t *testing.T) {
	s := openTemp(t)
	for _, m := range []MediaRow{
		sampleMovie("c", "zorro", 1998),
		sampleMovie("a", "Alpha", 2000),
		sampleMovie("b", "alpha", 2010), // same title, later year sorts after
		sampleMovie("d", "Beta", 2005),
	} {
		if err := s.UpsertMedia(m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListMovies()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4, got %d", len(got))
	}
	want := []string{"a", "b", "d", "c"}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("[%d] want %q got %q (order: %v)", i, w, got[i].ID, idsOf(got))
		}
	}
}

func TestSearchMovies_CaseInsensitiveLike(t *testing.T) {
	s := openTemp(t)
	for _, m := range []MediaRow{
		sampleMovie("1", "The Matrix", 1999),
		sampleMovie("2", "Inception", 2010),
		sampleMovie("3", "The Matrix Reloaded", 2003),
	} {
		_ = s.UpsertMedia(m)
	}

	got, err := s.SearchMovies("matrix")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("matrix search: want 2 results, got %d", len(got))
	}

	got, _ = s.SearchMovies("INCEP")
	if len(got) != 1 || got[0].Title != "Inception" {
		t.Errorf("uppercase search: %v", got)
	}

	got, _ = s.SearchMovies("nothing-here")
	if len(got) != 0 {
		t.Errorf("no-match search: want 0, got %d", len(got))
	}

	got, _ = s.SearchMovies("") // empty = all
	if len(got) != 3 {
		t.Errorf("empty search: want 3 (all), got %d", len(got))
	}
}

func TestCountMedia(t *testing.T) {
	s := openTemp(t)
	_ = s.UpsertMedia(sampleMovie("a", "A", 0))
	_ = s.UpsertMedia(sampleMovie("b", "B", 0))
	n, err := s.CountMedia("movie")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want 2 movies, got %d", n)
	}
	n, _ = s.CountMedia("episode")
	if n != 0 {
		t.Errorf("want 0 episodes, got %d", n)
	}
}

func idsOf(rows []MediaRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
