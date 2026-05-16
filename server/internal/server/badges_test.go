package server

import (
	"reflect"
	"testing"
)

func TestBadgeFilenames(t *testing.T) {
	cases := []struct {
		name     string
		height   int
		vCodec   string
		aCodec   string
		channels int
		want     []string
	}{
		{"empty_all", 0, "", "", 0, nil},
		{"only_height_1080", 1080, "", "", 0, []string{"1080.png"}},
		{"sd_for_low_res", 480, "", "", 0, []string{"sd.png"}},
		{"4k_threshold", 2160, "", "", 0, []string{"4k.png"}},
		{"2k_threshold", 1440, "", "", 0, []string{"2k.png"}},
		{"h264_alone", 0, "h264", "", 0, []string{"h264.png"}},
		{"hevc_alias", 0, "h265", "", 0, []string{"hevc.png"}},
		{"unknown_codec_skipped", 0, "av1", "", 0, nil},
		{"dts_via_dca", 0, "", "dts", 0, []string{"dca.png"}},
		{"5_1_channels", 0, "", "", 6, []string{"6.png"}},
		{"7_1_channels", 0, "", "", 8, []string{"8.png"}},
		{"stereo_skipped", 0, "", "", 2, nil},
		{"mono_kept", 0, "", "", 1, []string{"1.png"}},
		{
			"full_combo",
			1080, "h264", "ac3", 6,
			[]string{"1080.png", "h264.png", "ac3.png", "6.png"},
		},
		{
			"ordering_preserved",
			720, "hevc", "aac", 8,
			[]string{"720.png", "hevc.png", "aac.png", "8.png"},
		},
		{
			"partial_unknown_codec",
			1080, "av1", "aac", 6,
			[]string{"1080.png", "aac.png", "6.png"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BadgeFilenames(c.height, c.vCodec, c.aCodec, c.channels)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("BadgeFilenames(%d, %q, %q, %d) = %v, want %v",
					c.height, c.vCodec, c.aCodec, c.channels, got, c.want)
			}
		})
	}
}
