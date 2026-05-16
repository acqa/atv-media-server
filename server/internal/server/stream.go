package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atv-media-server/server/internal/logging"
	"github.com/atv-media-server/server/internal/storage"
	"github.com/atv-media-server/server/internal/transcoder"
)

// prepareTimeout caps how long ffmpeg gets to finish a single HLS transcode.
// Independent of the client request context so ATV3 dropping the connection
// (its HLS player has its own internal timeout) doesn't SIGKILL ffmpeg in the
// middle of writing the playlist — that left zombie 0-second caches behind.
const prepareTimeout = 10 * time.Minute

// Preparer is the subset of transcoder.Pipeline the stream handler relies on.
type Preparer interface {
	PrepareHLS(ctx context.Context, inputPath, outDir string, audioIndex int) error
}

var (
	idRe   = regexp.MustCompile(`^[a-f0-9]{12}$`)
	fileRe = regexp.MustCompile(`^(playlist\.m3u8|\d{3}\.ts)$`)
)

// streamHandler serves HLS playlists and segments. URLs:
//
//	GET /stream/<id>/playlist.m3u8
//	GET /stream/<id>/<NNN>.ts
//
// The handler runs the preparer for the playlist request; segment requests
// just serve from disk and 404 if the file is absent.
func streamHandler(store *storage.Store, prep Preparer, dataDir string) http.HandlerFunc {
	transcodedDir := filepath.Join(dataDir, "transcoded")
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/stream/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		id, file := parts[0], parts[1]
		if !idRe.MatchString(id) || !fileRe.MatchString(file) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		path, _, err := resolveByID(store, id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			logging.Warn("stream get:", err)
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		// audioIndex (?a=N) selects which audio stream to use. Different audio
		// choices produce different HLS bundles, so each gets its own outDir
		// suffix — the .ts segments are not shareable between dubs.
		audioIndex := 0
		if a := r.URL.Query().Get("a"); a != "" {
			if n, err := strconv.Atoi(a); err == nil && n >= 0 {
				audioIndex = n
			}
		}
		outDir := filepath.Join(transcodedDir, fmt.Sprintf("%s__a%d", id, audioIndex))
		if file == "playlist.m3u8" {
			logging.Info(fmt.Sprintf("stream: playlist requested id=%s audio=%d path=%q ua=%q range=%q",
				id, audioIndex, path, r.Header.Get("User-Agent"), r.Header.Get("Range")))
			// Use a background context (not r.Context()) so ATV3 closing
			// the connection mid-transcode doesn't SIGKILL ffmpeg.
			prepStart := time.Now()
			prepCtx, cancel := context.WithTimeout(context.Background(), prepareTimeout)
			err := prep.PrepareHLS(prepCtx, path, outDir, audioIndex)
			cancel()
			if err != nil {
				logging.Warn(fmt.Sprintf("PrepareHLS failed id=%s audio=%d after=%s err=%v",
					id, audioIndex, time.Since(prepStart).Round(time.Millisecond), err))
				http.Error(w, "transcode failed", http.StatusInternalServerError)
				return
			}
			logging.Info(fmt.Sprintf("stream: playlist ready id=%s audio=%d prep_time=%s",
				id, audioIndex, time.Since(prepStart).Round(time.Millisecond)))
			// Mark recently-used so the GC keeps this entry around.
			if err := transcoder.Touch(outDir); err != nil {
				logging.Warn("touch:", err)
			}
		}
		target := filepath.Join(outDir, file)
		// fileRe + idRe constraints make traversal impossible, but assert anyway.
		if !strings.HasPrefix(target, transcodedDir+string(filepath.Separator)) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, target)
	}
}
