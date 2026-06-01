# 🚀 GoSearX: High-Performance, Privacy-Respecting Metasearch Server in Go

GoSearX is a premium, lightweight, lightning-fast metasearch engine written in Go. Drawing inspiration from **SearXNG** and **Crawl4AI**, it is engineered with asynchronous fan-out routing, robust anti-blocking protections, advanced in-query parsing, and dynamic web crawling capabilities.

---

## 🌟 Core Feature Suite

### 1. High-Performance Metasearch Concurrency
* **Parallel Fan-Out Pattern**: Distributes a single query across multiple active search drivers in parallel using Goroutines and channels.
* **Intelligent Result Deduplication & Scoring**: Implements SearXNG's position-based scoring formula:
  $$\text{Contribution} = \frac{\text{Engine Weight}}{\text{Positional Rank}}$$
  Normalizes URLs dynamically (removing tracking query params like `utm_` or `gclid`) and merges duplicate result cards while preserving the most descriptive title and content snippet.
* **Engine Registry**: Modular architecture supporting HTML scraping and API integration (Google, Bing, DuckDuckGo, Brave, Yahoo, Wikipedia, Grokipedia).

### 2. SearXNG In-Query Syntax Parity
* **Engine & Shortcut Overrides (`!engine` / `!shortcut`)**: e.g., `!w space` routes search exclusively to Wikipedia, bypassing the categories engine.
* **Thematic Category Overrides (`!!category`)**: e.g., `!!science space` restricts queries to engines in the `science` category.
* **Language Overrides (`:language`)**: e.g., `quantum computing :fr` dynamically forces French localization rules.
* **Negative Exclusions Backend Pruning (`-term`)**: e.g., `quantum -physics`. Result cards are filtered out at the GoSearX scoring level if titles or snippets contain case-insensitive excluded words.

### 3. Production-Grade Anti-Blocking & Proxy Rotation
* **Dynamic Headless Chrome Layer (`chromedp`)**: Employs an automated, fingerprint-masked headless browser rendering engine for dynamic SPA web pages.
* **Proxy Rotator Pool**: Fans out concurrent requests through a pool of configured HTTP/HTTPS/SOCKS5 proxies.
* **Self-Healing Proxy Failover (`FailoverTransport`)**: Intercepts networking failures (timeouts, `502`, `504`) and automatically retries requests using alternate proxies from the pool.
* **HTTP/2 Upgrades**: Forces HTTP/2 multiplexing (`ForceAttemptHTTP2`) across all proxy handshakes to improve speeds and match organic client behavior.
* **Tor Integration**: Configurable isolation routing via local Tor SOCKS5 proxies for restricted search engines.

### 4. Advanced Bot Limiter Shield
* **Sliding Window Rate Limiter**: High-performance middleware (`LimitRate`) restricting client traffic to **5 requests per 10 seconds** per client IP.
* **Flexible Cache Backends**: Powered by either a thread-safe local `InMemoryCache` (with a janitor eviction routine) or a premium `RedisCache` cluster.
* **Auth-Shield**: Strict token-based Bearer authentication (`RequireAuth` middleware).

### 5. High-Performance Bulk Search Endpoint (`POST /search/bulk`)
* **DRY Architecture**: Refactored query execution into a reusable `SearchSingle` method.
* **Parallel Query Fan-Out**: Processes lists of queries in concurrent Goroutines synchronized via `sync.WaitGroup` and a thread-safe `sync.Mutex`.
* **Rate-Limit Bypass**: Allows clients to execute dozens of concurrent query searches in a single HTTP request round-trip, fanning them out across different rotated proxy IPs simultaneously.

### 6. Automated Markdown Content Crawler (`POST /crawl`)
* **Smart Article Extraction**: Utilizes `go-readability` and DOM selectors to pull clean article payloads from target URLs in parallel.
* **LLM-Ready Markdown Conversion**: Integrates a structural converter (`html-to-markdown`) with Crawl4AI whitespace-cleaning rules (collapsing duplicate lines, pruning empty list bullet lines).

---

## 🛠️ Installation & Setup

### Prerequisites
* Go 1.21 or higher
* Redis (Optional, for distributed caching)
* Google Chrome or Chromium (Optional, for headless rendering)

### Clone & Build
```bash
git clone https://github.com/Infinitude-Pvt-Ltd/gosearx.git
cd gosearx
go build -o gosearx main.go
```

### Configuration
GoSearX is configured via the `config/settings.yml` file. Here is a configuration snapshot:

```yaml
general:
  debug: true
  port: 8888
  bind_address: "0.0.0.0"
  api_keys:
    - "gosearx_sec_7df2_965c_a4b9"
  cache:
    enabled: true
    type: "redis" # "redis" or "inmemory"
    ttl: 300
    redis:
      address: "127.0.0.1:6379"

outgoing:
  request_timeout: 2.0
  max_request_timeout: 3.5
  proxies:
    - "http://username:password@proxy-provider-ip1:8080"
    - "http://username:password@proxy-provider-ip2:8080"
  tor_proxy: "socks5://127.0.0.1:9050"
```

---

## 📡 API Endpoints

### 1. POST `/search`
Submit a single search query across fanned-out engines.

* **Headers**:
  ```http
  Authorization: Bearer gosearx_sec_7df2_965c_a4b9
  Content-Type: application/json
  ```
* **Payload**:
  ```json
  {
    "q": "!w Space Exploration -wikipedia",
    "categories": ["general"],
    "locale": "en-US",
    "timeout": 3.0
  }
  ```

---

### 2. POST `/search/bulk`
Submit multiple search queries in parallel. Perfect for bulk data ingestion without hitting IP blocks.

* **Headers**:
  ```http
  Authorization: Bearer gosearx_sec_7df2_965c_a4b9
  Content-Type: application/json
  ```
* **Payload**:
  ```json
  {
    "queries": [
      "!w quantum computing",
      "gosearx metasearch engine",
      "!gk artificial intelligence"
    ],
    "categories": ["general"],
    "locale": "en-US",
    "timeout": 4.0
  }
  ```

---

### 3. POST `/crawl`
Fetch targets concurrently and extract readable content converted to clean markdown for LLM consumption.

* **Headers**:
  ```http
  Authorization: Bearer gosearx_sec_7df2_965c_a4b9
  Content-Type: application/json
  ```
* **Payload**:
  ```json
  {
    "urls": [
      "https://example.com/article1",
      "https://example.com/article2"
    ],
    "js_render": true,
    "wait_ms": 1000,
    "markdown": true
  }
  ```

---

### 4. GET `/health`
Returns system health diagnostics.

* **Response**:
  ```json
  {
    "status": "healthy",
    "timestamp": "2026-06-01T16:00:00Z",
    "service": "GoSearX"
  }
  ```

---

## 🧪 Verification & Testing

GoSearX comes with a lightweight, robust verification suite to test all advanced components in isolation.

### 1. Run Unit Tests
```bash
go test -v ./scoring/... ./cache/... ./proxy/...
```

### 2. Test In-Query Operators & Overrides
```bash
go run scratch/test_query_parser.go
```

### 3. Test Bot Limiter Shield Rate Limiter
```bash
go run scratch/test_rate_limiter.go
```

### 4. Test Concurrent Bulk Searches
```bash
go run scratch/test_bulk_search.go
```
