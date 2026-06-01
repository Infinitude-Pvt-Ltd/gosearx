package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

func main() {
	query := "quantum computing"
	formattedURL := fmt.Sprintf("https://www.google.com/search?q=%s&num=20&hl=en&gl=us", url.QueryEscape(query))
	proxyStr := "http://6782f138051b97e4448c:f45c7acbf461fb46@gw.dataimpulse.com:823"

	fmt.Println("Configuring proxy:", proxyStr)
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		fmt.Printf("Invalid proxy URL: %v\n", err)
		return
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", formattedURL, nil)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	rand.Seed(time.Now().UnixNano())
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
	
	// Inject GDPR consent cookies to bypass Google's consent wall redirect
	req.Header.Set("Cookie", "CONSENT=YES+; SOCS=CoYBOA")

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	fmt.Println("Sending request to Google through proxy...")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status: %d\n", resp.StatusCode)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading body: %v\n", err)
		return
	}

	bodyStr := string(bodyBytes)
	fmt.Printf("Body length: %d bytes\n", len(bodyStr))

	_ = os.WriteFile("google_proxy_response.html", bodyBytes, 0644)
	fmt.Println("Saved response to google_proxy_response.html")

	if strings.Contains(bodyStr, "captcha") || strings.Contains(bodyStr, "sorry.google.com") || strings.Contains(bodyStr, "unusual traffic") {
		fmt.Println("[WARNING] Captcha/Sorry redirect detected!")
		return
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		fmt.Printf("Error parsing HTML: %v\n", err)
		return
	}

	fmt.Printf("Page Title: '%s'\n", doc.Find("title").Text())


	// Try the selectors from settings.yml
	selectors := []string{"div.g", "div.MjjYud", "div.v7W49e"}
	var resultsFound int
	for _, sel := range selectors {
		nodes := doc.Find(sel)
		if nodes.Length() > 0 {
			resultsFound = nodes.Length()
			fmt.Printf("Selector '%s' found %d nodes!\n", sel, nodes.Length())
			nodes.Each(func(i int, item *goquery.Selection) {
				if i < 3 {
					title := item.Find("h3").Text()
					link, _ := item.Find("a").Attr("href")
					fmt.Printf("  Result %d: Title='%s', URL='%s'\n", i+1, strings.TrimSpace(title), link)
				}
			})
		}
	}

	if resultsFound == 0 {
		fmt.Println("[INFO] Class-based selectors failed. Trying fallback semantic parser...")
		var fallbackCount int
		doc.Find("a").Each(func(i int, link *goquery.Selection) {
			if link.Find("h2, h3, h4").Length() > 0 {
				parent := link.Parent()
				if parent.Length() > 0 {
					pTagName := strings.ToLower(goquery.NodeName(parent))
					if pTagName != "header" && pTagName != "footer" && pTagName != "nav" {
						fallbackCount++
						if fallbackCount <= 3 {
							titleText := strings.TrimSpace(link.Find("h2, h3, h4").First().Text())
							rawURL, _ := link.Attr("href")
							fmt.Printf("  Fallback %d: Title='%s', URL='%s'\n", fallbackCount, titleText, rawURL)
						}
					}
				}
			}
		})
		fmt.Printf("Fallback semantic parsing found %d candidate nodes\n", fallbackCount)
	}
}
