package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// TVResult is the projection of TMDb's search/tv hit relevant to us.
type TVResult struct {
	TMDbID       int
	Title        string
	OriginalName string
	Year         int
	Description  string
	PosterPath   string
	BackdropPath string
	Rating       float64
}

// EpisodeResult is the per-episode payload from tv/{id}/season/{n}/episode/{n}.
type EpisodeResult struct {
	Name        string
	Description string
	StillPath   string
	AirDate     string
	Rating      float64
}

// SearchTV queries /search/tv?query=&first_air_date_year= and returns the
// first hit.
func (c *Client) SearchTV(ctx context.Context, query string, year int) (TVResult, bool, error) {
	if c.APIKey == "" {
		return TVResult{}, false, ErrNoAPIKey
	}
	if strings.TrimSpace(query) == "" {
		return TVResult{}, false, errors.New("TMDb: empty query")
	}
	params := url.Values{}
	params.Set("api_key", c.APIKey)
	params.Set("language", c.Language)
	params.Set("query", query)
	params.Set("include_adult", "false")
	if year > 0 {
		params.Set("first_air_date_year", strconv.Itoa(year))
	}
	u := c.BaseURL + "/search/tv?" + params.Encode()

	body, err := c.getWithRetry(ctx, u)
	if err != nil {
		return TVResult{}, false, err
	}
	var resp struct {
		Results []struct {
			ID           int     `json:"id"`
			Name         string  `json:"name"`
			OriginalName string  `json:"original_name"`
			Overview     string  `json:"overview"`
			FirstAirDate string  `json:"first_air_date"`
			PosterPath   string  `json:"poster_path"`
			BackdropPath string  `json:"backdrop_path"`
			VoteAverage  float64 `json:"vote_average"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return TVResult{}, false, fmt.Errorf("TMDb decode: %w", err)
	}
	if len(resp.Results) == 0 {
		return TVResult{}, false, nil
	}
	hit := resp.Results[0]
	r := TVResult{
		TMDbID:       hit.ID,
		Title:        hit.Name,
		OriginalName: hit.OriginalName,
		Description:  hit.Overview,
		PosterPath:   hit.PosterPath,
		BackdropPath: hit.BackdropPath,
		Rating:       hit.VoteAverage,
	}
	if len(hit.FirstAirDate) >= 4 {
		if y, err := strconv.Atoi(hit.FirstAirDate[:4]); err == nil {
			r.Year = y
		}
	}
	return r, true, nil
}

// GetEpisode queries /tv/{tvID}/season/{season}/episode/{episode}.
// Returns the zero EpisodeResult and (false, nil) when the API returns 404
// (e.g. the episode doesn't exist in TMDb yet — common for very new shows).
func (c *Client) GetEpisode(ctx context.Context, tvID, season, episode int) (EpisodeResult, bool, error) {
	if c.APIKey == "" {
		return EpisodeResult{}, false, ErrNoAPIKey
	}
	if tvID <= 0 || season <= 0 || episode <= 0 {
		return EpisodeResult{}, false, errors.New("TMDb: tvID, season, episode must be positive")
	}
	params := url.Values{}
	params.Set("api_key", c.APIKey)
	params.Set("language", c.Language)
	u := fmt.Sprintf("%s/tv/%d/season/%d/episode/%d?%s",
		c.BaseURL, tvID, season, episode, params.Encode())

	body, err := c.getWithRetry(ctx, u)
	if err != nil {
		// 404 → "episode not in TMDb"; we treat as soft miss for callers.
		if strings.Contains(err.Error(), "-> 404") {
			return EpisodeResult{}, false, nil
		}
		return EpisodeResult{}, false, err
	}
	var resp struct {
		Name        string  `json:"name"`
		Overview    string  `json:"overview"`
		StillPath   string  `json:"still_path"`
		AirDate     string  `json:"air_date"`
		VoteAverage float64 `json:"vote_average"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return EpisodeResult{}, false, fmt.Errorf("TMDb decode: %w", err)
	}
	return EpisodeResult{
		Name:        resp.Name,
		Description: resp.Overview,
		StillPath:   resp.StillPath,
		AirDate:     resp.AirDate,
		Rating:      resp.VoteAverage,
	}, true, nil
}
