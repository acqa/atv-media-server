package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/atv-media-server/server/internal/logging"
)

// Thresholds for treating an in-progress playlist as playable. ATV3's
// AVPlayer needs a few segments queued before it'll start playback without
// stalling immediately.
const (
	playableMinSegments    = 3
	playableMinDurationSec = 15.0
	// How long PrepareHLS will block waiting for those first segments before
	// giving up. AVPlayer on ATV3 has its own ~30-60s playlist timeout, so
	// blocking longer is pointless — better to fail and let the user retry
	// (the background ffmpeg keeps running and may have produced more by then).
	playableWaitTimeout = 75 * time.Second
	// Background ffmpeg gets up to this much wall time even after the HTTP
	// request has returned. Long enough for any realistic single-file
	// transcode on the kind of hardware ATV3 owners use.
	backgroundTranscodeTimeout = 30 * time.Minute
)

// job tracks a background ffmpeg invocation for a single outDir. done is
// closed when the goroutine exits; err carries the final result. Held in
// Pipeline.jobs so concurrent PrepareHLS calls for the same id share state
// instead of stomping on each other with parallel ffmpegs.
type job struct {
	done chan struct{}
	err  error
}

// Pipeline dispatches between remux (fast, lossless) and full transcode based
// on a Probe of the source. It's the type the HTTP layer holds via the
// server.Preparer interface.
type Pipeline struct {
	Prober     Prober
	Remuxer    *Remuxer
	Transcoder *Transcoder

	mu   sync.Mutex
	jobs map[string]*job
}

// NewPipeline wires the default production stack: ExecProber + Remuxer/Transcoder
// backed by ExecRunner. Tests should construct Pipeline directly.
func NewPipeline() *Pipeline {
	return &Pipeline{
		Prober:     NewExecProber(),
		Remuxer:    NewRemuxer(nil),
		Transcoder: NewTranscoder(nil),
	}
}

// PrepareHLS makes sure the HLS bundle in outDir is ready enough for AVPlayer
// to start streaming. It launches the actual ffmpeg in a background goroutine
// and returns as soon as either:
//   - .done marker is present (full transcode finished previously),
//   - the playlist already contains enough segments to be playable, OR
//   - the background ffmpeg exited (with or without success).
//
// This matters for ATV3: AVPlayer enforces a ~30-60s timeout on playlist.m3u8
// load. A multi-minute full transcode synchronously held inside the HTTP
// handler would always exceed that and the user would see an error even though
// ffmpeg eventually succeeded. With this design ffmpeg keeps writing after
// the HTTP response returns; subsequent /stream/<id>/playlist.m3u8 fetches
// (which AVPlayer issues periodically per HLS spec) see the growing playlist
// and then the final .done state.
// audioIndex selects which audio stream from the source to use (0-based).
// 0 means "first audio stream" — the historical default.
func (p *Pipeline) PrepareHLS(ctx context.Context, inputPath, outDir string, audioIndex int) error {
	if inputPath == "" || outDir == "" {
		return errors.New("inputPath and outDir are required")
	}
	if audioIndex < 0 {
		audioIndex = 0
	}
	if isDone(outDir) {
		return nil
	}

	j := p.ensureJob(inputPath, outDir, audioIndex)
	return p.waitForPlayable(ctx, outDir, j)
}

// ensureJob returns the in-flight job for outDir, starting a new background
// ffmpeg goroutine if none exists. Safe for concurrent calls — at most one
// ffmpeg per outDir.
func (p *Pipeline) ensureJob(inputPath, outDir string, audioIndex int) *job {
	p.mu.Lock()
	if p.jobs == nil {
		p.jobs = map[string]*job{}
	}
	if existing, ok := p.jobs[outDir]; ok {
		p.mu.Unlock()
		return existing
	}
	j := &job{done: make(chan struct{})}
	p.jobs[outDir] = j
	p.mu.Unlock()

	go p.runJob(inputPath, outDir, audioIndex, j)
	return j
}

// runJob executes the probe + chosen transcoder for outDir. Runs on its own
// goroutine and survives HTTP request cancellation by design.
func (p *Pipeline) runJob(inputPath, outDir string, audioIndex int, j *job) {
	defer close(j.done)
	defer func() {
		// On failure, evict from the map so a later retry actually re-runs
		// ffmpeg. On success, keep the entry so isDone()/isPlayable hits the
		// fast path on subsequent fetches.
		if j.err != nil {
			p.mu.Lock()
			delete(p.jobs, outDir)
			p.mu.Unlock()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), backgroundTranscodeTimeout)
	defer cancel()

	info, err := p.Prober.Probe(ctx, inputPath)
	if err != nil {
		j.err = fmt.Errorf("probe: %w", err)
		logging.Warn("PrepareHLS probe failed:", j.err)
		return
	}
	mode := ChooseMode(info)
	switch {
	case info.AudioOnly:
		j.err = p.Transcoder.PrepareAudio(ctx, inputPath, outDir, audioIndex)
	case mode == ModeRemux:
		j.err = p.Remuxer.PrepareRemux(ctx, inputPath, outDir, audioIndex)
	case mode == ModeAudioTranscode:
		j.err = p.Transcoder.PrepareAudioTranscode(ctx, inputPath, outDir, audioIndex)
	default:
		j.err = p.Transcoder.PrepareTranscode(ctx, inputPath, outDir, audioIndex)
	}
	if j.err != nil {
		logging.Warn("PrepareHLS background failed:", j.err)
		return
	}
	logging.Info(fmt.Sprintf("PrepareHLS background ok outDir=%s audio=%d", outDir, audioIndex))
}

// waitForPlayable polls outDir for a playable playlist or job completion.
// Returns nil as soon as either condition is met; otherwise propagates the
// transcoder error or hits the wait timeout.
func (p *Pipeline) waitForPlayable(ctx context.Context, outDir string, j *job) error {
	deadline := time.Now().Add(playableWaitTimeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if isPlayable(outDir, playableMinSegments, playableMinDurationSec) {
			return nil
		}
		select {
		case <-j.done:
			// Final disposition: error wins, then full-completion marker,
			// then a last-chance playability check (covers fast fake-runner
			// tests where only a single segment is written before close).
			if j.err != nil {
				return j.err
			}
			if isDone(outDir) {
				return nil
			}
			if isPlayable(outDir, playableMinSegments, playableMinDurationSec) {
				return nil
			}
			if isPlayable(outDir, 1, 1.0) {
				return nil
			}
			return errors.New("transcode finished but playlist not playable")
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			return errors.New("playlist not playable within " + playableWaitTimeout.String())
		}
	}
}

// extinfRe extracts segment durations from a playlist.m3u8 body.
var extinfRe = regexp.MustCompile(`(?m)^#EXTINF:([0-9.]+)`)

// isPlayable inspects outDir/playlist.m3u8 and returns true once it carries
// at least minSegs segments totalling minDur seconds. Cheap (single file
// read) so OK to call on every poll tick.
func isPlayable(outDir string, minSegs int, minDur float64) bool {
	data, err := os.ReadFile(filepath.Join(outDir, "playlist.m3u8"))
	if err != nil {
		return false
	}
	matches := extinfRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) < minSegs {
		return false
	}
	var total float64
	for _, m := range matches {
		d, _ := strconv.ParseFloat(m[1], 64)
		total += d
	}
	return total >= minDur
}
