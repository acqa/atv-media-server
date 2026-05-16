package transcoder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// StreamInfo summarises what we need to decide remux vs transcode.
// VideoLevel is encoded as ffprobe does: 41 for Level 4.1, 50 for 5.0, etc.
// AudioOnly is true when the file has no video stream (music/podcasts).
type StreamInfo struct {
	VideoCodec    string
	VideoProfile  string
	VideoLevel    int
	Width         int
	Height        int
	AudioCodec    string
	AudioChannels int
	AudioCount    int // number of audio streams (>=1) — drives the "Audio N" UI
	DurationSec   int
	AudioOnly     bool
}

// Prober is the abstract probe interface; ExecProber runs the real ffprobe.
// Tests inject a fake to avoid depending on the binary.
type Prober interface {
	Probe(ctx context.Context, path string) (StreamInfo, error)
}

// ExecProber shells out to ffprobe. It parses the JSON output of
// `ffprobe -show_streams -show_format -print_format json`.
type ExecProber struct {
	// Output is overridable for tests; when nil, ExecProber actually invokes
	// ffprobe. Tests can swap this to feed canned JSON without an external dep.
	Output func(ctx context.Context, path string) ([]byte, error)
}

// NewExecProber returns a Prober wired to invoke ffprobe.
func NewExecProber() *ExecProber { return &ExecProber{} }

// Probe runs ffprobe on path and decodes the first video/audio stream.
// Returns an error if no video stream is present.
func (p *ExecProber) Probe(ctx context.Context, path string) (StreamInfo, error) {
	if path == "" {
		return StreamInfo{}, errors.New("probe: empty path")
	}
	output := p.Output
	if output == nil {
		output = ffprobeJSON
	}
	data, err := output(ctx, path)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbeJSON(data)
}

func ffprobeJSON(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		path,
	)
	return cmd.Output()
}

// parseProbeJSON converts the raw ffprobe output to StreamInfo. Numeric fields
// in ffprobe JSON are inconsistently typed (sometimes ints, sometimes strings);
// we accept either through json.Number.
func parseProbeJSON(data []byte) (StreamInfo, error) {
	var raw struct {
		Streams []struct {
			CodecType string      `json:"codec_type"`
			CodecName string      `json:"codec_name"`
			Profile   string      `json:"profile"`
			Level     json.Number `json:"level"`
			Width     int         `json:"width"`
			Height    int         `json:"height"`
			Channels  int         `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return StreamInfo{}, fmt.Errorf("ffprobe decode: %w", err)
	}

	var info StreamInfo
	var hasVideo bool
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if hasVideo {
				continue // only first video stream
			}
			hasVideo = true
			info.VideoCodec = s.CodecName
			info.VideoProfile = s.Profile
			info.VideoLevel = parseLevel(s.Level)
			info.Width = s.Width
			info.Height = s.Height
		case "audio":
			info.AudioCount++
			if info.AudioCodec != "" {
				continue
			}
			info.AudioCodec = s.CodecName
			info.AudioChannels = s.Channels
		}
	}
	if !hasVideo {
		if info.AudioCodec == "" {
			return info, errors.New("ffprobe: no audio or video stream")
		}
		info.AudioOnly = true
	}
	if d := raw.Format.Duration; d != "" {
		if f, err := strconv.ParseFloat(d, 64); err == nil {
			info.DurationSec = int(f)
		}
	}
	return info, nil
}

// parseLevel coerces ffprobe's level field (sometimes int 41, sometimes "4.1")
// into the integer form we compare on (Level 4.1 → 41, Level 5.0 → 50).
// Negative or zero levels are returned as 0 (unknown).
func parseLevel(n json.Number) int {
	if n == "" {
		return 0
	}
	s := n.String()
	if v, err := strconv.Atoi(s); err == nil {
		if v < 0 {
			return 0
		}
		return v
	}
	// "4.1" form — strip the dot.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		v := int(f * 10)
		if v < 0 {
			return 0
		}
		return v
	}
	return 0
}
