package library

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// mkTree creates a directory tree under root from a slice of relative file paths.
// Each entry creates parent dirs as needed and an empty file.
func mkTree(t *testing.T, root string, files []string) {
	t.Helper()
	for _, rel := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte{}, 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func TestScan_ParsesTitleFromDirectoryWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{
		"The Matrix (1999)/movie.mkv",
		"Inception (2010)/inception.1080p.mp4",
	})
	movies, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	sort.Slice(movies, func(i, j int) bool { return movies[i].Title < movies[j].Title })

	if len(movies) != 2 {
		t.Fatalf("want 2 movies, got %d: %+v", len(movies), movies)
	}
	if movies[0].Title != "Inception" || movies[0].Year != 2010 {
		t.Errorf("Inception: got %+v", movies[0])
	}
	if movies[1].Title != "The Matrix" || movies[1].Year != 1999 {
		t.Errorf("Matrix: got %+v", movies[1])
	}
}

func TestScan_FallsBackToFilenameWhenFlat(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{"Blade.Runner.1982.mp4"})
	movies, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(movies) != 1 {
		t.Fatalf("want 1 movie, got %d", len(movies))
	}
	if movies[0].Title != "Blade Runner" || movies[0].Year != 1982 {
		t.Errorf("got %+v", movies[0])
	}
}

func TestScan_RecognizedExtensionsOnly(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{
		"a.mp4",
		"b.mkv",
		"c.m4v",
		"d.mov",
		"e.avi",
		"readme.txt",
		"poster.jpg",
		"subs.srt",
	})
	movies, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(movies) != 5 {
		t.Fatalf("want 5 video files, got %d: %+v", len(movies), movies)
	}
}

func TestScan_SkipsHiddenEntries(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{
		"visible.mp4",
		".hidden.mp4",
		".trash/inside.mp4",
		"normal/film.mkv",
	})
	movies, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(movies) != 2 {
		t.Fatalf("want 2 visible movies, got %d: %+v", len(movies), movies)
	}
}

func TestScan_EmptyRoot(t *testing.T) {
	dir := t.TempDir()
	movies, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(movies) != 0 {
		t.Fatalf("want 0 movies, got %d", len(movies))
	}
}

func TestScan_StableIDsAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	mkTree(t, dir, []string{"X (2020)/x.mp4"})
	a, _ := Scan(dir)
	b, _ := Scan(dir)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected one movie each, got %d/%d", len(a), len(b))
	}
	if a[0].ID != b[0].ID {
		t.Errorf("IDs differ across runs: %q vs %q", a[0].ID, b[0].ID)
	}
}
