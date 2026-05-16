package server

import "strings"

// BadgeFilenames returns the ordered list of PNG filenames (relative to
// /assets/badges/) that visually summarise the given stream. The order is:
// resolution → video codec → audio codec → channel count. Unknown or
// unrecognised fields are skipped silently.
//
// Filenames mirror the PNG set bundled under assets/badges/ (which was sourced
// from PlexConnect — see THIRD_PARTY_LICENSES.md). Anything we can't map to a
// known badge is dropped: callers should treat an empty result as "render no
// <mediaBadges>".
func BadgeFilenames(videoHeight int, videoCodec, audioCodec string, audioChannels int) []string {
	var out []string

	if name := resolutionBadge(videoHeight); name != "" {
		out = append(out, name)
	}
	if name := videoCodecBadge(videoCodec); name != "" {
		out = append(out, name)
	}
	if name := audioCodecBadge(audioCodec); name != "" {
		out = append(out, name)
	}
	if name := channelBadge(audioChannels); name != "" {
		out = append(out, name)
	}
	return out
}

func resolutionBadge(h int) string {
	switch {
	case h >= 2160:
		return "4k.png"
	case h >= 1440:
		return "2k.png"
	case h >= 1080:
		return "1080.png"
	case h >= 720:
		return "720.png"
	case h > 0:
		return "sd.png"
	}
	return ""
}

func videoCodecBadge(codec string) string {
	switch strings.ToLower(codec) {
	case "h264":
		return "h264.png"
	case "hevc", "h265":
		return "hevc.png"
	case "mpeg1video":
		return "mpeg1video.png"
	case "mpeg2video":
		return "mpeg2video.png"
	case "mpeg4":
		return "mpeg.png"
	case "xvid":
		return "xvid.png"
	case "wmv1":
		return "wmv1.png"
	case "wmv2":
		return "wmv2.png"
	case "wmv3", "vc-1":
		return "wmv3.png"
	}
	return ""
}

func audioCodecBadge(codec string) string {
	switch strings.ToLower(codec) {
	case "aac":
		return "aac.png"
	case "ac3":
		return "ac3.png"
	case "eac3":
		return "eac3.png"
	case "dts", "dca":
		return "dca.png"
	case "flac":
		return "flac.png"
	case "mp2":
		return "mp2.png"
	case "mp3":
		return "mp3.png"
	case "opus":
		return "opus.png"
	case "wmav2":
		return "wmav2.png"
	}
	return ""
}

// channelBadge skips stereo (2) as the implicit default — only flagged when
// the layout is non-standard (mono / 5.1 / 7.1).
func channelBadge(channels int) string {
	switch {
	case channels >= 8:
		return "8.png"
	case channels >= 6:
		return "6.png"
	case channels == 1:
		return "1.png"
	}
	return ""
}
