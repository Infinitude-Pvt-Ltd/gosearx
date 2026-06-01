package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"gosearx/cache"
	"gosearx/config"
	"gosearx/engine"
	"gosearx/scoring"
	"gosearx/server"
)

func main() {
	fmt.Println("=== Testing Bulk Search Concurrent Execution ===")

	// 1. Create a dummy config
	cfg := &config.Config{}
	cfg.General.Cache.Enabled = true

	// 2. Create Registry & Mock Search Engines
	reg := engine.NewRegistry()
	mockWiki := &mockSearchEngine{
		name:       "wikipedia",
		categories: []string{"general"},
		results: []scoring.EngineResult{
			{
				Title:    "Space Exploration on Wikipedia",
				URL:      "https://en.wikipedia.org/wiki/Space",
				Content:  "Space exploration is the ongoing discovery and exploration of celestial structures.",
				Engine:   "wikipedia",
				Category: "general",
				Rank:     1,
				Weight:   1.0,
			},
		},
	}
	mockGrok := &mockSearchEngine{
		name:       "grokipedia",
		categories: []string{"general"},
		results: []scoring.EngineResult{
			{
				Title:    "Grokipedia Knowledge Graph",
				URL:      "https://grokipedia.org",
				Content:  "Grokipedia is a comprehensive AI-centric knowledge base.",
				Engine:   "grokipedia",
				Category: "general",
				Rank:     1,
				Weight:   1.0,
			},
		},
	}
	mockScraper := &mockSearchEngine{
		name:       "google",
		categories: []string{"general"},
		results: []scoring.EngineResult{
			{
				Title:    "GoSearX - High-Performance Metasearch engine in Go",
				URL:      "https://gosearx.io",
				Content:  "An organic metasearch engine fanning out queries concurrently.",
				Engine:   "google",
				Category: "general",
				Rank:     1,
				Weight:   1.0,
			},
		},
	}
	reg.Register(mockWiki)
	reg.Register(mockGrok)
	reg.Register(mockScraper)

	// 3. Create cache
	inMemoryCache := cache.NewInMemoryCache(30 * time.Second)
	defer inMemoryCache.Close()

	// 4. Create HandlerContext
	handlerCtx := server.NewHandlerContext(cfg, reg, inMemoryCache, http.DefaultClient)

	// 5. Spin up httptest server wrapping HandleBulkSearch
	ts := httptest.NewServer(http.HandlerFunc(handlerCtx.HandleBulkSearch))
	defer ts.Close()

	// 6. Formulate Bulk Request
	reqPayload := server.BulkSearchRequest{
		Queries:    []string{"!w space", "METASEARCH ENGINE", "!gk grokipedia"},
		Categories: []string{"general"},
	}
	jsonBytes, _ := json.Marshal(reqPayload)

	resp, err := ts.Client().Post(ts.URL, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		fmt.Printf("Error sending bulk request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %d\n", resp.StatusCode)

	var bulkResp server.BulkSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&bulkResp); err != nil {
		fmt.Printf("Error decoding bulk response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total queries processed: %d\n", len(bulkResp.Results))
	fmt.Printf("Processing Time: %d ms\n\n", bulkResp.ProcessingTimeMS)

	failed := false

	// Assertions for Wikipedia query
	wikiRes, found := bulkResp.Results["!w space"]
	if !found {
		fmt.Println("  [FAIL] Missing results for query '!w space'")
		failed = true
	} else {
		fmt.Printf("Query '!w space': found %d results, Error: %s\n", wikiRes.Count, wikiRes.Error)
		if wikiRes.Count > 0 {
			fmt.Printf("  First card Title: %q\n", wikiRes.Results[0].Title)
			if !strings.Contains(wikiRes.Results[0].Title, "Wikipedia") {
				fmt.Println("  [FAIL] Expected Wikipedia in title")
				failed = true
			}
		} else {
			fmt.Println("  [FAIL] Expected at least one result card")
			failed = true
		}
	}

	// Assertions for General Scraper query
	genRes, found := bulkResp.Results["METASEARCH ENGINE"]
	if !found {
		fmt.Println("  [FAIL] Missing results for query 'METASEARCH ENGINE'")
		failed = true
	} else {
		fmt.Printf("Query 'METASEARCH ENGINE': found %d results, Error: %s\n", genRes.Count, genRes.Error)
		if genRes.Count > 0 {
			fmt.Printf("  First card Title: %q\n", genRes.Results[0].Title)
			if !strings.Contains(genRes.Results[0].Title, "GoSearX") {
				fmt.Println("  [FAIL] Expected GoSearX in title")
				failed = true
			}
		} else {
			fmt.Println("  [FAIL] Expected at least one result card")
			failed = true
		}
	}

	// Assertions for Grokipedia query
	grokRes, found := bulkResp.Results["!gk grokipedia"]
	if !found {
		fmt.Println("  [FAIL] Missing results for query '!gk grokipedia'")
		failed = true
	} else {
		fmt.Printf("Query '!gk grokipedia': found %d results, Error: %s\n", grokRes.Count, grokRes.Error)
		if grokRes.Count > 0 {
			fmt.Printf("  First card Title: %q\n", grokRes.Results[0].Title)
			if !strings.Contains(grokRes.Results[0].Title, "Grokipedia") {
				fmt.Println("  [FAIL] Expected Grokipedia in title")
				failed = true
			}
		} else {
			fmt.Println("  [FAIL] Expected at least one result card")
			failed = true
		}
	}

	if failed {
		fmt.Println("\nBulk search tests failed!")
		os.Exit(1)
	} else {
		fmt.Println("\nAll concurrent bulk search verification tests passed successfully!")
	}
}

// Mock SearchEngine
type mockSearchEngine struct {
	name       string
	categories []string
	results    []scoring.EngineResult
}

func (m *mockSearchEngine) Search(ctx context.Context, query string, opts engine.SearchOptions) ([]scoring.EngineResult, error) {
	fmt.Printf("[DEBUG] mockSearchEngine %s.Search called with query=%q\n", m.name, query)
	var matched []scoring.EngineResult
	for _, res := range m.results {
		if strings.Contains(strings.ToLower(res.Title), strings.ToLower(query)) || strings.Contains(strings.ToLower(res.Content), strings.ToLower(query)) {
			matched = append(matched, res)
		} else if strings.ToLower(query) == "metasearch engine" { // dynamic match
			matched = append(matched, res)
		}
	}
	fmt.Printf("[DEBUG] mockSearchEngine %s.Search returning %d matches\n", m.name, len(matched))
	return matched, nil
}
func (m *mockSearchEngine) Name() string                                     { return m.name }
func (m *mockSearchEngine) Weight() float64                                 { return 1.0 }
func (m *mockSearchEngine) Categories() []string                             { return m.categories }
func (m *mockSearchEngine) Disabled() bool                                  { return false }
func (m *mockSearchEngine) Suspend(duration time.Duration, reason string)    {}
func (m *mockSearchEngine) SuspendedReason() (string, bool)                 { return "", false }
func (m *mockSearchEngine) Timeout() time.Duration                           { return 5 * time.Second }
