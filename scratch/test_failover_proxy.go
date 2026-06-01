package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"gosearx/proxy"
)

func main() {
	fmt.Println("=== TESTING PROXY FAILOVER TRANSPORT ===")

	// 1. Set up a mock upstream target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("SUCCESS TARGET REACHED"))
	}))
	defer targetServer.Close()

	// 2. Set up a bad proxy server (Gateway Timeout simulator)
	badProxyCallCount := 0
	badProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badProxyCallCount++
		w.WriteHeader(http.StatusGatewayTimeout) // Force Gateway Timeout error
	}))
	defer badProxy.Close()

	// 3. Set up a good proxy server (forward request to target server)
	goodProxyCallCount := 0
	goodProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodProxyCallCount++
		// Fetch from the target server
		resp, err := http.Get(targetServer.URL)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write([]byte("SUCCESS FROM PROXY"))
	}))
	defer goodProxy.Close()

	// 4. Construct proxy pool with bad proxy first, then good proxy
	proxyPool := []string{
		badProxy.URL,
		goodProxy.URL,
	}

	fmt.Printf("Configured Proxy Pool: %v\n", proxyPool)

	rotator, err := proxy.NewRotator(proxyPool)
	if err != nil {
		fmt.Printf("Rotator creation failed: %v\n", err)
		return
	}

	client := rotator.CreateClient(10, 10, 5*time.Second)

	fmt.Println("Executing query to target server through proxy pool...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetServer.URL, nil)
	if err != nil {
		fmt.Printf("Failed to create request: %v\n", err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Request execution failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Bad Proxy Intercept Count: %d\n", badProxyCallCount)
	fmt.Printf("Good Proxy Intercept Count: %d\n", goodProxyCallCount)

	if resp.StatusCode == http.StatusOK && badProxyCallCount > 0 && goodProxyCallCount > 0 {
		fmt.Println("\n=== FAILOVER SUCCESS! The client successfully intercepted the bad proxy failure and recovered using the good proxy! ===")
	} else {
		fmt.Println("\n=== FAILOVER FAILURE! The failover retry loop did not trigger as expected. ===")
	}
}
