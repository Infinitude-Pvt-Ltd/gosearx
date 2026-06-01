package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gosearx/config"
	"gosearx/scoring"
)

// WikipediaResponse matches the official Wikipedia Page Summary REST API response.
type WikipediaResponse struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	DisplayTitle string `json:"displaytitle"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
	Thumbnail   struct {
		Source string `json:"source"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"thumbnail"`
	ContentURLs struct {
		Desktop struct {
			Page string `json:"page"`
		} `json:"desktop"`
	} `json:"content_urls"`
}

type WikipediaEngine struct {
	name             string
	weight           float64
	categories       []string
	disabled         bool
	timeout          time.Duration
	client           *http.Client
	suspendedUntil   time.Time
	suspensionReason string
	mu               sync.RWMutex
}

// NewWikipediaEngine constructs a new custom Wikipedia API search driver.
func NewWikipediaEngine(cfg config.EngineConfig, client *http.Client) *WikipediaEngine {
	return &WikipediaEngine{
		name:       cfg.Name,
		weight:     cfg.Weight,
		categories: cfg.Categories,
		disabled:   cfg.Disabled,
		timeout:    time.Duration(cfg.Timeout * float64(time.Second)),
		client:     client,
	}
}

func (w *WikipediaEngine) Name() string        { return w.name }
func (w *WikipediaEngine) Weight() float64     { return w.weight }
func (w *WikipediaEngine) Categories() []string { return w.categories }
func (w *WikipediaEngine) Disabled() bool      { return w.disabled }
func (w *WikipediaEngine) Timeout() time.Duration { return w.timeout }

// Suspend marks this engine as suspended for the specified duration with a given reason.
func (w *WikipediaEngine) Suspend(duration time.Duration, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.suspendedUntil = time.Now().Add(duration)
	w.suspensionReason = reason
}

// SuspendedReason returns the reason and whether this engine is currently suspended.
func (w *WikipediaEngine) SuspendedReason() (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if time.Now().Before(w.suspendedUntil) {
		return w.suspensionReason, true
	}
	return "", false
}

// Search queries the official Wikipedia Page Summary API.
func (w *WikipediaEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	// Standardize query: capitalize terms to match Wikipedia article casing style
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	
	// E.g. "open source" -> "Open_Source"
	title := strings.Title(query)
	title = strings.ReplaceAll(title, " ", "_")

	// Determine Wikipedia language subdomain (default to "en")
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}
	// Sanitize language tag just in case
	lang = strings.Split(lang, "-")[0]

	// Target the specific language Wikipedia summary endpoint
	apiURL := fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s", lang, url.PathEscape(title))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "GoSearX/1.0 (https://github.com/gosearx/gosearx)")
	req.Header.Set("Accept", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Article not found is not a hard error, just return empty results
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-ok status: %d", resp.StatusCode)
	}

	var data WikipediaResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode json response: %w", err)
	}

	if data.ContentURLs.Desktop.Page == "" {
		return nil, nil
	}

	// Content extracts are mapped as a premium informational result
	contentSnippet := data.Extract
	if contentSnippet == "" {
		contentSnippet = data.Description
	}

	results := []scoring.EngineResult{
		{
			Title:    data.Title,
			URL:      data.ContentURLs.Desktop.Page,
			Content:  contentSnippet,
			Engine:   w.name,
			Category: w.categories[0],
			Rank:     1,
			Weight:   w.weight,
		},
	}

	return results, nil
}
