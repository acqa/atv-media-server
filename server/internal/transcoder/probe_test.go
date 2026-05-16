package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func jsonNumber(s string) json.Number { return json.Number(s) }

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseProbeJSON_H264HighAAC(t *testing.T) {
	info, err := parseProbeJSON(loadFixture(t, "h264_high_41_aac.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.VideoCodec != "h264" || info.VideoProfile != "High" || info.VideoLevel != 41 {
		t.Errorf("video fields: %+v", info)
	}
	if info.Width != 1920 || info.Height != 1080 {
		t.Errorf("dimensions: %+v", info)
	}
	if info.AudioCodec != "aac" || info.AudioChannels != 2 {
		t.Errorf("audio: %+v", info)
	}
	if info.DurationSec != 5432 {
		t.Errorf("duration: want 5432, got %d", info.DurationSec)
	}
}

func TestParseProbeJSON_HEVCMain10DTS_StringLevel(t *testing.T) {
	info, err := parseProbeJSON(loadFixture(t, "hevc_main10_dts.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.VideoCodec != "hevc" || info.VideoProfile != "Main 10" {
		t.Errorf("video codec/profile: %+v", info)
	}
	// "5.1" → 51
	if info.VideoLevel != 51 {
		t.Errorf("level: want 51, got %d", info.VideoLevel)
	}
	if info.AudioCodec != "dts" || info.AudioChannels != 6 {
		t.Errorf("audio: %+v", info)
	}
}

func TestParseProbeJSON_NegativeLevelIsZero(t *testing.T) {
	info, err := parseProbeJSON(loadFixture(t, "av1_no_audio.json"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.VideoLevel != 0 {
		t.Errorf("negative level should become 0, got %d", info.VideoLevel)
	}
	if info.AudioCodec != "" {
		t.Errorf("audio: want empty, got %q", info.AudioCodec)
	}
}

func TestParseProbeJSON_AudioOnlyMarksFlag(t *testing.T) {
	info, err := parseProbeJSON(loadFixture(t, "audio_only.json"))
	if err != nil {
		t.Fatalf("audio-only must not error: %v", err)
	}
	if !info.AudioOnly {
		t.Errorf("AudioOnly: want true, got false")
	}
	if info.AudioCodec != "flac" {
		t.Errorf("AudioCodec: want flac, got %q", info.AudioCodec)
	}
	if info.VideoCodec != "" {
		t.Errorf("VideoCodec: want empty, got %q", info.VideoCodec)
	}
}

func TestParseProbeJSON_InvalidJSON(t *testing.T) {
	_, err := parseProbeJSON([]byte(`{not json`))
	if err == nil {
		t.Error("expected decode error")
	}
}

func TestExecProber_UsesInjectedOutput(t *testing.T) {
	p := &ExecProber{
		Output: func(ctx context.Context, path string) ([]byte, error) {
			if path != "/in.mkv" {
				t.Errorf("path: %q", path)
			}
			return loadFixture(t, "h264_high_41_aac.json"), nil
		},
	}
	info, err := p.Probe(context.Background(), "/in.mkv")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.VideoCodec != "h264" {
		t.Errorf("got %+v", info)
	}
}

func TestExecProber_PropagatesOutputError(t *testing.T) {
	p := &ExecProber{
		Output: func(ctx context.Context, path string) ([]byte, error) {
			return nil, errors.New("ffprobe not installed")
		},
	}
	_, err := p.Probe(context.Background(), "/x")
	if err == nil {
		t.Error("expected error")
	}
}

func TestExecProber_EmptyPath(t *testing.T) {
	p := NewExecProber()
	_, err := p.Probe(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"41", 41},
		{"5.1", 51},
		{"5.0", 50},
		{"4.0", 40},
		{"-99", 0},
		{"", 0},
		{"not-a-number", 0},
	}
	for _, c := range cases {
		// We pass a json.Number; lift via construction.
		got := parseLevel(jsonNumber(c.in))
		if got != c.want {
			t.Errorf("parseLevel(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
