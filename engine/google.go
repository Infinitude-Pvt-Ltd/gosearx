package engine

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"gosearx/config"
	"gosearx/scoring"
)

type GoogleEngine struct {
	*ScraperEngine
}

// NewGoogleEngine constructs a new Google search driver.
func NewGoogleEngine(cfg config.EngineConfig, client *http.Client, proxies []string) *GoogleEngine {
	return &GoogleEngine{
		ScraperEngine: NewScraperEngine(cfg, client, proxies),
	}
}

// Search executes a Google query, injecting specific GDPR cookies and headers to bypass redirects and bot detection.
func (g *GoogleEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}
	country := strings.ToLower(opts.Country)
	if country == "" {
		country = "us"
	}

	// 1. Format the search URL
	formattedURL := strings.ReplaceAll(g.searchURL, "{query}", url.QueryEscape(query))
	formattedURL = strings.ReplaceAll(formattedURL, "{language}", url.QueryEscape(lang))
	formattedURL = strings.ReplaceAll(formattedURL, "{country}", url.QueryEscape(country))

	// Append localization restrict rules to enforce search territories like SearXNG does
	if lang != "" {
		formattedURL += "&lr=lang_" + url.QueryEscape(lang)
	}
	if country != "" {
		formattedURL += "&cr=country" + url.QueryEscape(strings.ToUpper(country))
	}

	if g.usingJSRender {
		html, err := g.SearchDynamic(ctx, formattedURL)
		if err != nil {
			return nil, fmt.Errorf("google js rendering search failed: %w", err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, fmt.Errorf("failed to parse google rendered html: %w", err)
		}
		return g.parseHTML(doc, formattedURL), nil
	}
	
	var resp *http.Response
	var lastErr error
	maxAttempts := 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", formattedURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create google request: %w", err)
		}

		// 2. Browser emulation headers
		req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Referer", "https://www.google.com/")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
		req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
		req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		
		// 3. Inject GDPR consent cookies to bypass Google's consent wall redirect
		req.Header.Set("Cookie", "CONSENT=YES+; SOCS=CoYBOA")

		resp, err = g.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("google http request failed (attempt %d/%d): %w", attempt, maxAttempts, err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Check if redirected to Google Captcha page
		if resp.Request != nil && resp.Request.URL != nil {
			if strings.Contains(resp.Request.URL.Path, "/sorry") || strings.Contains(resp.Request.URL.Host, "sorry.google") {
				resp.Body.Close()
				lastErr = fmt.Errorf("google captcha triggered (sorry redirect) (attempt %d/%d)", attempt, maxAttempts)
				if attempt < maxAttempts {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				g.Suspend(10*time.Minute, "Suspended: captcha")
				return nil, lastErr
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = fmt.Errorf("google rate-limit triggered (429) (attempt %d/%d)", attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			g.Suspend(5*time.Minute, "Suspended: too many requests")
			return nil, lastErr
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			lastErr = fmt.Errorf("google access denied: %d (attempt %d/%d)", resp.StatusCode, attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			g.Suspend(10*time.Minute, "Suspended: access denied")
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("google returned status code %d (attempt %d/%d)", resp.StatusCode, attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return nil, lastErr
		}

		// Request succeeded, break
		lastErr = nil
		break
	}

	if lastErr != nil {
		// Log warning and fall back to JS rendering session
		fmt.Printf("[WARNING] Google standard scraper blocked: %v. Falling back to dynamic JS rendering session.\n", lastErr)
		html, err := g.SearchDynamic(ctx, formattedURL)
		if err != nil {
			return nil, fmt.Errorf("google fallback js rendering search failed: %w", err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, fmt.Errorf("failed to parse google fallback rendered html: %w", err)
		}
		return g.parseHTML(doc, formattedURL), nil
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse google html: %w", err)
	}

	// Parse results using standard selector engine
	results := g.parseHTML(doc, formattedURL)
	return results, nil
}
