package library

import (
	"sort"
	"strings"
	"sync"
)

// Library is an in-memory catalogue of movies, safe for concurrent use.
type Library struct {
	mu     sync.RWMutex
	movies map[string]Movie
}

// NewLibrary returns an empty Library.
func NewLibrary() *Library {
	return &Library{movies: make(map[string]Movie)}
}

// Replace atomically swaps the catalogue with movies indexed by ID.
// Later duplicates (same ID) overwrite earlier ones — unlikely but
// possible if two paths hash to the same prefix; treat as last-write-wins.
func (l *Library) Replace(movies []Movie) {
	next := make(map[string]Movie, len(movies))
	for _, m := range movies {
		next[m.ID] = m
	}
	l.mu.Lock()
	l.movies = next
	l.mu.Unlock()
}

// Get returns a movie by id and whether it exists.
func (l *Library) Get(id string) (Movie, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	m, ok := l.movies[id]
	return m, ok
}

// All returns a snapshot of all movies sorted by (Title, Year).
func (l *Library) All() []Movie {
	l.mu.RLock()
	out := make([]Movie, 0, len(l.movies))
	for _, m := range l.movies {
		out = append(out, m)
	}
	l.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		ti := strings.ToLower(out[i].Title)
		tj := strings.ToLower(out[j].Title)
		if ti != tj {
			return ti < tj
		}
		return out[i].Year < out[j].Year
	})
	return out
}

// Count returns the number of movies currently loaded.
func (l *Library) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.movies)
}

// ScanInto runs Scan(root) and replaces the library with the result.
func (l *Library) ScanInto(root string) error {
	movies, err := Scan(root)
	if err != nil {
		return err
	}
	l.Replace(movies)
	return nil
}
