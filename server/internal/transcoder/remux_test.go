package transcoder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeRunner records invocations and simulates ffmpeg writing a playlist.
type fakeRunner struct {
	calls            int
	failNext         bool
	playlistContents string
	segmentNames     []string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls++
	if f.failNext {
		return errors.New("simulated ffmpeg failure")
	}
	// Locate the playlist path (last positional arg) and segment template.
	playlist := args[len(args)-1]
	if err := os.WriteFile(playlist, []byte(f.playlistContents), 0o644); err != nil {
		return err
	}
	dir := filepath.Dir(playlist)
	for _, seg := range f.segmentNames {
		if err := os.WriteFile(filepath.Join(dir, seg), []byte("seg"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestPrepareRemux_CreatesPlaylistAndDone(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	fr := &fakeRunner{
		playlistContents: "#EXTM3U\n#EXTINF:5.0,\n000.ts\n",
		segmentNames:     []string{"000.ts", "001.ts"},
	}
	rx := NewRemuxer(fr)

	if err := rx.PrepareRemux(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatalf("PrepareRemux: %v", err)
	}
	if fr.calls != 1 {
		t.Errorf("expected 1 ffmpeg call, got %d", fr.calls)
	}
	for _, name := range []string{"playlist.m3u8", "000.ts", "001.ts", ".done"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestPrepareRemux_IdempotentWhenDoneMarkerExists(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, ".done"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{}
	rx := NewRemuxer(fr)

	if err := rx.PrepareRemux(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatalf("PrepareRemux: %v", err)
	}
	if fr.calls != 0 {
		t.Errorf("expected 0 ffmpeg calls when .done present, got %d", fr.calls)
	}
}

func TestPrepareRemux_FailedRunDoesNotMarkDone(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	fr := &fakeRunner{failNext: true}
	rx := NewRemuxer(fr)

	err := rx.PrepareRemux(context.Background(), "/in.mkv", out, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, err := os.Stat(filepath.Join(out, ".done")); err == nil {
		t.Errorf(".done marker should not exist after a failed run")
	}

	// Second attempt with a working runner should succeed.
	fr.failNext = false
	fr.playlistContents = "#EXTM3U\n#EXTINF:5.0,\n000.ts\n"
	fr.segmentNames = []string{"000.ts"}
	if err := rx.PrepareRemux(context.Background(), "/in.mkv", out, 0); err != nil {
		t.Fatalf("recovery attempt failed: %v", err)
	}
}

func TestPrepareRemux_RejectsEmptyArgs(t *testing.T) {
	rx := NewRemuxer(&fakeRunner{})
	if err := rx.PrepareRemux(context.Background(), "", "/tmp/x", 0); err == nil {
		t.Error("expected error for empty inputPath")
	}
	if err := rx.PrepareRemux(context.Background(), "/x.mkv", "", 0); err == nil {
		t.Error("expected error for empty outDir")
	}
}
