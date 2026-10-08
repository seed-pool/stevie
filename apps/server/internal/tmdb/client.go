package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

const baseURL = "https://api.themoviedb.org/3"
const imageBase = "https://image.tmdb.org/t/p"

// DefaultRPS is a conservative ceiling under TMDB's ~40 req/s CDN limit.
// Staff guidance is approximately 40 requests/second per IP; staying near 20
// leaves headroom for artwork fetches and retries.
const DefaultRPS = 20.0

type Client struct {
	apiKey  string
	http    *http.Client
	rdb     *redis.Client
	ttl     time.Duration
	limiter *rate.Limiter
}

func New(apiKey string, rdb *redis.Client, ttl time.Duration) *Client {
	return NewWithRPS(apiKey, rdb, ttl, DefaultRPS)
}

func NewWithRPS(apiKey string, rdb *redis.Client, ttl time.Duration, rps float64) *Client {
	if rps <= 0 {
		rps = DefaultRPS
	}
	burst := int(rps)
	if burst < 1 {
		burst = 1
	}
	if burst > 10 {
		burst = 10
	}
	return &Client{
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 20 * time.Second},
		rdb:     rdb,
		ttl:     ttl,
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

type SearchResult struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	Overview     string  `json:"overview"`
	VoteAverage  float64 `json:"vote_average"`
	MediaType    string  `json:"media_type"`
}

type MovieDetails struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Tagline       string  `json:"tagline"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	Runtime       int     `json:"runtime"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	Popularity    float64 `json:"popularity"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Genres        []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Credits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
			Order       int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Job        string `json:"job"`
			Department string `json:"department"`
		} `json:"crew"`
	} `json:"credits"`
	ExternalIDs map[string]any `json:"external_ids"`
}

type ShowDetails struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	Tagline      string  `json:"tagline"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Popularity   float64 `json:"popularity"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	Genres       []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	Credits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			ProfilePath string `json:"profile_path"`
			Order       int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Job        string `json:"job"`
			Department string `json:"department"`
		} `json:"crew"`
	} `json:"credits"`
	ExternalIDs map[string]any `json:"external_ids"`
}

type EpisodeDetails struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	StillPath     string  `json:"still_path"`
	AirDate       string  `json:"air_date"`
	Runtime       int     `json:"runtime"`
	VoteAverage   float64 `json:"vote_average"`
	SeasonNumber  int     `json:"season_number"`
	EpisodeNumber int     `json:"episode_number"`
}

type SeasonDetails struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	AirDate      string `json:"air_date"`
	SeasonNumber int    `json:"season_number"`
}

func (c *Client) SearchMovie(ctx context.Context, title string, year int) ([]SearchResult, error) {
	results, err := c.searchMovieOnce(ctx, title, year)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 && year > 0 {
		results, err = c.searchMovieOnce(ctx, title, 0)
		if err != nil {
			return nil, err
		}
	}
	return RankResults(results, title, year, true), nil
}

func (c *Client) searchMovieOnce(ctx context.Context, title string, year int) ([]SearchResult, error) {
	q := url.Values{}
	q.Set("query", title)
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var resp struct {
		Results []SearchResult `json:"results"`
	}
	if err := c.get(ctx, "/search/movie", q, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (c *Client) SearchTV(ctx context.Context, title string, year int) ([]SearchResult, error) {
	results, err := c.searchTVOnce(ctx, title, year)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 && year > 0 {
		results, err = c.searchTVOnce(ctx, title, 0)
		if err != nil {
			return nil, err
		}
	}
	return RankResults(results, title, year, false), nil
}

func (c *Client) searchTVOnce(ctx context.Context, title string, year int) ([]SearchResult, error) {
	q := url.Values{}
	q.Set("query", title)
	if year > 0 {
		q.Set("first_air_date_year", strconv.Itoa(year))
	}
	var resp struct {
		Results []SearchResult `json:"results"`
	}
	if err := c.get(ctx, "/search/tv", q, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// RankResults prefers exact title matches and optional year alignment.
func RankResults(results []SearchResult, title string, year int, movie bool) []SearchResult {
	if len(results) < 2 {
		return results
	}
	want := strings.ToLower(strings.TrimSpace(title))
	score := func(r SearchResult) int {
		name := strings.ToLower(strings.TrimSpace(r.Name))
		if movie {
			name = strings.ToLower(strings.TrimSpace(r.Title))
		}
		s := 0
		if name == want {
			s += 100
		} else if strings.Contains(name, want) || strings.Contains(want, name) {
			s += 40
		}
		date := r.FirstAirDate
		if movie {
			date = r.ReleaseDate
		}
		if year > 0 && len(date) >= 4 {
			if y, err := strconv.Atoi(date[:4]); err == nil && y == year {
				s += 50
			}
		}
		return s
	}
	out := append([]SearchResult(nil), results...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if score(out[j]) > score(out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (c *Client) GetMovie(ctx context.Context, id int) (MovieDetails, error) {
	var out MovieDetails
	q := url.Values{}
	q.Set("append_to_response", "credits,external_ids")
	err := c.get(ctx, "/movie/"+strconv.Itoa(id), q, &out)
	return out, err
}

func (c *Client) GetShow(ctx context.Context, id int) (ShowDetails, error) {
	var out ShowDetails
	q := url.Values{}
	q.Set("append_to_response", "credits,external_ids")
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), q, &out)
	return out, err
}

func (c *Client) GetEpisode(ctx context.Context, showID, season, episode int) (EpisodeDetails, error) {
	var out EpisodeDetails
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	err := c.get(ctx, path, nil, &out)
	return out, err
}

func (c *Client) GetSeason(ctx context.Context, showID, season int) (SeasonDetails, error) {
	var out SeasonDetails
	path := fmt.Sprintf("/tv/%d/season/%d", showID, season)
	err := c.get(ctx, path, nil, &out)
	return out, err
}

func ImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "original"
	}
	return imageBase + "/" + size + path
}

func (c *Client) get(ctx context.Context, path string, query url.Values, dest any) error {
	if !c.Enabled() {
		return fmt.Errorf("tmdb api key not configured")
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("api_key", c.apiKey)
	full := baseURL + path + "?" + query.Encode()
	cacheKey := "tmdb:" + path + "?" + query.Encode()

	if c.rdb != nil {
		if cached, err := c.rdb.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
			return json.Unmarshal(cached, dest)
		}
	}

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp, time.Duration(attempt+1)*time.Second)
			lastErr = fmt.Errorf("tmdb %s: status 429", path)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
				continue
			}
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("tmdb %s: status %d: %s", path, resp.StatusCode, string(body))
		}
		if c.rdb != nil {
			_ = c.rdb.Set(ctx, cacheKey, body, c.ttl).Err()
		}
		return json.Unmarshal(body, dest)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("tmdb %s: exceeded retries", path)
	}
	return lastErr
}

func retryAfter(resp *http.Response, fallback time.Duration) time.Duration {
	if resp == nil {
		return fallback
	}
	raw := resp.Header.Get("Retry-After")
	if raw == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return fallback
}
