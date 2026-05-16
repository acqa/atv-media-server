package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestSearchTV_ParsesFirstHit(t *testing.T) {
	var got *url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL
		_, _ = w.Write(loadFixture(t, "search_tv_breaking_bad.json"))
	})
	res, ok, err := c.SearchTV(context.Background(), "Breaking Bad", 2008)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if res.TMDbID != 1396 || res.Title != "Breaking Bad" || res.OriginalName != "Breaking Bad" {
		t.Errorf("hit: %+v", res)
	}
	if res.Year != 2008 {
		t.Errorf("Year: want 2008, got %d", res.Year)
	}
	if got.Path != "/search/tv" {
		t.Errorf("path: %q", got.Path)
	}
	if got.Query().Get("query") != "Breaking Bad" || got.Query().Get("first_air_date_year") != "2008" {
		t.Errorf("query: %v", got.Query())
	}
}

func TestSearchTV_NoAPIKey(t *testing.T) {
	c := New("")
	_, _, err := c.SearchTV(context.Background(), "x", 0)
	if !errors.Is(err, ErrNoAPIKey) {
		t.Errorf("want ErrNoAPIKey, got %v", err)
	}
}

func TestSearchTV_EmptyResults(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(loadFixture(t, "search_empty.json"))
	})
	_, ok, err := c.SearchTV(context.Background(), "x", 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false for empty results")
	}
}

func TestGetEpisode_BuildsCorrectURL(t *testing.T) {
	var got *url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL
		_, _ = w.Write(loadFixture(t, "episode_s01e01.json"))
	})
	ep, ok, err := c.GetEpisode(context.Background(), 1396, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || ep.Name != "Pilot" || ep.StillPath != "/abc.jpg" {
		t.Errorf("episode: %+v", ep)
	}
	if got.Path != "/tv/1396/season/1/episode/1" {
		t.Errorf("path: %q", got.Path)
	}
}

func TestGetEpisode_404IsSoftMiss(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ep, ok, err := c.GetEpisode(context.Background(), 1, 1, 1)
	if err != nil {
		t.Errorf("404 should not error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for 404")
	}
	if ep != (EpisodeResult{}) {
		t.Errorf("expected zero episode, got %+v", ep)
	}
}

func TestGetEpisode_RejectsBadInputs(t *testing.T) {
	c := New("k")
	cases := []struct{ tv, s, e int }{
		{0, 1, 1}, {1, 0, 1}, {1, 1, 0}, {-1, 1, 1},
	}
	for _, c2 := range cases {
		_, _, err := c.GetEpisode(context.Background(), c2.tv, c2.s, c2.e)
		if err == nil {
			t.Errorf("expected error for %+v", c2)
		}
	}
}
