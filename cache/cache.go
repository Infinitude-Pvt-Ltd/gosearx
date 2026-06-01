package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// SearchCache defines the unified cache driver interface.
type SearchCache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Close() error
}

type cacheItem struct {
	value      string
	expiration int64
}

func (item cacheItem) Expired() bool {
	if item.expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.expiration
}

// InMemoryCache is a thread-safe, local key-value cache satisfying SearchCache.
type InMemoryCache struct {
	mu            sync.RWMutex
	items         map[string]cacheItem
	cleanupPeriod time.Duration
	stopJanitor   chan struct{}
}

// NewInMemoryCache creates a new InMemoryCache.
func NewInMemoryCache(cleanupPeriod time.Duration) *InMemoryCache {
	c := &InMemoryCache{
		items:         make(map[string]cacheItem),
		cleanupPeriod: cleanupPeriod,
		stopJanitor:   make(chan struct{}),
	}
	go c.janitorLoop()
	return c
}

// Close stops the background cleanup janitor.
func (c *InMemoryCache) Close() error {
	close(c.stopJanitor)
	return nil
}

// Set stores a value with a specific TTL.
func (c *InMemoryCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	var exp int64
	if ttl > 0 {
		exp = time.Now().Add(ttl).UnixNano()
	}

	c.mu.Lock()
	c.items[key] = cacheItem{
		value:      value,
		expiration: exp,
	}
	c.mu.Unlock()
	return nil
}

// Get retrieves a value. Returns "", false if not found or expired.
func (c *InMemoryCache) Get(ctx context.Context, key string) (string, bool) {
	c.mu.RLock()
	item, found := c.items[key]
	c.mu.RUnlock()

	if !found {
		return "", false
	}

	if item.Expired() {
		// Clean up immediately on read hit
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return "", false
	}

	return item.value, true
}

// Delete removes an item manually.
func (c *InMemoryCache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Clear flushes all cached items.
func (c *InMemoryCache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]cacheItem)
	c.mu.Unlock()
}

// janitorLoop performs periodic scans to evict expired cache keys.
func (c *InMemoryCache) janitorLoop() {
	ticker := time.NewTicker(c.cleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now().UnixNano()
			for k, item := range c.items {
				if item.expiration > 0 && now > item.expiration {
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		case <-c.stopJanitor:
			return
		}
	}
}

// RedisCache is a concrete driver backing by a real Redis client.
type RedisCache struct {
	client *redis.Client
}

// NewRedisCache instantiates a connection to the Redis server.
func NewRedisCache(addr, password string, db int) *RedisCache {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &RedisCache{
		client: rdb,
	}
}

// Close disconnects from the Redis server.
func (r *RedisCache) Close() error {
	return r.client.Close()
}

// Set saves the value in Redis under the given key and expiration window.
func (r *RedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Get retrieves the value from Redis.
func (r *RedisCache) Get(ctx context.Context, key string) (string, bool) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false
	} else if err != nil {
		// Log error or log fail-soft, fallback to cache miss to keep service responsive
		return "", false
	}
	return val, true
}

// GenerateKey builds a deterministic, unique cache key prefixed with gosearx: to avoid clashes.
func GenerateKey(query string, categories []string, lang, country, locale string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	cats := make([]string, len(categories))
	for i, cat := range categories {
		cats[i] = strings.ToLower(strings.TrimSpace(cat))
	}
	// Sort categories
	for i := 0; i < len(cats); i++ {
		for j := i + 1; j < len(cats); j++ {
			if cats[i] > cats[j] {
				cats[i], cats[j] = cats[j], cats[i]
			}
		}
	}

	rawKey := "gosearx:" + q + "|" + strings.Join(cats, ",") + "|" + strings.ToLower(lang) + "|" + strings.ToLower(country) + "|" + strings.ToLower(locale)
	
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}

// GenerateCrawlKey builds a deterministic, unique cache key for crawled web pages.
func GenerateCrawlKey(targetURL string) string {
	rawKey := "gosearx:crawl:" + strings.TrimSpace(strings.ToLower(targetURL))
	hash := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(hash[:])
}
