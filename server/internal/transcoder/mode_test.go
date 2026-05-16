package transcoder

import "testing"

func TestChooseMode(t *testing.T) {
	cases := []struct {
		name string
		info StreamInfo
		want Mode
	}{
		{
			name: "h264 high@4.1 + aac stereo → remux",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: "aac"},
			want: ModeRemux,
		},
		{
			name: "h264 main@4.0 + ac3 → remux",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "Main", VideoLevel: 40, AudioCodec: "ac3"},
			want: ModeRemux,
		},
		{
			// ATV3's AVPlayer can't decode eac3 — but the video here is fine,
			// so we only re-encode audio (much faster than full transcode).
			name: "h264 baseline@3.1 + eac3 → audio-transcode",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "Baseline", VideoLevel: 31, AudioCodec: "eac3"},
			want: ModeAudioTranscode,
		},
		{
			name: "h264 constrained baseline → remux",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "Constrained Baseline", VideoLevel: 31, AudioCodec: "aac"},
			want: ModeRemux,
		},
		{
			name: "h264 high@4.1 + dts → audio-transcode",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: "dts"},
			want: ModeAudioTranscode,
		},
		{
			name: "h264 high@5.0 → transcode (level too high)",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 50, AudioCodec: "aac"},
			want: ModeTranscode,
		},
		{
			name: "h264 high@unknown level → transcode (level=0)",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 0, AudioCodec: "aac"},
			want: ModeTranscode,
		},
		{
			name: "h264 high 10 (10-bit) → transcode (profile not in allow-list)",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High 10", VideoLevel: 41, AudioCodec: "aac"},
			want: ModeTranscode,
		},
		{
			name: "hevc main10 + aac → transcode (video codec)",
			info: StreamInfo{VideoCodec: "hevc", VideoProfile: "Main 10", VideoLevel: 51, AudioCodec: "aac"},
			want: ModeTranscode,
		},
		{
			name: "av1 main → transcode",
			info: StreamInfo{VideoCodec: "av1", VideoProfile: "Main", VideoLevel: 40, AudioCodec: "aac"},
			want: ModeTranscode,
		},
		{
			name: "h264 high@4.1 + flac → audio-transcode",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: "flac"},
			want: ModeAudioTranscode,
		},
		{
			name: "h264 high@4.1 + empty audio → audio-transcode (audio missing)",
			info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: ""},
			want: ModeAudioTranscode,
		},
		{
			name: "empty info → transcode (degraded fallback)",
			info: StreamInfo{},
			want: ModeTranscode,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ChooseMode(c.info)
			if got != c.want {
				t.Errorf("ChooseMode(%+v) = %s, want %s", c.info, got, c.want)
			}
		})
	}
}

func TestModeString(t *testing.T) {
	if ModeRemux.String() != "remux" || ModeTranscode.String() != "transcode" {
		t.Errorf("unexpected Mode strings: %s / %s", ModeRemux, ModeTranscode)
	}
}
