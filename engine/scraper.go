package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"gosearx/config"
	"gosearx/scoring"
)

// UserAgents list for browser emulation
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/120.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/120.0",
}

// ScraperEngine is a search engine driver that parses HTML using dynamic CSS selectors and semantic heuristics.
type ScraperEngine struct {
	name             string
	searchURL        string
	weight           float64
	categories       []string
	disabled         bool
	timeout          time.Duration
	selectors        config.SelectorConfig
	client           *http.Client
	suspendedUntil   time.Time
	suspensionReason string
	usingJSRender    bool
	proxies          []string
	mu               sync.RWMutex
}

// NewScraperEngine constructs a ScraperEngine using configuration settings.
func NewScraperEngine(cfg config.EngineConfig, client *http.Client, proxies []string) *ScraperEngine {
	return &ScraperEngine{
		name:          cfg.Name,
		searchURL:     cfg.SearchURL,
		weight:        cfg.Weight,
		categories:    cfg.Categories,
		disabled:      cfg.Disabled,
		timeout:       time.Duration(cfg.Timeout * float64(time.Second)),
		selectors:     cfg.Selectors,
		client:        client,
		usingJSRender: cfg.UsingJSRender,
		proxies:       proxies,
	}
}

func (s *ScraperEngine) SearchDynamic(ctx context.Context, targetURL string) (string, error) {
	var proxyStr string
	if s.proxies != nil && len(s.proxies) > 0 {
		proxyStr = s.proxies[rand.Intn(len(s.proxies))]
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	// Pick a random user agent
	userAgent := userAgents[rand.Intn(len(userAgents))]
	opts = append(opts, chromedp.Flag("user-agent", userAgent))

	if proxyStr != "" {
		opts = append(opts, chromedp.Flag("proxy-server", proxyStr))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	chromeCtx, cancelChrome := chromedp.NewContext(allocCtx)
	defer cancelChrome()

	var htmlContent string
	var tasks chromedp.Tasks

	// Evasion / Fingerprint masking: remove navigator.webdriver
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument("delete navigator.__proto__.webdriver;").Do(ctx)
		return err
	}))

	// Bypass Google Consent Redirect Page inside Headless session by injecting consent cookies
	if strings.Contains(targetURL, "google.com") {
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			err := network.SetCookie("CONSENT", "YES+").WithDomain(".google.com").WithPath("/").Do(ctx)
			if err != nil {
				return err
			}
			err = network.SetCookie("SOCS", "CoYBOA").WithDomain(".google.com").WithPath("/").Do(ctx)
			if err != nil {
				return err
			}
			return nil
		}))
	}

	// Navigation
	tasks = append(tasks, chromedp.Navigate(targetURL))

	// Fetch outer DOM
	tasks = append(tasks, chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery))

	err := chromedp.Run(chromeCtx, tasks)
	if err != nil {
		return "", err
	}

	if strings.Contains(htmlContent, "captcha") || strings.Contains(htmlContent, "sorry.google.com") || strings.Contains(htmlContent, "unusual traffic") {
		return "", fmt.Errorf("captcha triggered in dynamic browser session")
	}

	return htmlContent, nil
}

func (s *ScraperEngine) Name() string        { return s.name }
func (s *ScraperEngine) Weight() float64     { return s.weight }
func (s *ScraperEngine) Categories() []string { return s.categories }
func (s *ScraperEngine) Disabled() bool      { return s.disabled }
func (s *ScraperEngine) Timeout() time.Duration { return s.timeout }

// Suspend marks this engine as suspended for the specified duration with a given reason.
func (s *ScraperEngine) Suspend(duration time.Duration, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suspendedUntil = time.Now().Add(duration)
	s.suspensionReason = reason
}

// SuspendedReason returns the reason and whether this engine is currently suspended.
func (s *ScraperEngine) SuspendedReason() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if time.Now().Before(s.suspendedUntil) {
		return s.suspensionReason, true
	}
	return "", false
}

