package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const doneMarker = ".done"

// validateHLS checks that a freshly-written HLS playlist actually has playable
// content. Catches the case where ffmpeg was interrupted (context cancelled,
// SIGKILL, etc.) and left behind a stub like:
//
//	#EXT-X-TARGETDURATION:0
//	#EXTINF:0.000011,
//	000.ts
//	#EXT-X-ENDLIST
//
// Without this guard, markDone would happily lock in the broken cache and
// playback would silently fail on every retry.
func validateHLS(outDir string) error {
	data, err := os.ReadFile(filepath.Join(outDir, "playlist.m3u8"))
	if err != nil {
		return errors.New("read playlist: " + err.Error())
	}
	s := string(data)
	if !strings.Contains(s, "#EXTM3U") {
		return errors.New("playlist missing #EXTM3U")
	}
	extinfRe := regexp.MustCompile(`(?m)^#EXTINF:([0-9.]+)`)
	matches := extinfRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return errors.New("playlist has no segments")
	}
	var total float64
	for _, m := range matches {
		d, _ := strconv.ParseFloat(m[1], 64)
		total += d
	}
	// 1 second is a generous floor — even a short audio clip should exceed
	// this. Real media is always many seconds long.
	if total < 1.0 {
		return errors.New("playlist total duration too short: " + strconv.FormatFloat(total, 'f', 6, 64) + "s")
	}
	return nil
}

// Remuxer prepares HLS output by repackaging a compatible source (H.264 + AAC/AC3)
// without re-encoding. Compatibility detection happens in Phase 3; for now we
// assume the caller knows the source is fine.
type Remuxer struct {
	runner Runner
}

// NewRemuxer returns a Remuxer using the supplied command runner.
// Pass ExecRunner{} in production; tests can substitute a fake.
func NewRemuxer(runner Runner) *Remuxer {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Remuxer{runner: runner}
}

// PrepareRemux ensures outDir contains a valid HLS playlist for inputPath.
// Idempotent: if outDir/.done is present, returns nil without running ffmpeg.
// On success creates outDir/.done.
// audioIndex selects which audio stream from the source to use (0-based).
func (r *Remuxer) PrepareRemux(ctx context.Context, inputPath, outDir string, audioIndex int) error {
	if inputPath == "" || outDir == "" {
		return errors.New("inputPath and outDir are required")
	}
	if audioIndex < 0 {
		audioIndex = 0
	}
	if isDone(outDir) {
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	playlist := filepath.Join(outDir, "playlist.m3u8")
	segments := filepath.Join(outDir, "%03d.ts")
	// -fflags +genpts + -avoid_negative_ts make_zero normalises stream
	// timestamps so AVPlayer always starts at the first frame. Without this,
	// MKV sources with edit lists or non-zero start_time would cause ATV3
	// to seek into the middle of the file on Play. Remux preserves codec
	// data; only container metadata is rewritten.
	args := []string{
		"-y",
		"-fflags", "+genpts",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", fmt.Sprintf("0:a:%d", audioIndex),
		"-avoid_negative_ts", "make_zero",
		"-c:v", "copy",
		"-c:a", "copy",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segments,
		playlist,
	}
	if err := r.runner.Run(ctx, "ffmpeg", args...); err != nil {
		return err
	}
	if _, err := os.Stat(playlist); err != nil {
		return errors.New("ffmpeg produced no playlist: " + err.Error())
	}
	if err := validateHLS(outDir); err != nil {
		return err
	}
	return markDone(outDir)
}

func isDone(outDir string) bool {
	_, err := os.Stat(filepath.Join(outDir, doneMarker))
	return err == nil
}

func markDone(outDir string) error {
	f, err := os.Create(filepath.Join(outDir, doneMarker))
	if err != nil {
		return err
	}
	return f.Close()
}
