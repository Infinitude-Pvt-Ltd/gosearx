package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"gosearx/cache"
	"gosearx/config"
	"gosearx/server"
)

func main() {
	fmt.Println("=== Testing Bot Limiter Shield Rate Limiting Middleware ===")

	// 1. Create a dummy config
	cfg := &config.Config{}
	cfg.General.Cache.Enabled = true

	// 2. Create in-memory cache
	inMemoryCache := cache.NewInMemoryCache(30 * time.Second)
	defer inMemoryCache.Close()

	// 3. Initialize HandlerContext
	handlerCtx := server.NewHandlerContext(cfg, nil, inMemoryCache, http.DefaultClient)

	// 4. Create a dummy handler wrapped with LimitRate
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	rateLimitedHandler := handlerCtx.LimitRate(dummyHandler)

	// 5. Spin up a test server
	ts := httptest.NewServer(rateLimitedHandler)
	defer ts.Close()

	client := ts.Client()

	failed := false

	// Make 6 sequential requests immediately
	for i := 1; i <= 6; i++ {
		req, err := http.NewRequest("GET", ts.URL, nil)
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			os.Exit(1)
		}
		// Set a dummy X-Forwarded-For header to simulate client IP
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error sending request: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		fmt.Printf("Request %d: Status = %d\n", i, resp.StatusCode)

		if i <= 5 {
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("  [FAIL] Expected 200 OK for request %d, got %d\n", i, resp.StatusCode)
				failed = true
			}
		} else {
			if resp.StatusCode != http.StatusTooManyRequests {
				fmt.Printf("  [FAIL] Expected 429 Too Many Requests for request 6, got %d\n", resp.StatusCode)
				failed = true
			}
		}
	}

	if failed {
		fmt.Println("Rate limiter tests failed!")
		os.Exit(1)
	} else {
		fmt.Println("All rate limiter shield tests passed successfully!")
	}
}
