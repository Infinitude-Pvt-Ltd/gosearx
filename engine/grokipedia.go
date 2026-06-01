package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"gosearx/config"
	"gosearx/scoring"
)

var (
	htmlRegex        = regexp.MustCompile(`<[^>]*>`)
	mdImageRegex     = regexp.MustCompile(`\!\[([^\]]*)\]\([^\)]*\)`)
	mdLinkRegex      = regexp.MustCompile(`\[([^\]]*)\]\([^\)]*\)`)
	mdLinkTruncRegex = regexp.MustCompile(`\[([^\]]*)\]\([^\)]*$`)
	spacesRegex      = regexp.MustCompile(`\s+`)
)

// GrokipediaResult matches the individual result items in the Grokipedia API response.
type GrokipediaResult struct {
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Snippet string `json:"snippet"`
}

// GrokipediaResponse matches the JSON payload structure of grokipedia.com API.
type GrokipediaResponse struct {
	Results []GrokipediaResult `json:"results"`
}

type GrokipediaEngine struct {
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

// NewGrokipediaEngine constructs a specialized Grokipedia API search driver.
func NewGrokipediaEngine(cfg config.EngineConfig, client *http.Client) *GrokipediaEngine {
	return &GrokipediaEngine{
		name:       cfg.Name,
		weight:     cfg.Weight,
		categories: cfg.Categories,
		disabled:   cfg.Disabled,
		timeout:    time.Duration(cfg.Timeout * float64(time.Second)),
		client:     client,
	}
}

func (g *GrokipediaEngine) Name() string        { return g.name }
func (g *GrokipediaEngine) Weight() float64     { return g.weight }
func (g *GrokipediaEngine) Categories() []string { return g.categories }
func (g *GrokipediaEngine) Disabled() bool      { return g.disabled }
func (g *GrokipediaEngine) Timeout() time.Duration { return g.timeout }

// Suspend marks this engine as suspended for the specified duration with a given reason.
func (g *GrokipediaEngine) Suspend(duration time.Duration, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.suspendedUntil = time.Now().Add(duration)
	g.suspensionReason = reason
}

// SuspendedReason returns the reason and whether this engine is currently suspended.
func (g *GrokipediaEngine) SuspendedReason() (string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if time.Now().Before(g.suspendedUntil) {
		return g.suspensionReason, true
	}
	return "", false
}

// Search queries the Grokipedia full-text search API and formats the response.
func (g *GrokipediaEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	apiURL := fmt.Sprintf("https://grokipedia.com/api/full-text-search?query=%s&limit=10&offset=0", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create grokipedia request: %w", err)
	}
	req.Header.Set("User-Agent", "GoSearX/1.0 (https://github.com/gosearx/gosearx)")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grokipedia http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grokipedia returned status code %d", resp.StatusCode)
	}

	var data GrokipediaResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode grokipedia json: %w", err)
	}

	var results []scoring.EngineResult
	for i, item := range data.Results {
		if item.Slug == "" || item.Title == "" {
			continue
		}

		// Grokipedia snippets might contain HTML tags and Markdown remnants, we sanitize them for clean text output
		cleanContent := cleanGrokipediaSnippet(item.Snippet)

		results = append(results, scoring.EngineResult{
			Title:    item.Title,
			URL:      "https://grokipedia.com/page/" + item.Slug,
			Content:  cleanContent,
			Engine:   g.name,
			Category: g.categories[0],
			Rank:     i + 1,
			Weight:   g.weight,
		})
	}

	return results, nil
}

// cleanGrokipediaSnippet cleans HTML tags, Markdown fragments, URL/slug paths, and stray punctuation from Grokipedia content.
func cleanGrokipediaSnippet(snippet string) string {
	// 1. Strip HTML tags
	s := htmlRegex.ReplaceAllString(snippet, " ")

	// 2. Strip Markdown images completely
	s = mdImageRegex.ReplaceAllString(s, " ")

	// 3. Keep text from Markdown links
	s = mdLinkRegex.ReplaceAllString(s, " $1 ")
	s = mdLinkTruncRegex.ReplaceAllString(s, " $1 ")

	// 4. Split into tokens to filter out URL slugs and broken markdown fragments
	words := strings.Fields(s)
	var cleanWords []string

	for _, word := range words {
		// Clean leading/trailing stray markdown brackets/parentheses/punctuation from the word
		cleaned := trimStrayMarkdown(word)
		if cleaned == "" {
			continue
		}

		if isSlugOrURL(cleaned) {
			continue
		}

		cleanWords = append(cleanWords, cleaned)
	}

	// Join and clean up extra spaces
	result := strings.Join(cleanWords, " ")
	result = spacesRegex.ReplaceAllString(result, " ")
	return strings.TrimSpace(result)
}

// trimStrayMarkdown removes leading/trailing brackets, parentheses, and other markdown leftovers.
func trimStrayMarkdown(word string) string {
	if isPureMarkdownPunct(word) {
		return ""
	}

	// Trim leading/trailing brackets/parens/backticks
	cutset := "[]()'`\"*_"
	word = strings.Trim(word, cutset)

	// After trimming, check again
	if isPureMarkdownPunct(word) {
		return ""
	}
	return word
}

// isPureMarkdownPunct returns true if the string is solely composed of markdown symbols or slashes.
func isPureMarkdownPunct(s string) bool {
	if len(s) == 0 {
		return true
	}
	for _, r := range s {
		if !strings.ContainsRune("[]()!#*_-/\\", r) {
			return false
		}
	}
	return true
}

// isSlugOrURL determines if a token is a URL, slug path, or a broken markdown url fragment.
func isSlugOrURL(word string) bool {
	w := strings.ToLower(word)

	// 1. Check for standard URL protocols or domains
	if strings.Contains(w, "http://") || strings.Contains(w, "https://") || strings.HasPrefix(w, "www.") {
		return true
	}
	if strings.Contains(w, ".com/") || strings.Contains(w, ".org/") || strings.Contains(w, ".net/") {
		return true
	}

	// 2. Check for typical slug-like path fragments
	if strings.Contains(w, "/") {
		dashes := strings.Count(w, "-")
		underscores := strings.Count(w, "_")
		slashes := strings.Count(w, "/")
		hasDigits := false
		for _, r := range w {
			if unicode.IsDigit(r) {
				hasDigits = true
				break
			}
		}

		// Slug detection criteria:
		// If it has digits, OR more than 1 slash, OR has hyphens/underscores, OR is very long
		if hasDigits || slashes > 1 || dashes > 0 || underscores > 0 || len(w) > 25 {
			return true
		}

		// If it starts or ends with a slash or hyphen
		if strings.HasPrefix(w, "/") || strings.HasSuffix(w, "/") || strings.HasPrefix(w, "-") || strings.HasSuffix(w, "-") {
			return true
		}
	} else {
		// Even without a slash, some stray slug fragments might be left over:
		// e.g. "one-day-internationals-2" (no slash but has numbers and multiple hyphens)
		dashes := strings.Count(w, "-")
		underscores := strings.Count(w, "_")
		hasDigits := false
		for _, r := range w {
			if unicode.IsDigit(r) {
				hasDigits = true
				break
			}
		}
		if (dashes + underscores) > 2 || (hasDigits && (dashes + underscores) > 0) {
			return true
		}
	}

	return false
}
