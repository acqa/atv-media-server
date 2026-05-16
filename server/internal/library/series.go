package library

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Series is a TV show as discovered on disk.
type Series struct {
	ID    string // sha1(absolute path of show dir)[:12]
	Path  string // absolute path of the show directory
	Title string
	Year  int
}

// Episode is one file in a Series.
type Episode struct {
	ID       string // sha1(absolute path of file)[:12]
	SeriesID string
	Path     string
	Season   int
	Episode  int
	Filename string
}

// episodePatterns are tried in order on the filename (without extension).
// First match wins. All patterns must capture (season, episode) groups.
var episodePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[Ss](\d{1,2})[Ee](\d{1,3})`),                              // S01E02, s1e2
	regexp.MustCompile(`(?i)(\d{1,2})x(\d{1,3})`),                                     // 1x02
	regexp.MustCompile(`(?i)Season[\s._-]*(\d{1,2})[\s._-]+Episode[\s._-]*(\d{1,3})`), // Season 1 Episode 2
}

// ParseEpisodeFile extracts (season, episode) from a file or directory name.
// Returns (0, 0, false) if no pattern matches.
func ParseEpisodeFile(name string) (season, episode int, ok bool) {
	for _, re := range episodePatterns {
		m := re.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		return s, e, true
	}
	return 0, 0, false
}

// seasonDirRe matches typical season directory names: "Season 1", "Season 02",
// "S1", "s01". Returns the season number or 0 if no match.
var seasonDirRe = regexp.MustCompile(`(?i)^season[\s._-]*(\d{1,2})$|^s(\d{1,2})$`)

// ParseSeasonDir extracts the season number from a directory name. Returns
// 0 when the directory doesn't look like a season folder — callers can then
// fall back to the number inside the episode file name.
func ParseSeasonDir(name string) int {
	m := seasonDirRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	for _, group := range m[1:] {
		if group != "" {
			n, _ := strconv.Atoi(group)
			return n
		}
	}
	return 0
}

// ScanSeries walks root, expecting <root>/<Show Name>/Season N/<episode file>.
// Files without a parseable season/episode are skipped. Shows with no recognised
// episodes are also skipped.
func ScanSeries(root string) ([]Series, []Episode, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(absRoot); err != nil {
		// Missing root is not an error — series/ may not exist yet.
		return nil, nil, nil
	}

	type bucket struct {
		series   Series
		episodes []Episode
	}
	byShow := map[string]*bucket{}

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

		filename := strings.TrimSuffix(name, filepath.Ext(name))
		seasonDir := filepath.Base(filepath.Dir(path))
		showDir := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if showDir == filepath.Base(absRoot) {
			return nil // depth too shallow: file directly under series/
		}

		season := ParseSeasonDir(seasonDir)
		fileSeason, fileEpisode, ok := ParseEpisodeFile(filename)
		if !ok {
			return nil
		}
		if season == 0 {
			season = fileSeason
		}
		if season == 0 {
			return nil
		}

		showPath := filepath.Dir(filepath.Dir(path))
		key := showPath
		b, found := byShow[key]
		if !found {
			title, year := ParseMovieName(showDir)
			if title == "" {
				title = cleanTitle(showDir)
			}
			b = &bucket{
				series: Series{
					ID:    seriesID(showPath),
					Path:  showPath,
					Title: title,
					Year:  year,
				},
			}
			byShow[key] = b
		}
		b.episodes = append(b.episodes, Episode{
			ID:       episodeID(path),
			SeriesID: b.series.ID,
			Path:     path,
			Season:   season,
			Episode:  fileEpisode,
			Filename: filename,
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	var seriesOut []Series
	var episodesOut []Episode
	for _, b := range byShow {
		seriesOut = append(seriesOut, b.series)
		episodesOut = append(episodesOut, b.episodes...)
	}
	return seriesOut, episodesOut, nil
}

func seriesID(path string) string {
	sum := sha1.Sum([]byte("series:" + path))
	return hex.EncodeToString(sum[:])[:12]
}

func episodeID(path string) string {
	sum := sha1.Sum([]byte("episode:" + path))
	return hex.EncodeToString(sum[:])[:12]
}
