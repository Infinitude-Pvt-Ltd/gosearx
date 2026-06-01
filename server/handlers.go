package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/go-shiori/go-readability"
	"gosearx/cache"
	"gosearx/config"
	"gosearx/engine"
	"gosearx/scoring"
)

// SearchResponse defines the standard high-fidelity JSON payload returned by the search API.
type SearchResponse struct {
	Query               string                 `json:"query"`
	Results             []scoring.SearchResult `json:"results"`
	UnresponsiveEngines [][]string             `json:"unresponsive_engines"`
	Metrics             []engine.EngineMetrics `json:"metrics"`
	Count               int                    `json:"count"`
	ProcessingTimeMS    int64                  `json:"processing_time_ms"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HandlerContext struct {
	Config   *config.Config
	Registry *engine.Registry
	Cache    cache.SearchCache
	Client   *http.Client
}

// NewHandlerContext initializes a new shared handlers context.
func NewHandlerContext(cfg *config.Config, reg *engine.Registry, c cache.SearchCache, client *http.Client) *HandlerContext {
	return &HandlerContext{
		Config:   cfg,
		Registry: reg,
		Cache:    c,
		Client:   client,
	}
}

// RequireAuth is a higher-order middleware verifying Bearer Token authentication.
func (h *HandlerContext) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If API key is not configured, bypass auth
		if len(h.Config.General.APIKeys) == 0 {
			next(w, r)
			return
		}

		// 1. Get Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "unauthorized: missing Authorization header"})
			return
		}

		// 2. Parse Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "unauthorized: invalid Authorization header format. Use 'Bearer <token>'"})
			return
		}

		token := parts[1]

		// 3. Verify key against configured keys
		authorized := false
		for _, key := range h.Config.General.APIKeys {
			if token == key {
				authorized = true
				break
			}
		}

		if !authorized {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "unauthorized: invalid API key"})
			return
		}

		// Authorized, proceed to next handler
		next(w, r)
	}
}

// getClientIP extracts the client IP address from request headers or RemoteAddr.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// LimitRate is a middleware verifying the rate limit for a client IP.
func (h *HandlerContext) LimitRate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Cache == nil {
			next(w, r)
			return
		}

		ip := getClientIP(r)
		key := "ratelimit:" + ip
		now := time.Now().UnixNano()
		window := 10 * time.Second
		maxRequests := 5

		var timestamps []int64

		val, found := h.Cache.Get(r.Context(), key)
		if found {
			if err := json.Unmarshal([]byte(val), &timestamps); err == nil {
				// Filter out timestamps older than 10 seconds
				cutoff := now - window.Nanoseconds()
				var filtered []int64
				for _, ts := range timestamps {
					if ts > cutoff {
						filtered = append(filtered, ts)
					}
				}
				timestamps = filtered
			}
		}

		if len(timestamps) >= maxRequests {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "Too many requests. Limit is 5 requests per 10 seconds."})
			return
		}

		timestamps = append(timestamps, now)
		if data, err := json.Marshal(timestamps); err == nil {
			h.Cache.Set(r.Context(), key, string(data), window)
		}

		next(w, r)
	}
}

// SearchSingle processes a single search query, handling cache lookup, operators parsing, fanning out, scoring, and writing to cache.
func (h *HandlerContext) SearchSingle(ctx context.Context, rawQuery string, categories []string, reqLocale, reqLanguage, reqCountry string, reqTimeout float64) (SearchResponse, bool, error) {
	start := time.Now()

	// 1. Validate "q" (query)
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return SearchResponse{}, false, fmt.Errorf("field 'q' is required in request body")
	}

	// Parse Query Overrides
	parsedQuery := engine.ParseQuery(rawQuery, h.Registry)
	query := parsedQuery.CleanQuery
	if query == "" {
		return SearchResponse{}, false, fmt.Errorf("search query cannot be empty after parsing overrides")
	}

	// 2. Parse categories
	if len(categories) == 0 {
		categories = []string{"general"}
	} else {
		var cleanedCats []string
		for _, cat := range categories {
			trimmed := strings.ToLower(strings.TrimSpace(cat))
			if trimmed != "" {
				cleanedCats = append(cleanedCats, trimmed)
			}
		}
		categories = cleanedCats
		if len(categories) == 0 {
			categories = []string{"general"}
		}
	}

	// Category Override check
	searchCategories := categories
	if parsedQuery.CategoryOverride != "" {
		searchCategories = []string{parsedQuery.CategoryOverride}
	}

	// 3. Parse search option filters
	locale := strings.TrimSpace(reqLocale)
	language := strings.TrimSpace(reqLanguage)
	country := strings.TrimSpace(reqCountry)

	// If locale is set, extract language and country from it as defaults
	if locale != "" {
		parts := strings.Split(locale, "-")
		if len(parts) > 0 && language == "" {
			language = strings.ToLower(parts[0])
		}
		if len(parts) > 1 && country == "" {
			country = strings.ToUpper(parts[1])
		}
	}

	// Dynamic fallbacks
	if language == "" {
		language = "en"
	}
	if country == "" {
		country = "US"
	}

	// Language Override check
	if parsedQuery.LanguageOverride != "" {
		language = parsedQuery.LanguageOverride
	}

	if locale == "" {
		locale = language + "-" + country
	} else {
		// Update locale to reflect any language override
		parts := strings.Split(locale, "-")
		if len(parts) > 1 {
			locale = language + "-" + parts[1]
		} else {
			locale = language + "-" + country
		}
	}

	opts := engine.SearchOptions{
		Locale:   locale,
		Language: language,
		Country:  country,
	}

	// Cache lookup check
	var cacheKey string
	if h.Cache != nil && h.Config.General.Cache.Enabled {
		// Use rawQuery to make caches unique for different operators
		cacheKey = cache.GenerateKey(rawQuery, searchCategories, language, country, locale)
		if cachedJSON, found := h.Cache.Get(ctx, cacheKey); found {
			var response SearchResponse
			if err := json.Unmarshal([]byte(cachedJSON), &response); err == nil {
				return response, true, nil
			}
		}
	}

	// 4. Parse custom timeout (optional)
	timeoutSec := h.Config.Outgoing.RequestTimeout
	if timeoutSec <= 0 {
		timeoutSec = 5.0
	}
	maxTimeoutSec := h.Config.Outgoing.MaxRequestTimeout
	if maxTimeoutSec <= 0 {
		maxTimeoutSec = 10.0
	}
	if reqTimeout > 0 {
		// Do not exceed configured absolute max timeout
		if reqTimeout > maxTimeoutSec {
			timeoutSec = maxTimeoutSec
		} else {
			timeoutSec = reqTimeout
		}
	}

	// 5. Resolve active and suspended engines
	var activeEngines []engine.SearchEngine
	var suspendedEngines [][]string

	if len(parsedQuery.EngineOverrides) > 0 {
		for _, name := range parsedQuery.EngineOverrides {
			if eng, found := h.Registry.GetEngine(name); found {
				if eng.Disabled() {
					continue
				}
				if reason, isSuspended := eng.SuspendedReason(); isSuspended {
					suspendedEngines = append(suspendedEngines, []string{eng.Name(), reason})
				} else {
					activeEngines = append(activeEngines, eng)
				}
			}
		}
	} else {
		activeEngines, suspendedEngines = h.Registry.GetActiveAndSuspendedEnginesForCategories(searchCategories)
	}

	if len(activeEngines) == 0 {
		return SearchResponse{
			Query:               rawQuery,
			Results:             []scoring.SearchResult{},
			UnresponsiveEngines: suspendedEngines,
			Metrics:             []engine.EngineMetrics{},
			Count:               0,
		}, false, nil
	}

	// 6. Build context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	// 7. Execute Concurrency Layer Fan-Out
	rawResults, metrics := engine.ExecuteSearchConcurrently(queryCtx, activeEngines, query, opts)

	// 8. Execute Result Scoring & Deduplication with Exclusions
	finalResults := scoring.AggregateAndScore(rawResults, parsedQuery.ExcludedTerms)

	duration := time.Since(start).Milliseconds()

	response := SearchResponse{
		Query:               rawQuery,
		Results:             finalResults,
		UnresponsiveEngines: suspendedEngines,
		Metrics:             metrics,
		Count:               len(finalResults),
		ProcessingTimeMS:    duration,
	}

	if h.Cache != nil && h.Config.General.Cache.Enabled && len(finalResults) > 0 {
		if jsonBytes, err := json.Marshal(response); err == nil {
			h.Cache.Set(ctx, cacheKey, string(jsonBytes), time.Duration(h.Config.General.Cache.TTL)*time.Second)
		}
	}

	return response, false, nil
}

// HandleSearch processes search queries concurrently, deduplicates results, scores them, and responds with JSON.
func (h *HandlerContext) HandleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Only allow POST requests
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed. Use POST."})
		return
	}

	// Define the POST body structure
	type SearchRequest struct {
		Query      string   `json:"q"`
		Categories []string `json:"categories"`
		Locale     string   `json:"locale"`
		Language   string   `json:"language"`
		Country    string   `json:"country"`
		Timeout    float64  `json:"timeout"`
	}

	var reqPayload SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid JSON request body"})
		return
	}

	response, isHit, err := h.SearchSingle(r.Context(), reqPayload.Query, reqPayload.Categories, reqPayload.Locale, reqPayload.Language, reqPayload.Country, reqPayload.Timeout)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	if isHit {
		w.Header().Set("X-Cache", "HIT")
	} else {
		w.Header().Set("X-Cache", "MISS")
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// BulkSearchRequest defines the request schema for POST /search/bulk
type BulkSearchRequest struct {
	Queries    []string `json:"queries"`
	Categories []string `json:"categories"`
	Locale     string   `json:"locale"`
	Language   string   `json:"language"`
	Country    string   `json:"country"`
	Timeout    float64  `json:"timeout"`
}

// SingleQueryResponse represents the search result sub-card per query in bulk response
type SingleQueryResponse struct {
	Results             []scoring.SearchResult `json:"results"`
	UnresponsiveEngines [][]string             `json:"unresponsive_engines"`
	Count               int                    `json:"count"`
	Error               string                 `json:"error,omitempty"`
}

// BulkSearchResponse defines the response schema for POST /search/bulk
type BulkSearchResponse struct {
	Results          map[string]SingleQueryResponse `json:"results"`
	ProcessingTimeMS int64                          `json:"processing_time_ms"`
}

// HandleBulkSearch processes multiple search queries in parallel concurrently.
func (h *HandlerContext) HandleBulkSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed. Use POST."})
		return
	}

	var reqPayload BulkSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid JSON request body"})
		return
	}

	if len(reqPayload.Queries) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "field 'queries' is required and must contain at least one query"})
		return
	}

	start := time.Now()

	var mu sync.Mutex
	resultsMap := make(map[string]SingleQueryResponse)

	var wg sync.WaitGroup

	for _, qStr := range reqPayload.Queries {
		qStr := qStr
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			resp, _, err := h.SearchSingle(r.Context(), qStr, reqPayload.Categories, reqPayload.Locale, reqPayload.Language, reqPayload.Country, reqPayload.Timeout)
			
			mu.Lock()
			defer mu.Unlock()
			
			if err != nil {
				resultsMap[qStr] = SingleQueryResponse{
					Results: []scoring.SearchResult{},
					Error:   err.Error(),
				}
			} else {
				resultsMap[qStr] = SingleQueryResponse{
					Results:             resp.Results,
					UnresponsiveEngines: resp.UnresponsiveEngines,
					Count:               resp.Count,
				}
			}
		}()
	}

	wg.Wait()

	duration := time.Since(start).Milliseconds()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(BulkSearchResponse{
		Results:          resultsMap,
		ProcessingTimeMS: duration,
	})
}

// HandleHealth returns the system health status.
func (h *HandlerContext) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"service":   "GoSearX",
	})
}

// CrawlResponse defines the clean parsed article content returned on success.
type CrawlResponse struct {
	URL          string `json:"url"`
	Title        string `json:"title"`
	Excerpt      string `json:"excerpt"`
	Byline       string `json:"byline"`
	SiteName     string `json:"site_name"`
	Length       int    `json:"length"`
	LeadImageURL string `json:"lead_image_url"`
	Content      string `json:"content"`
	Markdown     string `json:"markdown"`
	DurationMS   int64  `json:"duration_ms"`
}

// HandleCrawl fetches one or more target URLs concurrently and extracts their readability content in parallel.
func (h *HandlerContext) HandleCrawl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Only allow POST requests
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Method not allowed. Use POST."})
		return
	}

	start := time.Now()

	// 2. Parse all target URLs from POST body
	type CrawlRequest struct {
		URL             string   `json:"url"`
		URLs            []string `json:"urls"`
		JSRender        bool     `json:"js_render"`
		WaitMs          int      `json:"wait_ms"`
		WaitSelector    string   `json:"wait_selector"`
		ExtractSelector string   `json:"extract_selector"`
		Markdown        bool     `json:"markdown"`
	}

	var reqPayload CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid JSON request body"})
		return
	}

	// Clean and filter empty URLs
	var targetURLs []string
	if strings.TrimSpace(reqPayload.URL) != "" {
		targetURLs = append(targetURLs, strings.TrimSpace(reqPayload.URL))
	}
	for _, u := range reqPayload.URLs {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" {
			targetURLs = append(targetURLs, trimmed)
		}
	}

	if len(targetURLs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "field 'url' or 'urls' is required in request body"})
		return
	}

	// 3. Create context with bounded timeout for crawler fetch
	timeoutSec := h.Config.Outgoing.MaxRequestTimeout
	if timeoutSec <= 0 {
		timeoutSec = 5.0
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	// 4. Struct to hold a single crawl result
	type SingleCrawlResult struct {
		URL          string `json:"url"`
		Title        string `json:"title,omitempty"`
		Excerpt      string `json:"excerpt,omitempty"`
		Byline       string `json:"byline,omitempty"`
		SiteName     string `json:"site_name,omitempty"`
		Length       int    `json:"length,omitempty"`
		LeadImageURL string `json:"lead_image_url,omitempty"`
		Content      string `json:"content,omitempty"`
		Markdown     string `json:"markdown,omitempty"`
		Error        string `json:"error,omitempty"`
		DurationMS   int64  `json:"duration_ms"`
	}

	// Channel to aggregate results asynchronously
	resultChan := make(chan SingleCrawlResult, len(targetURLs))

	// 5. Execute parallel fan-out queries using Goroutines
	for _, targetURLStr := range targetURLs {
		go func(urlStr string) {
			crawlStart := time.Now()
			res := SingleCrawlResult{URL: urlStr}

			// A. If cache is enabled, check cache first
			var cacheKey string
			if h.Cache != nil && h.Config.General.Cache.Enabled {
				cacheKey = cache.GenerateCrawlKey(urlStr)
				if cachedJSON, found := h.Cache.Get(ctx, cacheKey); found {
					var cachedResult SingleCrawlResult
					if err := json.Unmarshal([]byte(cachedJSON), &cachedResult); err == nil {
						cachedResult.DurationMS = time.Since(crawlStart).Milliseconds()
						resultChan <- cachedResult
						return
					}
				}
			}

			// B. Cache miss - perform live crawl
			parsedURL, err := url.Parse(urlStr)
			if err != nil || !parsedURL.IsAbs() {
				res.Error = "invalid absolute URL structure"
				res.DurationMS = time.Since(crawlStart).Milliseconds()
				resultChan <- res
				return
			}

			var proxyStr string
			if h.Config.Outgoing.Proxies != nil && len(h.Config.Outgoing.Proxies) > 0 {
				proxyStr = h.Config.Outgoing.Proxies[rand.Intn(len(h.Config.Outgoing.Proxies))]
			}

			var html string
			var crawlErr error

			if reqPayload.JSRender {
				// Dynamic Headless Crawl (Crawl4AI-style JS rendering)
				html, crawlErr = h.CrawlDynamicPage(ctx, parsedURL.String(), reqPayload.WaitMs, reqPayload.WaitSelector, proxyStr)
				if crawlErr != nil {
					res.Error = fmt.Sprintf("dynamic crawl failed: %v", crawlErr)
					res.DurationMS = time.Since(crawlStart).Milliseconds()
					resultChan <- res
					return
				}
			} else {
				// Static HTTP Crawl fallback
				req, err := http.NewRequestWithContext(ctx, "GET", parsedURL.String(), nil)
				if err != nil {
					res.Error = fmt.Sprintf("failed to construct crawl request: %v", err)
					res.DurationMS = time.Since(crawlStart).Milliseconds()
					resultChan <- res
					return
				}

				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
				req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
				req.Header.Set("Accept-Language", "en-US,en;q=0.9")
				req.Header.Set("Referer", "https://www.google.com/")

				resp, err := h.Client.Do(req)
				if err != nil {
					res.Error = fmt.Sprintf("failed to fetch target webpage: %v", err)
					res.DurationMS = time.Since(crawlStart).Milliseconds()
					resultChan <- res
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					res.Error = fmt.Sprintf("target server returned non-ok status: %d", resp.StatusCode)
					res.DurationMS = time.Since(crawlStart).Milliseconds()
					resultChan <- res
					return
				}

				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					res.Error = fmt.Sprintf("failed to read target response body: %v", err)
					res.DurationMS = time.Since(crawlStart).Milliseconds()
					resultChan <- res
					return
				}
				html = string(bodyBytes)
			}

			// Parse DOM using goquery
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
			if err != nil {
				res.Error = fmt.Sprintf("failed to parse HTML DOM: %v", err)
				res.DurationMS = time.Since(crawlStart).Milliseconds()
				resultChan <- res
				return
			}

			var contentHTML string
			var contentText string

			// If a custom element selector is provided, extract it
			if reqPayload.ExtractSelector != "" {
				selection := doc.Find(reqPayload.ExtractSelector)
				if selection.Length() > 0 {
					contentHTML, _ = selection.Html()
					contentText = strings.TrimSpace(selection.Text())
					
					res.Title = doc.Find("title").Text()
					if res.Title == "" {
						res.Title = parsedURL.String()
					}
					res.Length = len(contentText)
					res.Content = contentText
				}
			}

			// Fallback to go-readability (smart element extraction) if selector was not specified or not found
			if contentHTML == "" {
				article, err := readability.FromReader(strings.NewReader(html), parsedURL)
				if err != nil {
					// Fallback to body content if readability also fails
					contentHTML, _ = doc.Find("body").Html()
					contentText = strings.TrimSpace(doc.Find("body").Text())
					res.Title = doc.Find("title").Text()
					if res.Title == "" {
						res.Title = parsedURL.String()
					}
					res.Length = len(contentText)
					res.Content = contentText
				} else {
					res.Title = article.Title
					res.Excerpt = article.Excerpt
					res.Byline = article.Byline
					res.SiteName = article.SiteName
					res.LeadImageURL = article.Image
					res.Length = len(article.TextContent)
					res.Content = article.TextContent
					contentHTML = article.Content
					contentText = article.TextContent
				}
			}

			// Automated Markdown conversion (Crawl4AI-style)
			if reqPayload.Markdown && contentHTML != "" {
				converter := htmltomarkdown.NewConverter(parsedURL.Scheme+"://"+parsedURL.Host, true, nil)
				markdownStr, err := converter.ConvertString(contentHTML)
				if err == nil {
					res.Markdown = CleanMarkdown(markdownStr)
				} else {
					res.Markdown = contentText
				}
			}

			res.DurationMS = time.Since(crawlStart).Milliseconds()

			// C. Cache successful crawl results
			if h.Cache != nil && h.Config.General.Cache.Enabled && res.Error == "" {
				if jsonBytes, err := json.Marshal(res); err == nil {
					ttl := time.Duration(h.Config.General.Cache.TTL) * time.Second
					if ttl <= 0 {
						ttl = 300 * time.Second
					}
					h.Cache.Set(ctx, cacheKey, string(jsonBytes), ttl)
				}
			}

			resultChan <- res
		}(targetURLStr)
	}

	// 6. Aggregate results concurrently
	var results []SingleCrawlResult
	completedCount := 0

loop:
	for completedCount < len(targetURLs) {
		select {
		case res := <-resultChan:
			completedCount++
			results = append(results, res)
		case <-ctx.Done():
			// Bounded overall request timeout triggered, aggregate unfinished as timed out
			break loop
		}
	}

	// Populate any timed-out pages
	if completedCount < len(targetURLs) {
		completedMap := make(map[string]bool)
		for _, r := range results {
			completedMap[r.URL] = true
		}

		for _, urlStr := range targetURLs {
			if !completedMap[urlStr] {
				results = append(results, SingleCrawlResult{
					URL:        urlStr,
					Error:      ctx.Err().Error(),
					DurationMS: time.Since(start).Milliseconds(),
				})
			}
		}
	}

	duration := time.Since(start).Milliseconds()

	// 7. Output clean response
	// If only 1 URL was requested, return a single object directly for backward compatibility
	if len(targetURLs) == 1 {
		res := results[0]
		if res.Error != "" {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(ErrorResponse{Error: res.Error})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(CrawlResponse{
			URL:          res.URL,
			Title:        res.Title,
			Excerpt:      res.Excerpt,
			Byline:       res.Byline,
			SiteName:     res.SiteName,
			Length:       res.Length,
			LeadImageURL: res.LeadImageURL,
			Content:      res.Content,
			Markdown:     res.Markdown,
			DurationMS:   duration,
		})
		return
	}

	// If multiple URLs were requested, return the array wrapper
	type MultiCrawlResponse struct {
		Results          []SingleCrawlResult `json:"results"`
		Count            int                 `json:"count"`
		ProcessingTimeMS int64               `json:"processing_time_ms"`
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(MultiCrawlResponse{
		Results:          results,
		Count:            len(results),
		ProcessingTimeMS: duration,
	})
}

// CrawlDynamicPage drives a headless Chrome browser session using chromedp to render JavaScript.
func (h *HandlerContext) CrawlDynamicPage(ctx context.Context, targetURL string, waitMs int, waitSelector string, proxyStr string) (string, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)
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

	// Navigation
	tasks = append(tasks, chromedp.Navigate(targetURL))

	// CSS Selector waiting
	if waitSelector != "" {
		tasks = append(tasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	}

	// Hydration waiting (React/Vue/Angular)
	if waitMs > 0 {
		tasks = append(tasks, chromedp.Sleep(time.Duration(waitMs)*time.Millisecond))
	}

	// Fetch outer DOM
	tasks = append(tasks, chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery))

	err := chromedp.Run(chromeCtx, tasks)
	if err != nil {
		return "", err
	}

	return htmlContent, nil
}

// CleanMarkdown standardizes Markdown whitespace and strips empty list markers for LLM consumption (Crawl4AI patterns).
func CleanMarkdown(md string) string {
	// Collapse 3 or more newlines into 2
	reNewlines := regexp.MustCompile(`\n{3,}`)
	md = reNewlines.ReplaceAllString(md, "\n\n")

	// Strip empty bullets and list lines
	reEmptyBullets := regexp.MustCompile(`(?m)^\s*[\*\-\+]\s*$`)
	md = reEmptyBullets.ReplaceAllString(md, "")

	return strings.TrimSpace(md)
}
