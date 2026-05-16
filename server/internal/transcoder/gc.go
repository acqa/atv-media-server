package transcoder

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CacheEntry is one transcoded movie directory under the cache root.
type CacheEntry struct {
	Name    string    // directory name (= movie id)
	Path    string    // absolute path
	Size    int64     // total bytes (playlist + segments)
	Touched time.Time // last access time of playlist.m3u8 (atime), fallback mtime
}

// ScanCache returns one entry per immediate subdirectory of root. Directories
// without a playlist.m3u8 are still listed but use the directory's mtime —
// they're "in-flight" or aborted and safe to remove.
func ScanCache(root string) ([]CacheEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []CacheEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		size, err := dirSize(dir)
		if err != nil {
			continue
		}
		touched := touchedAt(dir)
		out = append(out, CacheEntry{
			Name:    e.Name(),
			Path:    dir,
			Size:    size,
			Touched: touched,
		})
	}
	return out, nil
}

// TotalSize sums the Size fields. Convenience for callers that don't need
// the full slice.
func TotalSize(entries []CacheEntry) int64 {
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	return total
}

// GC removes the least-recently-touched entries under root until the total
// size is ≤ maxBytes. Returns the entries it removed and any error from the
// initial scan. Individual removal failures are silently skipped so a single
// permission issue doesn't stall the whole sweep.
//
// Mark playlist.m3u8 as "touched" on each request by calling Touch(path).
func GC(root string, maxBytes int64) ([]CacheEntry, error) {
	entries, err := ScanCache(root)
	if err != nil {
		return nil, err
	}
	if TotalSize(entries) <= maxBytes {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Touched.Before(entries[j].Touched)
	})
	var removed []CacheEntry
	total := TotalSize(entries)
	for _, e := range entries {
		if total <= maxBytes {
			break
		}
		if err := os.RemoveAll(e.Path); err != nil {
			continue
		}
		removed = append(removed, e)
		total -= e.Size
	}
	return removed, nil
}

// Touch updates the mtime of <dir>/playlist.m3u8 so the LRU GC sees this
// entry as recently used. Best-effort: errors are returned but callers
// typically log and ignore.
func Touch(dir string) error {
	now := time.Now()
	return os.Chtimes(filepath.Join(dir, "playlist.m3u8"), now, now)
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// touchedAt returns the most recent timestamp signalling activity in dir:
// the mtime of playlist.m3u8 if present, otherwise the directory's own mtime.
// We use mtime (not atime) because atime is unreliable on Linux mounts that
// use the relatime/noatime option.
func touchedAt(dir string) time.Time {
	if info, err := os.Stat(filepath.Join(dir, "playlist.m3u8")); err == nil {
		return info.ModTime()
	}
	if info, err := os.Stat(dir); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}
