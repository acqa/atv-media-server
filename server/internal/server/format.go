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

// FormatQuality joins resolution + video/audio codec + channel layout into a
// single string like "1080p · H.264 · AC3 5.1". Any of the inputs may be zero/
// empty when unknown — corresponding parts are omitted. Returns empty when
// nothing is known.
//
// audioChannels is rendered as "5.1"/"7.1"/"Mono" — Stereo (2) is implicit and
// stays unshown to avoid noise.
func FormatQuality(videoHeight int, videoCodec, audioCodec string, audioChannels int) string {
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
	if audio := formatAudio(audioCodec, audioChannels); audio != "" {
		parts = append(parts, audio)
	}
	return strings.Join(parts, " · ")
}

// formatAudio combines codec + channel layout, e.g. "AC3 5.1", "AAC", "5.1".
func formatAudio(codec string, channels int) string {
	var codecLabel string
	switch strings.ToLower(codec) {
	case "":
	case "ac3":
		codecLabel = "AC3"
	case "eac3":
		codecLabel = "E-AC3"
	case "aac":
		codecLabel = "AAC"
	case "dts":
		codecLabel = "DTS"
	default:
		codecLabel = strings.ToUpper(codec)
	}
	var chLabel string
	switch {
	case channels >= 8:
		chLabel = "7.1"
	case channels >= 6:
		chLabel = "5.1"
	case channels == 1:
		chLabel = "Mono"
	}
	switch {
	case codecLabel != "" && chLabel != "":
		return codecLabel + " " + chLabel
	case codecLabel != "":
		return codecLabel
	default:
		return chLabel
	}
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
