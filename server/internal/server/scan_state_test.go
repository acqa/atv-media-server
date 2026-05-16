package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/atv-media-server/server/internal/library"
)

func TestScanState_ScanReturns202(t *testing.T) {
	state := NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		return library.ScanResult{Total: 3, Matched: 2, Skipped: 0}, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/library/scan", state.ScanHandler())
	mux.HandleFunc("/api/library/status", state.StatusHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/library/scan", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202, got %d", resp.StatusCode)
	}

	// Wait briefly for the goroutine to finish.
	deadline := time.Now().Add(time.Second)
	var snap StatusSnapshot
	for time.Now().Before(deadline) {
		snap = state.Snapshot()
		if !snap.Running {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if snap.Running {
		t.Fatal("scan never finished")
	}
	if snap.Result.Total != 3 || snap.Result.Matched != 2 {
		t.Errorf("result: %+v", snap.Result)
	}
}

func TestScanState_ConcurrentTriggerReturns409(t *testing.T) {
	gate := make(chan struct{})
	state := NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		<-gate
		return library.ScanResult{}, nil
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/library/scan", state.ScanHandler())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// First request kicks off the (blocked) scan.
	resp1, err := http.Post(srv.URL+"/api/library/scan", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first: want 202, got %d", resp1.StatusCode)
	}

	// Second request should collide.
	resp2, _ := http.Post(srv.URL+"/api/library/scan", "", nil)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("second: want 409, got %d", resp2.StatusCode)
	}

	close(gate)
	// Drain to keep test fast.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !state.Snapshot().Running {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("scan never finished")
}

func TestScanState_StatusJSONShape(t *testing.T) {
	state := NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		return library.ScanResult{Total: 1}, nil
	})
	state.TriggerAndWait()

	srv := httptest.NewServer(state.StatusHandler())
	t.Cleanup(srv.Close)

	resp, _ := http.Get(srv.URL)
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: %q", ct)
	}
	var got map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, want := range []string{"running", "started", "finished", "result"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing key %q in %v", want, got)
		}
	}
	if got["running"].(bool) {
		t.Error("running should be false after wait")
	}
}

func TestScanState_RecordsError(t *testing.T) {
	want := errors.New("boom")
	state := NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		return library.ScanResult{}, want
	})
	snap := state.TriggerAndWait()
	if snap.Error != "boom" {
		t.Errorf("Error: want boom, got %q", snap.Error)
	}
}

func TestScanState_StatusHandlerMethodCheck(t *testing.T) {
	state := NewScanState(func(context.Context, library.Logger) (library.ScanResult, error) {
		return library.ScanResult{}, nil
	})
	rec := httptest.NewRecorder()
	state.StatusHandler()(rec, httptest.NewRequest(http.MethodPost, "/api/library/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

func TestScanState_ScanHandlerMethodCheck(t *testing.T) {
	state := NewScanState(func(context.Context, library.Logger) (library.ScanResult, error) {
		return library.ScanResult{}, nil
	})
	rec := httptest.NewRecorder()
	state.ScanHandler()(rec, httptest.NewRequest(http.MethodGet, "/api/library/scan", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

// Bonus: race coverage on Snapshot under concurrent readers/writer.
func TestScanState_SnapshotConcurrentSafe(t *testing.T) {
	state := NewScanState(func(ctx context.Context, log library.Logger) (library.ScanResult, error) {
		return library.ScanResult{}, nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = state.Snapshot()
				_ = state.Trigger()
			}
		}()
	}
	wg.Wait()
}
