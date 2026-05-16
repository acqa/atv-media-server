package transcoder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordRunner captures the command and writes a fake playlist on success.
type recordRunner struct {
	calls   int
	args    []string
	failNow bool
}

func (r *recordRunner) Run(ctx context.Context, name string, args ...string) error {
	r.calls++
	r.args = append([]string{name}, args...)
	if r.failNow {
		return errors.New("simulated ffmpeg failure")
	}
	playlist := args[len(args)-1]
	dir := filepath.Dir(playlist)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:5.0,\n000.ts\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "000.ts"), []byte("s"), 0o644)
	return nil
}

func TestTranscoder_RunsLibx264Aac(t *testing.T) {
	r := &recordRunner{}
	tr := NewTranscoder(r)
	out := filepath.Join(t.TempDir(), "o")
	if err := tr.PrepareTranscode(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatalf("PrepareTranscode: %v", err)
	}
	if r.calls != 1 {
		t.Errorf("calls: want 1, got %d", r.calls)
	}
	joined := strings.Join(r.args, " ")
	for _, want := range []string{"libx264", "-profile:v high", "-level 4.1", "-c:a aac", "-b:a 192k", "-ac 2", "-f hls"} {
		if !strings.Contains(joined, want) {
			t.Errorf("ffmpeg args missing %q in:\n%s", want, joined)
		}
	}
	for _, name := range []string{"playlist.m3u8", "000.ts", ".done"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestTranscoder_IdempotentWhenDone(t *testing.T) {
	r := &recordRunner{}
	tr := NewTranscoder(r)
	out := filepath.Join(t.TempDir(), "o")
	_ = os.MkdirAll(out, 0o755)
	_ = os.WriteFile(filepath.Join(out, ".done"), nil, 0o644)
	if err := tr.PrepareTranscode(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatal(err)
	}
	if r.calls != 0 {
		t.Errorf("expected 0 calls when .done exists, got %d", r.calls)
	}
}

func TestTranscoder_FailedRunDoesNotMarkDone(t *testing.T) {
	r := &recordRunner{failNow: true}
	tr := NewTranscoder(r)
	out := filepath.Join(t.TempDir(), "o")
	if err := tr.PrepareTranscode(context.Background(), "/in.mkv", out, 0); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(filepath.Join(out, ".done")); err == nil {
		t.Error(".done should not exist after failure")
	}
	// Subsequent attempt with working runner recovers.
	r.failNow = false
	if err := tr.PrepareTranscode(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Errorf("recovery: %v", err)
	}
}

func TestTranscoder_RejectsEmptyArgs(t *testing.T) {
	tr := NewTranscoder(&recordRunner{})
	if err := tr.PrepareTranscode(context.Background(), "", "/o", 0); err == nil {
		t.Error("expected error for empty input")
	}
	if err := tr.PrepareTranscode(context.Background(), "/x.mkv", "", 0); err == nil {
		t.Error("expected error for empty outDir")
	}
}
