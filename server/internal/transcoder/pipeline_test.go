package transcoder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modeRunner remembers whether ffmpeg was invoked with -c:v copy (remux)
// or -c:v libx264 (transcode), and creates the expected output on success.
type modeRunner struct {
	lastMode Mode
	calls    int
	failNow  bool
}

func (r *modeRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls++
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "libx264") {
		r.lastMode = ModeTranscode
	} else if strings.Contains(joined, "-c:v copy") {
		r.lastMode = ModeRemux
	}
	if r.failNow {
		return errors.New("simulated ffmpeg failure")
	}
	playlist := args[len(args)-1]
	dir := filepath.Dir(playlist)
	_ = os.MkdirAll(dir, 0o755)
	return os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:5.0,\n000.ts\n"), 0o644)
}

// fakeProber returns a canned StreamInfo.
type fakeProber struct {
	info StreamInfo
	err  error
}

func (f *fakeProber) Probe(ctx context.Context, path string) (StreamInfo, error) {
	return f.info, f.err
}

func newPipeline(prober Prober, runner Runner) *Pipeline {
	return &Pipeline{
		Prober:     prober,
		Remuxer:    NewRemuxer(runner),
		Transcoder: NewTranscoder(runner),
	}
}

func TestPipeline_DispatchesRemuxForCompatibleSource(t *testing.T) {
	r := &modeRunner{}
	p := newPipeline(
		&fakeProber{info: StreamInfo{VideoCodec: "h264", VideoProfile: "High", VideoLevel: 41, AudioCodec: "aac"}},
		r,
	)
	out := filepath.Join(t.TempDir(), "o")
	if err := p.PrepareHLS(context.Background(), "/in.mp4", out, 0); err != nil {
		t.Fatal(err)
	}
	if r.lastMode != ModeRemux {
		t.Errorf("want remux, got %s", r.lastMode)
	}
}

func TestPipeline_DispatchesTranscodeForIncompatible(t *testing.T) {
	r := &modeRunner{}
	p := newPipeline(
		&fakeProber{info: StreamInfo{VideoCodec: "hevc", VideoProfile: "Main 10", VideoLevel: 51, AudioCodec: "dts"}},
		r,
	)
	out := filepath.Join(t.TempDir(), "o")
	if err := p.PrepareHLS(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatal(err)
	}
	if r.lastMode != ModeTranscode {
		t.Errorf("want transcode, got %s", r.lastMode)
	}
}

func TestPipeline_ShortCircuitsOnDoneMarker(t *testing.T) {
	r := &modeRunner{}
	probedCalls := 0
	p := newPipeline(
		proberFunc(func(ctx context.Context, path string) (StreamInfo, error) {
			probedCalls++
			return StreamInfo{}, nil
		}),
		r,
	)
	out := filepath.Join(t.TempDir(), "o")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(out, ".done"), nil, 0o644)

	if err := p.PrepareHLS(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatal(err)
	}
	if probedCalls != 0 {
		t.Errorf("probe should be skipped when .done exists, got %d calls", probedCalls)
	}
	if r.calls != 0 {
		t.Errorf("runner should be skipped, got %d calls", r.calls)
	}
}

func TestPipeline_ProbeErrorPropagates(t *testing.T) {
	want := errors.New("ffprobe blew up")
	p := newPipeline(&fakeProber{err: want}, &modeRunner{})
	out := filepath.Join(t.TempDir(), "o")
	if err := p.PrepareHLS(context.Background(), "/in.mkv", out, 0); !errors.Is(err, want) {
		t.Errorf("want %v, got %v", want, err)
	}
}

func TestPipeline_RejectsEmptyArgs(t *testing.T) {
	p := newPipeline(&fakeProber{}, &modeRunner{})
	if err := p.PrepareHLS(context.Background(), "", "/o", 0); err == nil {
		t.Error("expected error for empty input")
	}
	if err := p.PrepareHLS(context.Background(), "/x.mkv", "", 0); err == nil {
		t.Error("expected error for empty outDir")
	}
}

// proberFunc adapts a function to the Prober interface for inline tests.
type proberFunc func(ctx context.Context, path string) (StreamInfo, error)

func (f proberFunc) Probe(ctx context.Context, path string) (StreamInfo, error) {
	return f(ctx, path)
}
