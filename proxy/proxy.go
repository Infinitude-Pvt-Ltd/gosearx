package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type dnsCacheEntry struct {
	ips       []net.IP
	expiresAt time.Time
}

// DNSCache provides high-performance thread-safe local DNS caching.
type DNSCache struct {
	mu    sync.RWMutex
	cache map[string]dnsCacheEntry
	ttl   time.Duration
}

// NewDNSCache instantiates a DNS Cache.
func NewDNSCache(ttl time.Duration) *DNSCache {
	return &DNSCache{
		cache: make(map[string]dnsCacheEntry),
		ttl:   ttl,
	}
}

// Lookup queries the cache first, falling back to the standard resolver.
func (c *DNSCache) Lookup(ctx context.Context, host string) ([]net.IP, error) {
	c.mu.RLock()
	entry, found := c.cache[host]
	c.mu.RUnlock()

	if found && time.Now().Before(entry.expiresAt) {
		return entry.ips, nil
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[host] = dnsCacheEntry{
		ips:       ips,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return ips, nil
}

// Global DNS cache shared by all Dialers
var globalDNSCache = NewDNSCache(10 * time.Minute)

// Rotator manages a pool of proxy URLs and distributes them in a round-robin fashion.
type Rotator struct {
	proxies []*url.URL
	index   uint64
}

// NewRotator parses a list of proxy address strings and constructs a new Rotator.
func NewRotator(proxyStrings []string) (*Rotator, error) {
	var parsed []*url.URL
	for _, pStr := range proxyStrings {
		pURL, err := url.Parse(pStr)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL '%s': %w", pStr, err)
		}
		if pURL.Scheme == "" {
			pURL.Scheme = "http"
		}
		parsed = append(parsed, pURL)
	}
	return &Rotator{
		proxies: parsed,
	}, nil
}

// GetNext returns the next proxy URL in round-robin order. Returns nil if no proxies are configured.
func (r *Rotator) GetNext() *url.URL {
	if len(r.proxies) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&r.index, 1) - 1
	return r.proxies[idx%uint64(len(r.proxies))]
}

// FailoverTransport wraps http.RoundTripper to handle automatic proxy failover and request retries.
type FailoverTransport struct {
	rotator   *Rotator
	transport *http.Transport
}

// RoundTrip executes the request, catching errors and retrying using alternate rotated proxies from the pool.
func (f *FailoverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(f.rotator.proxies) == 0 {
		return f.transport.RoundTrip(req)
	}

	var lastErr error
	maxAttempts := 3
	if len(f.rotator.proxies) < maxAttempts {
		maxAttempts = len(f.rotator.proxies)
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Clone request so we don't pollute state across retries
		clonedReq := req.Clone(req.Context())

		resp, err := f.transport.RoundTrip(clonedReq)
		if err == nil {
			// Catch bad gateway / gateway timeouts from proxies and retry
			if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusServiceUnavailable {
				resp.Body.Close()
				lastErr = fmt.Errorf("proxy returned status: %d (attempt %d/%d)", resp.StatusCode, attempt, maxAttempts)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return resp, nil
		}

		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}

	return nil, fmt.Errorf("proxy failover failed after %d attempts: %w", maxAttempts, lastErr)
}

// CreateClient creates an optimized HTTP client that dynamically routes requests
// through the rotator's proxy pool in a round-robin fashion with failover support.
func (r *Rotator) CreateClient(maxIdleConns, maxConnsPerHost int, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second, // Tuned for metasearch speed (fail-fast)
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			nextProxy := r.GetNext()
			if nextProxy != nil {
				return nextProxy, nil
			}
			return nil, nil
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := globalDNSCache.Lookup(ctx, host)
			if err != nil || len(ips) == 0 {
				return dialer.DialContext(ctx, network, addr)
			}
			// Client-side load balancing
			ip := ips[rand.Intn(len(ips))]
			targetAddr := net.JoinHostPort(ip.String(), port)
			return dialer.DialContext(ctx, network, targetAddr)
		},
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second, // Tuned for metasearch speed (fail-fast)
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true, // Explicitly force HTTP/2 support even when using custom dialers
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
	}

	return &http.Client{
		Transport: &FailoverTransport{
			rotator:   r,
			transport: transport,
		},
		Timeout:   timeout,
	}
}

// CreateSingleProxyClient creates an HTTP client locked to a specific proxy.
func CreateSingleProxyClient(proxyStr string, maxIdleConns, maxConnsPerHost int, timeout time.Duration) (*http.Client, error) {
	pURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL '%s': %w", proxyStr, err)
	}

	dialer := &net.Dialer{
		Timeout:   3 * time.Second, // Tuned for metasearch speed (fail-fast)
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		Proxy: http.ProxyURL(pURL),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := globalDNSCache.Lookup(ctx, host)
			if err != nil || len(ips) == 0 {
				return dialer.DialContext(ctx, network, addr)
			}
			ip := ips[rand.Intn(len(ips))]
			targetAddr := net.JoinHostPort(ip.String(), port)
			return dialer.DialContext(ctx, network, targetAddr)
		},
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxConnsPerHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second, // Tuned for metasearch speed (fail-fast)
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true, // Force HTTP/2 client handshake
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}
