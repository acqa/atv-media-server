package transcoder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mkEntry creates a fake cache entry of given byte size with mtime baseline + offset.
func mkEntry(t *testing.T, root, name string, sizeBytes int, offsetSec int, baseline time.Time) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	playlist := filepath.Join(dir, "playlist.m3u8")
	if err := os.WriteFile(playlist, make([]byte, sizeBytes), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := baseline.Add(time.Duration(offsetSec) * time.Second)
	if err := os.Chtimes(playlist, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScanCache_EmptyRoot(t *testing.T) {
	entries, err := ScanCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("want empty, got %d", len(entries))
	}
}

func TestScanCache_MissingRootReturnsNil(t *testing.T) {
	entries, err := ScanCache(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if entries != nil {
		t.Errorf("want nil entries, got %v", entries)
	}
}

func TestScanCache_ListsSubdirsWithSize(t *testing.T) {
	root := t.TempDir()
	base := time.Now()
	mkEntry(t, root, "abc", 100, 0, base)
	mkEntry(t, root, "def", 250, 0, base)

	entries, err := ScanCache(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	total := TotalSize(entries)
	if total != 350 {
		t.Errorf("TotalSize: want 350, got %d", total)
	}
}

func TestGC_NoEvictionWhenUnderLimit(t *testing.T) {
	root := t.TempDir()
	base := time.Now()
	mkEntry(t, root, "a", 100, 0, base)
	mkEntry(t, root, "b", 100, 60, base)

	removed, err := GC(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
	// Both directories remain.
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Errorf("dirs remaining: %d", len(entries))
	}
}

func TestGC_EvictsOldestUntilUnderLimit(t *testing.T) {
	root := t.TempDir()
	base := time.Now()
	// 3 entries × 100 bytes = 300 total. Eviction limit 200 → removes one oldest.
	mkEntry(t, root, "oldest", 100, 0, base)
	mkEntry(t, root, "middle", 100, 60, base)
	mkEntry(t, root, "newest", 100, 120, base)

	removed, err := GC(root, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("want 1 removed, got %d", len(removed))
	}
	if removed[0].Name != "oldest" {
		t.Errorf("want oldest removed, got %s", removed[0].Name)
	}
	if _, err := os.Stat(filepath.Join(root, "oldest")); !os.IsNotExist(err) {
		t.Error("oldest still on disk")
	}
	for _, name := range []string{"middle", "newest"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s should remain: %v", name, err)
		}
	}
}

func TestGC_EvictsMultipleWhenWayOverLimit(t *testing.T) {
	root := t.TempDir()
	base := time.Now()
	for i, name := range []string{"e1", "e2", "e3", "e4"} {
		mkEntry(t, root, name, 100, i*60, base)
	}
	// Total 400 bytes, limit 50 → remove all but the newest (oldest go first).
	removed, err := GC(root, 50)
	if err != nil {
		t.Fatal(err)
	}
	// We must remove at least 350 bytes (4×100 − 50). Since each is 100,
	// that means at least 4 removed actually — wait, 400-50=350 bytes,
	// each removal frees 100, so we need at least 4 removals → all of them.
	if len(removed) != 4 {
		t.Errorf("expected 4 removed, got %d", len(removed))
	}
}

func TestTouch_UpdatesPlaylistMtime(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-time.Hour)
	dir := mkEntry(t, root, "a", 100, 0, base)

	// Sanity: mtime should be in the past.
	info, _ := os.Stat(filepath.Join(dir, "playlist.m3u8"))
	if time.Since(info.ModTime()) < 30*time.Minute {
		t.Fatalf("setup mtime not in past: %v", info.ModTime())
	}

	if err := Touch(dir); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(filepath.Join(dir, "playlist.m3u8"))
	if time.Since(info.ModTime()) > 5*time.Second {
		t.Errorf("Touch did not update mtime, current age: %v", time.Since(info.ModTime()))
	}
}

func TestGC_TouchProtectsRecentlyUsed(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-time.Hour)
	mkEntry(t, root, "a", 100, 0, base)   // oldest by default
	mkEntry(t, root, "b", 100, 60, base)  // middle
	mkEntry(t, root, "c", 100, 120, base) // newest by default
	// Touch 'a' so it becomes newest.
	if err := Touch(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	// Total 300, limit 200 → evict exactly one (the now-oldest 'b').
	removed, err := GC(root, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].Name != "b" {
		t.Errorf("expected b evicted, got %+v", removed)
	}
}
