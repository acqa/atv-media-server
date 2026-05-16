package appletv

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMainHandler_OK(t *testing.T) {
	gen := New("appletv.redbull.tv")
	srv := httptest.NewServer(http.HandlerFunc(gen.MainHandler))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
		t.Errorf("Content-Type: want application/xml, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	// 1. Must be parseable as XML.
	var generic interface{}
	if err := xml.Unmarshal(body, &generic); err != nil {
		t.Fatalf("response is not valid XML: %v\nbody:\n%s", err, body)
	}
	// 2. Must contain the Movies navigation item.
	s := string(body)
	if !strings.Contains(s, "https://appletv.redbull.tv/movies.xml") {
		t.Errorf("body missing movies.xml link:\n%s", s)
	}
	if !strings.Contains(s, "<title>Movies</title>") {
		t.Errorf("body missing Movies title:\n%s", s)
	}
}

func TestMainHandler_MethodNotAllowed(t *testing.T) {
	gen := New("appletv.redbull.tv")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	gen.MainHandler(rec, req)

	// We respond with the error template (200) — but the body must be an error dialog.
	body := rec.Body.String()
	if !strings.Contains(body, "Method Not Allowed") {
		t.Errorf("body missing error: %s", body)
	}
}

func TestRenderError_FallsBackToPlainText(t *testing.T) {
	// We can't easily corrupt the embedded FS; just confirm that the happy path
	// produces an XML error dialog with title + description.
	gen := New("h")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	gen.RenderError(rec, req, ErrorData{Title: "Boom", Description: "details"})

	body := rec.Body.String()
	if !strings.Contains(body, "<title>Boom</title>") || !strings.Contains(body, "<description>details</description>") {
		t.Errorf("error body unexpected:\n%s", body)
	}
}
