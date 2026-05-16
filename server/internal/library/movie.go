package library

import (
	"crypto/sha1"
	"encoding/hex"
)

// Movie is a single movie file in the library.
type Movie struct {
	ID       string // stable hash of Path; safe for URLs
	Path     string // absolute filesystem path
	Title    string // parsed display title
	Year     int    // 0 if unknown
	Filename string // file name without extension (for diagnostics)
}

// movieID derives a 12-char hex id from a path.
func movieID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:])[:12]
}