// Search executes the scraper search request, parses the HTML, and applies the dynamic selectors/fallbacks.
func (s *ScraperEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}
	country := strings.ToLower(opts.Country)
	if country == "" {
		country = "us"
	}

	// Format the search query URL with query, language and country placeholders
	formattedURL := strings.ReplaceAll(s.searchURL, "{query}", url.QueryEscape(query))
	formattedURL = strings.ReplaceAll(formattedURL, "{language}", url.QueryEscape(lang))
	formattedURL = strings.ReplaceAll(formattedURL, "{country}", url.QueryEscape(country))

	if s.usingJSRender {
		html, err := s.SearchDynamic(ctx, formattedURL)
		if err != nil {
			return nil, fmt.Errorf("js rendering search failed: %w", err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return nil, fmt.Errorf("failed to parse rendered html: %w", err)
		}
		return s.parseHTML(doc, formattedURL), nil
	}
	
	var resp *http.Response
	var lastErr error
	maxAttempts := 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "GET", formattedURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Emulate standard web browser headers
		req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Sec-Ch-Ua", `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`)
		req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
		req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
		req.Header.Set("Upgrade-Insecure-Requests", "1")

		resp, err = s.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http request failed (attempt %d/%d): %w", attempt, maxAttempts, err)
			time.Sleep(100 * time.Millisecond) // brief delay before retry
			continue
		}

		// Check if we were redirected to a sorry/captcha page (very common for Google block pages)
		if resp.Request != nil && resp.Request.URL != nil {
			if strings.Contains(resp.Request.URL.Path, "/sorry") || strings.Contains(resp.Request.URL.Host, "sorry.google") {
				resp.Body.Close()
				lastErr = fmt.Errorf("captcha triggered (sorry redirect) (attempt %d/%d)", attempt, maxAttempts)
				if attempt < maxAttempts {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				s.Suspend(5*time.Minute, "Suspended: captcha")
				return nil, lastErr
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = fmt.Errorf("received 429 Too Many Requests (attempt %d/%d)", attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			s.Suspend(2*time.Minute, "Suspended: too many requests")
			return nil, lastErr
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			lastErr = fmt.Errorf("received access denied: %d (attempt %d/%d)", resp.StatusCode, attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			s.Suspend(5*time.Minute, "Suspended: access denied")
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("received non-ok status: %d (attempt %d/%d)", resp.StatusCode, attempt, maxAttempts)
			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return nil, lastErr
		}

		// Request succeeded, break from loop
		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, lastErr
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse html document: %w", err)
	}

	results := s.parseHTML(doc, formattedURL)
	return results, nil
}

// parseHTML extracts search result lists from the document using multi-selector testing and semantic fallbacks.
func (s *ScraperEngine) parseHTML(doc *goquery.Document, requestURL string) []scoring.EngineResult {
	var results []scoring.EngineResult
	baseURI, err := url.Parse(requestURL)
	if err != nil {
		baseURI = &url.URL{}
	}

	// 1. Try configured result selectors in priority order
	var resultNodes *goquery.Selection
	for _, sel := range s.selectors.Result {
		nodes := doc.Find(sel)
		if nodes.Length() > 0 {
			resultNodes = nodes
			break
		}
	}

	// 2. Class-Agnostic Semantic Fallback: if class-based selectors fail, trigger structural parsing.
	if resultNodes == nil || resultNodes.Length() == 0 {
		// Heuristic: Search engines represent search results as a link 'a' tag nesting an h2/h3 header tag
		// We look for all `a` tags containing `h2`, `h3`, or `h4`!
		var fallbackNodes []*goquery.Selection
		doc.Find("a").Each(func(i int, link *goquery.Selection) {
			if link.Find("h2, h3, h4").Length() > 0 {
				// The result card is usually the link's enclosing container (parent or parent's parent)
				parent := link.Parent()
				if parent.Length() > 0 {
					// Exclude header, footer, or navigation elements
					pTagName := strings.ToLower(goquery.NodeName(parent))
					if pTagName != "header" && pTagName != "footer" && pTagName != "nav" {
						fallbackNodes = append(fallbackNodes, parent)
					}
				}
			}
		})

		// Parse from semantic fallbacks if found
		if len(fallbackNodes) > 0 {
			for idx, card := range fallbackNodes {
				titleNode := card.Find("h2, h3, h4").First()
				linkNode := card.Find("a").First()
				
				// Try to extract content snippet from the enclosing card (all text nodes minus the title)
				titleText := strings.TrimSpace(titleNode.Text())
				cardText := strings.TrimSpace(card.Text())
				content := strings.TrimSpace(strings.Replace(cardText, titleText, "", 1))
				
				rawURL, exists := linkNode.Attr("href")
				if !exists || rawURL == "" || titleText == "" {
					continue
				}

				absoluteURL := resolveRelativeURL(baseURI, rawURL)
				results = append(results, scoring.EngineResult{
					Title:    titleText,
					URL:      absoluteURL,
					Content:  cleanSnippet(content),
					Engine:   s.name,
					Category: s.categories[0],
					Rank:     idx + 1,
					Weight:   s.weight,
				})
			}
			return results
		}
		return nil
	}

	// 3. Parse result containers using priority selectors
	rank := 1
	resultNodes.Each(func(i int, item *goquery.Selection) {
		// Extract Title
		var title string
		for _, sel := range s.selectors.Title {
			tNode := item.Find(sel)
			if tNode.Length() > 0 {
				title = strings.TrimSpace(tNode.Text())
				break
			}
		}

		// Extract Link
		var rawURL string
		var urlFound bool
		for _, sel := range s.selectors.URL {
			uNode := item.Find(sel)
			if uNode.Length() > 0 {
				rawURL, urlFound = uNode.Attr("href")
				if urlFound && rawURL != "" {
					break
				}
			}
		}
		// If not found in explicit selector, try the first generic anchor in container
		if !urlFound || rawURL == "" {
			rawURL, urlFound = item.Find("a").Attr("href")
		}

		// Extract Content Snippet
		var content string
		for _, sel := range s.selectors.Content {
			cNode := item.Find(sel)
			if cNode.Length() > 0 {
				content = strings.TrimSpace(cNode.Text())
				break
			}
		}

		// Skip invalid records
		if rawURL == "" || title == "" {
			return
		}

		absoluteURL := resolveRelativeURL(baseURI, rawURL)
		results = append(results, scoring.EngineResult{
			Title:    title,
			URL:      absoluteURL,
			Content:  cleanSnippet(content),
			Engine:   s.name,
			Category: s.categories[0],
			Rank:     rank,
			Weight:   s.weight,
		})
		rank++
	})

	return results
}

// resolveRelativeURL converts a relative search engine link (e.g. `/url?q=...`) to an absolute link,
// and extracts the target URL from search engine redirect/tracking links (Google, DuckDuckGo, Bing, Yahoo).
func resolveRelativeURL(baseURI *url.URL, relativeStr string) string {
	relativeStr = strings.TrimSpace(relativeStr)
	if strings.HasPrefix(relativeStr, "//") {
		relativeStr = baseURI.Scheme + ":" + relativeStr
	}
	
	// Handle search engine tracking and redirection links
	if strings.Contains(relativeStr, "/url?") || strings.Contains(relativeStr, "google.com/url?") ||
		strings.Contains(relativeStr, "/l/?") || strings.Contains(relativeStr, "duckduckgo.com/l/?") ||
		strings.Contains(relativeStr, "yahoo.com/url") || strings.Contains(relativeStr, "/url/") ||
		strings.Contains(relativeStr, "bing.com/ck/") {
		
		parsed, err := url.Parse(relativeStr)
		if err == nil {
			query := parsed.Query()
			// DuckDuckGo tracking parameter
			if uddg := query.Get("uddg"); uddg != "" {
				return uddg
			}
			// Google/Yahoo tracking parameters
			if q := query.Get("q"); q != "" {
				return q
			}
			if urlParam := query.Get("url"); urlParam != "" {
				return urlParam
			}
			// Bing tracking parameter (Base64 encoded with "a1" prefix)
			if u := query.Get("u"); u != "" {
				if strings.HasPrefix(u, "a1") && len(u) > 2 {
					base64Str := u[2:]
					// Standardize padding for base64 decoding
					if pad := len(base64Str) % 4; pad != 0 {
						base64Str += strings.Repeat("=", 4-pad)
					}
					// Decode safely using standard base64 alphabet after replacing URL safe ones
					base64Str = strings.ReplaceAll(base64Str, "-", "+")
					base64Str = strings.ReplaceAll(base64Str, "_", "/")
					
					if decoded, err := base64.StdEncoding.DecodeString(base64Str); err == nil {
						return string(decoded)
					}
				}
				return u
			}
		}
	}

	parsed, err := url.Parse(relativeStr)
	if err != nil {
		return relativeStr
	}

	if parsed.IsAbs() {
		return relativeStr
	}

	return baseURI.ResolveReference(parsed).String()
}

// cleanSnippet trims spaces and fixes encoding bugs in scrapped descriptions.
func cleanSnippet(snippet string) string {
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	snippet = strings.ReplaceAll(snippet, "\r", "")
	for strings.Contains(snippet, "  ") {
		snippet = strings.ReplaceAll(snippet, "  ", " ")
	}
	return strings.TrimSpace(snippet)
}
