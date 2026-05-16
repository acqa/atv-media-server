package server

import "testing"

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, ""},
		{-5, ""},
		{30, "0m"},
		{59, "0m"},
		{60, "1m"},
		{600, "10m"},
		{3599, "59m"},
		{3600, "1h 0m"},
		{6420, "1h 47m"},
		{7200, "2h 0m"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatQuality(t *testing.T) {
	cases := []struct {
		name   string
		height int
		vCodec string
		aCodec string
		want   string
	}{
		{"empty", 0, "", "", ""},
		{"only_codec", 0, "h264", "", "H.264"},
		{"only_audio", 0, "", "ac3", "AC3"},
		{"full_1080", 1080, "h264", "ac3", "1080p · H.264 · AC3"},
		{"720_hevc_aac", 720, "hevc", "aac", "720p · HEVC · AAC"},
		{"sd", 480, "h264", "aac", "SD · H.264 · AAC"},
		{"eac3", 1080, "h264", "eac3", "1080p · H.264 · E-AC3"},
		{"unknown_codec_uppercased", 720, "av1", "opus", "720p · AV1 · OPUS"},
		{"h265_alias", 1080, "h265", "dts", "1080p · HEVC · DTS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatQuality(c.height, c.vCodec, c.aCodec); got != c.want {
				t.Errorf("FormatQuality(%d, %q, %q) = %q, want %q",
					c.height, c.vCodec, c.aCodec, got, c.want)
			}
		})
	}
}

func TestRatingPercent(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 0},
		{-1, 0},
		{0.1, 1},
		{5, 50},
		{7.5, 75},
		{8.14, 81},
		{8.15, 82},
		{10, 100},
		{12, 100}, // clamp
	}
	for _, c := range cases {
		if got := RatingPercent(c.in); got != c.want {
			t.Errorf("RatingPercent(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
