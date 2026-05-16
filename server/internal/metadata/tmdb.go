// Package metadata talks to TMDb. Only the small subset of the v3 API the
// scanner needs is wrapped — search/movie, movie/{id}. TV endpoints land in
// Phase 4.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://api.themoviedb.org/3"
	defaultLanguage = "ru-RU"
	imageBaseURL    = "https://image.tmdb.org/t/p"
)

// Client is a thin TMDb v3 client. Zero value is unusable — construct with New.
type Client struct {
	APIKey   string
	Language string
	BaseURL  string
	HTTP     *http.Client
	// MaxRetries is the number of 429-retry attempts after the initial request.
	// Zero (default) means one attempt with no retry; tests can override.
	MaxRetries int
	// BackoffMin is the minimum delay before a retry; doubled each retry.
	BackoffMin time.Duration
}

// MovieResult is the projection of TMDb's search/movie hit relevant to us.
type MovieResult struct {
	TMDbID        int
	Title         string
	OriginalTitle string
	Year          int
	Description   string
	PosterPath    string // relative; combine with PosterURL to build absolute URLs
	BackdropPath  string
	Rating        float64
}

// New returns a Client with sensible defaults. apiKey may be empty — in which
// case all calls return ErrNoAPIKey, useful for letting the scanner degrade
// gracefully when TMDB_API_KEY is unset.
func New(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		Language:   defaultLanguage,
		BaseURL:    defaultBaseURL,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		MaxRetries: 2,
		BackoffMin: 500 * time.Millisecond,
	}
}

// ErrNoAPIKey is returned when SearchMovie is called on a Client without an API key.
var ErrNoAPIKey = errors.New("TMDb: API key not configured")

// SearchMovie queries /search/movie?query=...&year=... and returns the first hit.
// The found bool is false when the API returns zero results.
func (c *Client) SearchMovie(ctx context.Context, query string, year int) (MovieResult, bool, error) {
	if c.APIKey == "" {
		return MovieResult{}, false, ErrNoAPIKey
	}
	if strings.TrimSpace(query) == "" {
		return MovieResult{}, false, errors.New("TMDb: empty query")
	}
	params := url.Values{}
	params.Set("api_key", c.APIKey)
	params.Set("language", c.Language)
	params.Set("query", query)
	params.Set("include_adult", "false")
	if year > 0 {
		params.Set("year", strconv.Itoa(year))
	}
	u := c.BaseURL + "/search/movie?" + params.Encode()

	body, err := c.getWithRetry(ctx, u)
	if err != nil {
		return MovieResult{}, false, err
	}
	var resp struct {
		Results []struct {
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			OriginalTitle string  `json:"original_title"`
			Overview      string  `json:"overview"`
			ReleaseDate   string  `json:"release_date"`
			PosterPath    string  `json:"poster_path"`
			BackdropPath  string  `json:"backdrop_path"`
			VoteAverage   float64 `json:"vote_average"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return MovieResult{}, false, fmt.Errorf("TMDb decode: %w", err)
	}
	if len(resp.Results) == 0 {
		return MovieResult{}, false, nil
	}
	hit := resp.Results[0]
	r := MovieResult{
		TMDbID:        hit.ID,
		Title:         hit.Title,
		OriginalTitle: hit.OriginalTitle,
		Description:   hit.Overview,
		PosterPath:    hit.PosterPath,
		BackdropPath:  hit.BackdropPath,
		Rating:        hit.VoteAverage,
	}
	if len(hit.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(hit.ReleaseDate[:4]); err == nil {
			r.Year = y
		}
	}
	return r, true, nil
}

// getWithRetry performs GET with exponential backoff on 429 Too Many Requests.
// Other errors surface immediately.
func (c *Client) getWithRetry(ctx context.Context, u string) ([]byte, error) {
	wait := c.BackoffMin
	if wait <= 0 {
		wait = 500 * time.Millisecond
	}
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := readClose(resp)
		if readErr != nil {
			return nil, readErr
		}
		switch resp.StatusCode {
		case http.StatusOK:
			return body, nil
		case http.StatusTooManyRequests:
			if attempt == c.MaxRetries {
				return nil, fmt.Errorf("TMDb: %d after %d retries", resp.StatusCode, attempt)
			}
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
			wait *= 2
			continue
		default:
			return nil, fmt.Errorf("TMDb: %s %s -> %d", req.Method, u, resp.StatusCode)
		}
	}
	return nil, errors.New("TMDb: unreachable")
}

// PosterURL builds an absolute image URL from a TMDb relative path.
// size is e.g. "w500", "w780", "w1280", "original". An empty path returns "".
func PosterURL(relPath, size string) string {
	if relPath == "" {
		return ""
	}
	if size == "" {
		size = "w500"
	}
	if !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}
	return imageBaseURL + "/" + size + relPath
}

func readClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	const limit = 8 << 20 // 8 MiB; defensive bound on search responses
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
