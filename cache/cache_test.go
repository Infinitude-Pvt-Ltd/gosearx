package cache

import (
	"context"
	"testing"
	"time"
)

func TestGenerateKey(t *testing.T) {
	key1 := GenerateKey("Physics", []string{"science", "general"}, "en", "US", "en-US")
	key2 := GenerateKey(" physics ", []string{"general", "science"}, "EN", "us", "en-us")

	if key1 != key2 {
		t.Errorf("GenerateKey is not deterministic or case-insensitive: key1=%s, key2=%s", key1, key2)
	}
}

func TestCacheSetGet(t *testing.T) {
	c := NewInMemoryCache(1 * time.Second)
	defer c.Close()
	ctx := context.Background()

	err := c.Set(ctx, "test_key", "hello", 1*time.Minute)
	if err != nil {
		t.Fatalf("failed to set cache key: %v", err)
	}

	val, found := c.Get(ctx, "test_key")
	if !found || val != "hello" {
		t.Errorf("Expected 'hello', found: %v (found: %t)", val, found)
	}

	c.Delete("test_key")
	_, found = c.Get(ctx, "test_key")
	if found {
		t.Errorf("Expected key to be deleted")
	}
}

func TestCacheExpiration(t *testing.T) {
	c := NewInMemoryCache(10 * time.Millisecond)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "exp_key", "world", 5*time.Millisecond)

	// Immediate check should succeed
	val, found := c.Get(ctx, "exp_key")
	if !found || val != "world" {
		t.Errorf("Expected key to exist immediately")
	}

	// Sleep to trigger expiration
	time.Sleep(15 * time.Millisecond)

	_, found = c.Get(ctx, "exp_key")
	if found {
		t.Errorf("Expected key to have expired")
	}
}

func TestCacheClear(t *testing.T) {
	c := NewInMemoryCache(1 * time.Second)
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "k1", "v1", 0)
	c.Set(ctx, "k2", "v2", 0)

	c.Clear()

	_, f1 := c.Get(ctx, "k1")
	_, f2 := c.Get(ctx, "k2")

	if f1 || f2 {
		t.Errorf("Expected all keys to be cleared")
	}
}
