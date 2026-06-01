package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gosearx/config"
	"gosearx/engine"
)

func main() {
	fmt.Println("=== TESTING DYNAMIC HEADLESS CHROME GOOGLE SEARCH ENGINE ===")

	// 1. Setup minimal engine config for google
	engCfg := config.EngineConfig{
		Name:          "google",
		Type:          "html",
		Weight:        1.0,
		Categories:    []string{"general"},
		SearchURL:     "https://www.google.com/search?q={query}&num=20&hl={language}&gl={country}",
		Timeout:       10.0,
		Disabled:      false,
		UsingJSRender: true, // Enable headless browser search!
		Selectors: config.SelectorConfig{
			Result:  []string{"div.g", "div.MjjYud", "div.v7W49e"},
			Title:   []string{"h3", "h3.LC20lb", "span.title"},
			URL:     []string{"a", "a[href]"},
			Content: []string{"div.VwiC3b", "div.yD3NFf", "span.st"},
		},
	}

	// 2. Setup standard client (we won't configure proxies here to check basic local flow)
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// 3. Construct GoogleEngine
	// We pass a nil/empty proxies list to test local chromedp rendering, or we can add the dataimpulse proxy
	proxies := []string{}
	googleDriver := engine.NewGoogleEngine(engCfg, client, proxies)

	fmt.Println("Sending query 'quantum computing' to Google via headless Chrome...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	opts := engine.SearchOptions{
		Language: "en",
		Country:  "US",
		Locale:   "en-US",
	}

	results, err := googleDriver.Search(ctx, "quantum computing", opts)
	if err != nil {
		fmt.Printf("Search execution failed: %v\n", err)
		return
	}

	fmt.Printf("\nSUCCESS! Search successfully parsed %d results from Google!\n", len(results))
	for idx, res := range results {
		if idx < 5 {
			fmt.Printf("Result %d: Title='%s', URL='%s'\n", idx+1, res.Title, res.URL)
		}
	}
}
