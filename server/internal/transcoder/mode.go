package transcoder

import "strings"

// Mode is the strategy for turning a source file into HLS for ATV3.
type Mode int

const (
	// ModeRemux: copy video+audio into HLS containers without re-encoding.
	// Fast (~real-time × n) and lossless. Used when source already matches ATV3.
	ModeRemux Mode = iota
	// ModeTranscode: re-encode to H.264 High@4.1 + AAC stereo. Slow, CPU-bound.
	ModeTranscode
	// ModeAudioTranscode: video is fine for ATV3 but the audio codec isn't
	// (e.g. eac3 / DTS / FLAC). Copy video, re-encode only audio to AAC stereo.
	// Roughly 5-10× faster than a full transcode because the heavy lifting
	// (libx264) is skipped.
	ModeAudioTranscode
)

func (m Mode) String() string {
	switch m {
	case ModeRemux:
		return "remux"
	case ModeTranscode:
		return "transcode"
	case ModeAudioTranscode:
		return "audio-transcode"
	default:
		return "unknown"
	}
}

// ChooseMode decides whether a source can be remuxed straight to HLS, needs
// only an audio re-encode, or must be fully transcoded. Rules mirror ATV3's
// hardware decoder limits:
//
//	video: h264, profile in {Baseline, Main, High}, level ≤ 4.1
//	audio: aac or ac3 (NOT eac3 — see audioCompatible)
//
// Anything else — HEVC, AV1, H.264 above 4.1, unknown level — full transcode.
// If only audio is the problem (file has good video but eac3/DTS/FLAC audio),
// we skip the expensive video re-encode.
func ChooseMode(info StreamInfo) Mode {
	if !videoCompatible(info) {
		return ModeTranscode
	}
	if !audioCompatible(info) {
		return ModeAudioTranscode
	}
	return ModeRemux
}

func videoCompatible(info StreamInfo) bool {
	if info.VideoCodec != "h264" {
		return false
	}
	switch normalizeProfile(info.VideoProfile) {
	case "baseline", "main", "high":
		// ok
	default:
		return false
	}
	if info.VideoLevel <= 0 || info.VideoLevel > 41 {
		return false
	}
	return true
}

func audioCompatible(info StreamInfo) bool {
	// ATV3's AVPlayer (iOS 7-9) decodes AAC and AC-3 in HLS but silently
	// fails on Enhanced AC-3 (eac3 / Dolby Digital Plus). Files that ship
	// only eac3 audio need a transcode to AAC even if their video is fine.
	switch info.AudioCodec {
	case "aac", "ac3":
		return true
	}
	return false
}

// normalizeProfile lowercases the profile and strips extra qualifiers,
// e.g. "Constrained Baseline" → "baseline", "Main 10" → "main 10".
func normalizeProfile(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "constrained ")
	return s
}
