package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/120.0",
}

func main() {
	query := "quantum physics"
	escaped := url.QueryEscape(query)

	engines := []struct {
		Name      string
		URL       string
		Selectors struct {
			Result  []string
			Title   []string
			URL     []string
			Content []string
		}
	}{
		{
			Name: "DuckDuckGo (HTML)",
			URL:  fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", escaped),
			Selectors: struct {
				Result  []string
				Title   []string
				URL     []string
				Content []string
			}{
				Result:  []string{"div.results_links_deep", "div.result"},
				Title:   []string{"a.result__a", "h2.result__title"},
				URL:     []string{"a.result__a"},
				Content: []string{"a.result__snippet", "div.result__snippet"},
			},
		},
		{
			Name: "Brave Search",
			URL:  fmt.Sprintf("https://search.brave.com/search?q=%s", escaped),
			Selectors: struct {
				Result  []string
				Title   []string
				URL     []string
				Content []string
			}{
				Result:  []string{"div.snippet", "div.web-results"},
				Title:   []string{"a.title-link", "span.snippet-title", "h2"},
				URL:     []string{"a.title-link"},
				Content: []string{"p.snippet-description", "div.snippet-content"},
			},
		},
		{
			Name: "Mojeek",
			URL:  fmt.Sprintf("https://www.mojeek.com/search?q=%s", escaped),
			Selectors: struct {
				Result  []string
				Title   []string
				URL     []string
				Content []string
			}{
				Result:  []string{"li.result", "div.result"},
				Title:   []string{"a.title", "h2 a"},
				URL:     []string{"a.title"},
				Content: []string{"p.s", "span.desc"},
			},
		},
	}

	for _, eng := range engines {
		fmt.Printf("\n=== Testing %s ===\n", eng.Name)
		req, err := http.NewRequestWithContext(context.Background(), "GET", eng.URL, nil)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Request error: %v\n", err)
			continue
		}

		fmt.Printf("Status: %d\n", resp.StatusCode)
		if resp.StatusCode != 200 {
			resp.Body.Close()
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
			continue
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(bodyBytes)))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}

		// Find results
		var resultNodes *goquery.Selection
		for _, sel := range eng.Selectors.Result {
			nodes := doc.Find(sel)
			if nodes.Length() > 0 {
				resultNodes = nodes
				fmt.Printf("Found %d results using selector '%s'\n", nodes.Length(), sel)
				break
			}
		}

		if resultNodes == nil || resultNodes.Length() == 0 {
			fmt.Println("No results found using standard selectors.")
			continue
		}

		// Print first 2 results
		resultNodes.Each(func(i int, item *goquery.Selection) {
			if i < 2 {
				var title string
				for _, sel := range eng.Selectors.Title {
					tNode := item.Find(sel)
					if tNode.Length() > 0 {
						title = strings.TrimSpace(tNode.Text())
						break
					}
				}
				var href string
				for _, sel := range eng.Selectors.URL {
					uNode := item.Find(sel)
					if uNode.Length() > 0 {
						href, _ = uNode.Attr("href")
						break
					}
				}
				if href == "" {
					href, _ = item.Find("a").Attr("href")
				}
				fmt.Printf("  [%d] Title: '%s'\n      URL: '%s'\n", i+1, title, href)
			}
		})
	}
}
