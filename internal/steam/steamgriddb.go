package steam

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mili/moxie/internal/log"
)

// ---------------------------------------------------------------------------
// SteamGridDB v2 API client
// ---------------------------------------------------------------------------

// SGDBClient is a rate-limited HTTP client for the SteamGridDB v2 REST API.
type SGDBClient struct {
	apiKey            string
	http              *http.Client
	mu                sync.Mutex
	lastReq           time.Time
	minDelay          time.Duration // 1050ms minimum between API requests
	lastImageDownload time.Time
	cdnMinDelay       time.Duration // 200ms minimum between image downloads
}

// CDNMinDelay is the minimum delay between SteamGridDB CDN image downloads.
const CDNMinDelay = 200 * time.Millisecond

// NewSGDBClient creates a SteamGridDB API client with the given API key.
// The free tier allows 1 req/s and 200 req/day.
func NewSGDBClient(apiKey string) *SGDBClient {
	return &SGDBClient{
		apiKey:      apiKey,
		http:        &http.Client{Timeout: 30 * time.Second},
		minDelay:    1050 * time.Millisecond,
		cdnMinDelay: CDNMinDelay,
	}
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

// sgdbAutocompleteResponse is the wrapper JSON returned by the
// /search/autocomplete/{term} endpoint.
type sgdbAutocompleteResponse struct {
	Success bool             `json:"success"`
	Data    []SGDBGameResult `json:"data"`
	Errors  []string         `json:"errors"`
}

// SearchGame searches SteamGridDB for a game by name.
// Returns up to 10 results with their SGDB ID and name.
func (c *SGDBClient) SearchGame(term string) ([]SGDBGameResult, error) {
	respBody, err := c.doGet("/search/autocomplete/" + url.PathEscape(term))
	if err != nil {
		return nil, err
	}
	var wrapper sgdbAutocompleteResponse
	if err := json.Unmarshal(respBody, &wrapper); err != nil {
		return nil, fmt.Errorf("steamgriddb: parse error: %w", err)
	}
	if !wrapper.Success {
		return nil, fmt.Errorf("steamgriddb: %s", strings.Join(wrapper.Errors, "; "))
	}
	// Limit to 10 results.
	if len(wrapper.Data) > 10 {
		wrapper.Data = wrapper.Data[:10]
	}
	return wrapper.Data, nil
}

// GetGridsBySteamAppID fetches available grid images for a real Steam App ID.
// dimensions filters by size (e.g., "600x900"). Empty string means no filter.
func (c *SGDBClient) GetGridsBySteamAppID(steamAppID int, dimensions string) ([]SGDBImageResult, error) {
	params := url.Values{}
	params.Set("mimes", "image/png")
	if dimensions != "" {
		params.Set("dimensions", dimensions)
	}
	path := fmt.Sprintf("/grids/steam/%d?%s", steamAppID, params.Encode())
	respBody, err := c.doGet(path)
	if err != nil {
		return nil, err
	}
	var results []SGDBImageResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("steamgriddb: parse error: %w", err)
	}
	return results, nil
}

// GetHeroesBySteamAppID fetches hero/banner images for a real Steam App ID.
func (c *SGDBClient) GetHeroesBySteamAppID(steamAppID int) ([]SGDBImageResult, error) {
	respBody, err := c.doGet(fmt.Sprintf("/heroes/steam/%d?mimes=image/png", steamAppID))
	if err != nil {
		return nil, err
	}
	var results []SGDBImageResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("steamgriddb: parse error: %w", err)
	}
	return results, nil
}

