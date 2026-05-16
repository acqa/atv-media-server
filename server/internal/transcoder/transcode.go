package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Transcoder re-encodes a source file to ATV3-compatible H.264 High@4.1
// + AAC stereo HLS. Slow, CPU-bound. Idempotent via .done marker like Remuxer.
type Transcoder struct {
	runner Runner
}

// NewTranscoder returns a Transcoder using the supplied runner; nil = real ffmpeg.
func NewTranscoder(runner Runner) *Transcoder {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Transcoder{runner: runner}
}

// PrepareAudio produces an audio-only HLS bundle (AAC stereo, 192 kbps) for
// music/podcast files. Idempotent via .done marker like the other paths.
// audioIndex selects which audio stream from the source to use (0-based).
func (t *Transcoder) PrepareAudio(ctx context.Context, inputPath, outDir string, audioIndex int) error {
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
	// -map 0:a:<N> picks a specific audio stream explicitly. Required because
	// some MP3s carry an attached_pic (embedded album art as an mjpeg stream)
	// that survives -vn — ffmpeg ends up encoding that single still as the
	// "video" of the HLS output, yielding a 0.000011s playlist.
	args := []string{
		"-y",
		"-i", inputPath,
		"-map", fmt.Sprintf("0:a:%d", audioIndex),
		"-vn",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", "10",
		"-hls_list_size", "0",
		"-hls_segment_filename", segments,
		playlist,
	}
	if err := t.runner.Run(ctx, "ffmpeg", args...); err != nil {
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

// PrepareAudioTranscode produces HLS with video copied unchanged and audio
// re-encoded to AAC stereo. Used when the source video is already ATV3-friendly
// but the audio codec (eac3, DTS, FLAC, etc.) isn't — saves the bulk of the
// CPU cost of a full transcode by skipping libx264.
// audioIndex selects which audio stream from the source to use (0-based).
func (t *Transcoder) PrepareAudioTranscode(ctx context.Context, inputPath, outDir string, audioIndex int) error {
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
	// Video stays as the source's h264 stream (zero CPU cost in the loop).
	// Audio gets normalised to AAC stereo so AVPlayer on ATV3 can decode it.
	// Timestamp flags match the full-transcode path so playback always starts
	// at frame 0.
	args := []string{
		"-y",
		"-fflags", "+genpts",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", fmt.Sprintf("0:a:%d", audioIndex),
		"-avoid_negative_ts", "make_zero",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segments,
		playlist,
	}
	if err := t.runner.Run(ctx, "ffmpeg", args...); err != nil {
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

// PrepareTranscode produces playlist.m3u8 + segments in outDir by re-encoding
// inputPath. The bitrate caps are conservative for ATV3 hardware: 5 Mbps video,
// 192 kbps audio downmixed to stereo.
// audioIndex selects which audio stream from the source to use (0-based).
func (t *Transcoder) PrepareTranscode(ctx context.Context, inputPath, outDir string, audioIndex int) error {
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
	// -fflags +genpts + -avoid_negative_ts make_zero normalises PTS/DTS so
	// HLS doesn't start mid-file. Some BDRip / MKV sources carry edit lists
	// or non-zero start timestamps that confuse the player: it would either
	// skip the first ~minute or seek to the wrong place. These flags
	// regenerate timestamps from zero on the way in.
	//
	// Explicit -map 0:v:0 -map 0:a:<N> picks the first video and a chosen
	// audio stream. Defends against files with extra streams (chapter
	// thumbnails, alternate language tracks, subtitles) that ffmpeg might
	// otherwise pick up and break HLS output.
	args := []string{
		"-y",
		"-fflags", "+genpts",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", fmt.Sprintf("0:a:%d", audioIndex),
		"-avoid_negative_ts", "make_zero",
		"-c:v", "libx264",
		"-profile:v", "high",
		"-level", "4.1",
		"-pix_fmt", "yuv420p",
		"-preset", "veryfast",
		"-b:v", "5000k",
		"-maxrate", "5000k",
		"-bufsize", "10000k",
		"-c:a", "aac",
		"-b:a", "192k",
		"-ac", "2",
		"-f", "hls",
		"-hls_time", "6",
		"-hls_list_size", "0",
		"-hls_segment_filename", segments,
		playlist,
	}
	if err := t.runner.Run(ctx, "ffmpeg", args...); err != nil {
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
