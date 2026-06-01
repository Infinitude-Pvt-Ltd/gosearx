package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type TestCase struct {
	Name    string
	URL     string
	Headers map[string]string
}

func main() {
	query := "quantum physics"
	escapedQuery := url.QueryEscape(query)

	tests := []TestCase{
		{
			Name: "Minimal Headers (User-Agent only), No Cookies, Simple URL",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s", escapedQuery),
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			},
		},
		{
			Name: "Minimal Headers with Cookie, Simple URL",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s", escapedQuery),
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				"Cookie":     "CONSENT=YES+; SOCS=CoYBOA",
			},
		},
		{
			Name: "Full Emulation, Simple URL",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s", escapedQuery),
			Headers: map[string]string{
				"User-Agent":                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
				"Accept-Language":           "en-US,en;q=0.9",
				"Referer":                   "https://www.google.com/",
				"Upgrade-Insecure-Requests": "1",
				"Sec-Ch-Ua":                 `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
				"Sec-Ch-Ua-Mobile":          "?0",
				"Sec-Ch-Ua-Platform":        `"Macintosh"`,
				"Sec-Fetch-Dest":            "document",
				"Sec-Fetch-Mode":            "navigate",
				"Sec-Fetch-Site":            "same-origin",
				"Cookie":                    "CONSENT=YES+; SOCS=CoYBOA",
			},
		},
		{
			Name: "Full Emulation, Search with GL and HL and Num=20",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s&num=20&hl=en&gl=us", escapedQuery),
			Headers: map[string]string{
				"User-Agent":                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
				"Accept-Language":           "en-US,en;q=0.9",
				"Referer":                   "https://www.google.com/",
				"Upgrade-Insecure-Requests": "1",
				"Sec-Ch-Ua":                 `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
				"Sec-Ch-Ua-Mobile":          "?0",
				"Sec-Ch-Ua-Platform":        `"Macintosh"`,
				"Sec-Fetch-Dest":            "document",
				"Sec-Fetch-Mode":            "navigate",
				"Sec-Fetch-Site":            "same-origin",
				"Cookie":                    "CONSENT=YES+; SOCS=CoYBOA",
			},
		},
		{
			Name: "Old IE6 User Agent (no-JS HTML fallback)",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s", escapedQuery),
			Headers: map[string]string{
				"User-Agent": "Mozilla/4.0 (compatible; MSIE 6.0; Windows NT 5.1)",
			},
		},
		{
			Name: "Googlebot User Agent (sometimes bypasses checks or gets different markup)",
			URL:  fmt.Sprintf("https://www.google.com/search?q=%s", escapedQuery),
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			},
		},
	}

	for i, tc := range tests {
		fmt.Printf("\n=== TEST %d: %s ===\n", i+1, tc.Name)
		fmt.Println("URL:", tc.URL)

		req, err := http.NewRequestWithContext(context.Background(), "GET", tc.URL, nil)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			continue
		}

		for k, v := range tc.Headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Request error: %v\n", err)
			continue
		}

		fmt.Printf("Status: %d\n", resp.StatusCode)
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Printf("Read error: %v\n", err)
			continue
		}

		bodyStr := string(bodyBytes)
		fmt.Printf("Body size: %d bytes\n", len(bodyStr))

		// Check for challenge
		if strings.Contains(bodyStr, "captcha") || strings.Contains(bodyStr, "sorry.google.com") || strings.Contains(bodyStr, "unusual traffic") {
			fmt.Println("Result: BLOCKED (Captcha/Sorry)")
			continue
		}
		if strings.Contains(bodyStr, "enablejs") || strings.Contains(bodyStr, "window.google") && len(bodyStr) < 150000 && !strings.Contains(bodyStr, "search") {
			fmt.Println("Result: BLOCKED (JS Challenge Wall)")
			continue
		}

		// Parse HTML
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}

		// Print Title
		title := doc.Find("title").Text()
		fmt.Printf("Page Title: '%s'\n", title)

		// Search for search result divs
		selectors := []string{"div.g", "div.MjjYud", "div.v7W49e", "div.kCrYT", "div.ZINdqf"}
		foundResults := false
		for _, sel := range selectors {
			nodes := doc.Find(sel)
			if nodes.Length() > 0 {
				fmt.Printf("Selector '%s' found %d nodes!\n", sel, nodes.Length())
				foundResults = true
				// Print first result
				nodes.Each(func(idx int, s *goquery.Selection) {
					if idx == 0 {
						h3 := s.Find("h3").Text()
						a, _ := s.Find("a").Attr("href")
						fmt.Printf("  First Node: Title='%s', URL='%s'\n", strings.TrimSpace(h3), a)
					}
				})
			}
		}

		if !foundResults {
			// Try fallback semantic parsing
			var fallbackCount int
			doc.Find("a").Each(func(idx int, link *goquery.Selection) {
				if link.Find("h2, h3, h4").Length() > 0 {
					fallbackCount++
					if fallbackCount == 1 {
						titleText := strings.TrimSpace(link.Find("h2, h3, h4").First().Text())
						rawURL, _ := link.Attr("href")
						fmt.Printf("  Semantic Fallback 1: Title='%s', URL='%s'\n", titleText, rawURL)
					}
				}
			})
			fmt.Printf("Semantic Fallback found %d nodes\n", fallbackCount)
			if fallbackCount > 0 {
				foundResults = true
			}
		}

		if foundResults {
			// Save HTML for manual inspection of a working test
			filename := fmt.Sprintf("working_test_%d.html", i+1)
			_ = os.WriteFile(filename, bodyBytes, 0644)
			fmt.Printf("SUCCESS! Saved working html to %s\n", filename)
		} else {
			fmt.Println("Result: No search results found in HTML markup.")
		}
	}
}