// GetGridsBySGDBGameID fetches grid images for a SteamGridDB game ID
// (obtained from SearchGame). dimensions filters by size (e.g., "600x900").
func (c *SGDBClient) GetGridsBySGDBGameID(gameID int, dimensions string) ([]SGDBImageResult, error) {
	params := url.Values{}
	params.Set("mimes", "image/png")
	if dimensions != "" {
		params.Set("dimensions", dimensions)
	}
	respBody, err := c.doGet(fmt.Sprintf("/grids/game/%d?%s", gameID, params.Encode()))
	if err != nil {
		return nil, err
	}
	var results []SGDBImageResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("steamgriddb: parse error: %w", err)
	}
	return results, nil
}

// GetHeroesBySGDBGameID fetches hero/banner images for a SteamGridDB game ID.
func (c *SGDBClient) GetHeroesBySGDBGameID(gameID int) ([]SGDBImageResult, error) {
	respBody, err := c.doGet(fmt.Sprintf("/heroes/game/%d?mimes=image/png", gameID))
	if err != nil {
		return nil, err
	}
	var results []SGDBImageResult
	if err := json.Unmarshal(respBody, &results); err != nil {
		return nil, fmt.Errorf("steamgriddb: parse error: %w", err)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Image download
// ---------------------------------------------------------------------------

// DownloadImage downloads an image from url and saves it to destPath.
// Creates parent directories if needed.
func (c *SGDBClient) DownloadImage(imgURL, destPath string) error {
	if !isValidDownloadURL(imgURL) {
		return ErrInvalidURL
	}

	// CDN rate limit: ensure minimum delay between image downloads.
	c.mu.Lock()
	elapsed := time.Since(c.lastImageDownload)
	if elapsed < c.cdnMinDelay {
		c.mu.Unlock()
		time.Sleep(c.cdnMinDelay - elapsed)
		c.mu.Lock()
	}
	c.lastImageDownload = time.Now()
	c.mu.Unlock()

	resp, err := c.http.Get(imgURL)
	if err != nil {
		log.Error("steamgriddb download failed", "url", imgURL, "error", err)
		return fmt.Errorf("steamgriddb: download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Error("steamgriddb download HTTP error", "url", imgURL, "status", resp.StatusCode)
		return fmt.Errorf("steamgriddb: download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("steamgriddb: cannot create dir: %w", err)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("steamgriddb: cannot create file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(destPath)
		return fmt.Errorf("steamgriddb: write failed: %w", err)
	}
	// fsync before close to prevent empty/corrupt file on crash.
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(destPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(destPath)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// BestGridImage picks the best grid image from an SGDB result list by score,
// skipping any results with non-HTTP URLs (data: URIs, SVGs).
// Returns the URL and true, or "" and false if no suitable image is found.
func BestGridImage(results []SGDBImageResult) (string, bool) {
	var best *SGDBImageResult
	for i, r := range results {
		// Skip data: URIs and SVGs — we can't decode these.
		if strings.HasPrefix(r.URL, "data:") || strings.Contains(r.URL, ".svg") {
			continue
		}
		if best == nil || r.Score > best.Score {
			best = &results[i]
		}
	}
	if best == nil {
		return "", false
	}
	return best.URL, true
}

// doGet performs a rate-limited GET request to the SteamGridDB API.
func (c *SGDBClient) doGet(path string) ([]byte, error) {
	c.mu.Lock()
	elapsed := time.Since(c.lastReq)
	if elapsed < c.minDelay {
		c.mu.Unlock()
		time.Sleep(c.minDelay - elapsed)
		c.mu.Lock()
	}
	c.lastReq = time.Now()
	c.mu.Unlock()

	req, err := http.NewRequest("GET", "https://www.steamgriddb.com/api/v2"+path, nil)
	if err != nil {
		return nil, fmt.Errorf("steamgriddb: request error: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("steamgriddb: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("steamgriddb: read error: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Error("steamgriddb API error",
			"path", path,
			"status", resp.StatusCode,
			"body", string(body),
		)
		// Truncate body in error message to avoid leaking sensitive response data.
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("steamgriddb: HTTP %d: %s", resp.StatusCode, bodyStr)
	}
	return body, nil
}
