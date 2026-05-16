package server

import (
	"fmt"
	"strings"
)

// FormatDuration converts seconds to a human-readable "1h 47m" / "23m" string.
// Returns empty string for non-positive input so templates can {{ if }} on it.
func FormatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// FormatQuality joins resolution + video/audio codec info into a single string
// like "1080p · H.264 · AC3". Pass videoHeight=0 when unknown (resolution part
// is omitted). Returns empty string when nothing is known.
func FormatQuality(videoHeight int, videoCodec, audioCodec string) string {
	var parts []string
	switch {
	case videoHeight >= 1080:
		parts = append(parts, "1080p")
	case videoHeight >= 720:
		parts = append(parts, "720p")
	case videoHeight > 0:
		parts = append(parts, "SD")
	}
	switch strings.ToLower(videoCodec) {
	case "":
	case "h264":
		parts = append(parts, "H.264")
	case "hevc", "h265":
		parts = append(parts, "HEVC")
	default:
		parts = append(parts, strings.ToUpper(videoCodec))
	}
	switch strings.ToLower(audioCodec) {
	case "":
	case "ac3":
		parts = append(parts, "AC3")
	case "eac3":
		parts = append(parts, "E-AC3")
	case "aac":
		parts = append(parts, "AAC")
	case "dts":
		parts = append(parts, "DTS")
	default:
		parts = append(parts, strings.ToUpper(audioCodec))
	}
	return strings.Join(parts, " · ")
}

// RatingPercent maps a 0..10 TMDb rating to the 0..100 percentage that the
// ATV3 <starRating> widget expects. Clamps to [0, 100].
func RatingPercent(rating float64) int {
	if rating <= 0 {
		return 0
	}
	p := int(rating*10 + 0.5)
	if p > 100 {
		p = 100
	}
	return p
}
