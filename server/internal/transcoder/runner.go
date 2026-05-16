package transcoder

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/atv-media-server/server/internal/logging"
)

// Runner executes external commands. Abstracted so tests can substitute a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands via os/exec. Stdout is discarded; stderr is captured
// so callers see *why* ffmpeg/ffprobe failed (codec not supported, file
// missing, etc.) instead of an opaque "exit status 1".
type ExecRunner struct{}

// stderrTailLimit caps how many bytes of stderr we keep in memory. ffmpeg
// spams progress lines forever; the last few KB is what carries the real
// error message.
const stderrTailLimit = 4096

// Run implements Runner using exec.CommandContext.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &tailWriter{buf: &stderr, limit: stderrTailLimit}
	err := cmd.Run()
	if err != nil {
		tail := strings.TrimSpace(stderr.String())
		logging.Warn(fmt.Sprintf("%s failed: %v\nargs: %v\nstderr tail:\n%s",
			name, err, args, tail))
		if tail != "" {
			return fmt.Errorf("%s: %w: %s", name, err, lastLine(tail))
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// tailWriter keeps only the last `limit` bytes written to it. Cheap way to
// retain the end of ffmpeg's verbose stderr without unbounded memory.
type tailWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if t.buf.Len() > t.limit {
		over := t.buf.Len() - t.limit
		t.buf.Next(over)
	}
	return len(p), nil
}

// lastLine returns the final non-empty line of s — usually the most useful
// error message from ffmpeg/ffprobe.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
