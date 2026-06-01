package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
		UsingJSRender: false, // Disable JSRender to test lightweight mobile scraper!
		Selectors: config.SelectorConfig{
			Result:  []string{"div.g", "div.MjjYud", "div.v7W49e", "div.Gx5Zad"},
			Title:   []string{"h3", "h3.LC20lb", "span.title", "div.BNeawe.deC31e.AP7Wnd"},
			URL:     []string{"a", "a[href]"},
			Content: []string{"div.VwiC3b", "div.yD3NFf", "span.st", "div.s3V9rc", "div.BNeawe.s3V9rc", "div.BNeawe"},
		},
	}

	// 2. Setup client with proxy
	proxyURL, _ := url.Parse("http://6782f138051b97e4448c:f45c7acbf461fb46@gw.dataimpulse.com:823")
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	// 3. Construct GoogleEngine
	proxies := []string{"http://6782f138051b97e4448c:f45c7acbf461fb46@gw.dataimpulse.com:823"}
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
