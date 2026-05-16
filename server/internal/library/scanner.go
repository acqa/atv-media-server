package library

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// videoExts is the set of file extensions considered as movies.
var videoExts = map[string]struct{}{
	".mp4": {},
	".mkv": {},
	".m4v": {},
	".mov": {},
	".avi": {},
}

// Scan walks root and returns one Movie per video file found.
// Hidden entries (dot-prefixed names) are skipped, including directories.
// Title and year are derived preferentially from the parent directory name
// (when it differs from root) — that's typically the canonical "Movie (Year)" folder.
func Scan(root string) ([]Movie, error) {
	var movies []Movie
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
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
		if _, ok := videoExts[ext]; !ok {
			return nil
		}
		movies = append(movies, makeMovie(absRoot, path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return movies, nil
}

func makeMovie(root, path string) Movie {
	filename := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parent := filepath.Base(filepath.Dir(path))

	source := filename
	if parent != "" && filepath.Dir(path) != root {
		source = parent
	}
	title, year := ParseMovieName(source)
	if title == "" {
		title = cleanTitle(filename)
	}
	return Movie{
		ID:       movieID(path),
		Path:     path,
		Title:    title,
		Year:     year,
		Filename: filename,
	}
}
