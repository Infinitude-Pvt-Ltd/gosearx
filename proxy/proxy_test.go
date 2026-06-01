package proxy

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestRotator(t *testing.T) {
	proxyStrings := []string{
		"http://127.0.0.1:8080",
		"socks5://user:pass@127.0.0.1:1080",
	}

	rotator, err := NewRotator(proxyStrings)
	if err != nil {
		t.Fatalf("failed to create proxy rotator: %v", err)
	}

	if len(rotator.proxies) != 2 {
		t.Errorf("expected 2 parsed proxies, got %d", len(rotator.proxies))
	}

	// Verify SOCKS5 scheme and credentials
	s5 := rotator.proxies[1]
	if s5.Scheme != "socks5" {
		t.Errorf("expected socks5 scheme, got %s", s5.Scheme)
	}
	if s5.User.Username() != "user" {
		t.Errorf("expected user 'user', got %s", s5.User.Username())
	}

	// Verify Round-Robin cycling
	p1 := rotator.GetNext()
	p2 := rotator.GetNext()
	p3 := rotator.GetNext() // should wrap around to first proxy

	if p1.Host != "127.0.0.1:8080" {
		t.Errorf("expected first proxy host to be 127.0.0.1:8080, got %s", p1.Host)
	}
	if p2.Host != "127.0.0.1:1080" {
		t.Errorf("expected second proxy host to be 127.0.0.1:1080, got %s", p2.Host)
	}
	if p3.Host != p1.Host {
		t.Errorf("expected wrap-around to p1, got host %s", p3.Host)
	}
}

func TestDNSCache(t *testing.T) {
	cache := NewDNSCache(100 * time.Millisecond)
	ctx := context.Background()

	// Perform first lookup (uncached)
	ips1, err := cache.Lookup(ctx, "localhost")
	if err != nil {
		t.Skip("Skipping local DNS test: localhost lookup failed: ", err)
		return
	}
	if len(ips1) == 0 {
		t.Skip("Skipping local DNS test: no IPs found for localhost")
		return
	}

	// Perform second lookup (should hit cache)
	ips2, err := cache.Lookup(ctx, "localhost")
	if err != nil {
		t.Fatalf("dns cache lookup failed on second attempt: %v", err)
	}

	if len(ips1) != len(ips2) {
		t.Errorf("dns cache returned different IP count: %d vs %d", len(ips1), len(ips2))
	}

	// Inject custom IP to prove cache hit bypasses resolver
	cache.mu.Lock()
	cache.cache["dummy.test"] = dnsCacheEntry{
		ips:       []net.IP{net.ParseIP("192.0.2.1")},
		expiresAt: time.Now().Add(50 * time.Millisecond),
	}
	cache.mu.Unlock()

	ips3, err := cache.Lookup(ctx, "dummy.test")
	if err != nil || len(ips3) != 1 || ips3[0].String() != "192.0.2.1" {
		t.Errorf("failed to retrieve cached entry for dummy.test: %v, got %v", err, ips3)
	}

	// Wait for expiration
	time.Sleep(70 * time.Millisecond)

	_, err = cache.Lookup(ctx, "dummy.test")
	if err == nil {
		t.Errorf("expected expired cache lookup to fail lookup for unresolvable dummy.test host")
	}
}
