# 🚀 GoSearX: High-Performance, Self-Hosted Privacy Metasearch Engine in Golang (SearXNG Alternative)

GoSearX is a premium, lightweight, lightning-fast metasearch engine written in Go. Drawing inspiration from **SearXNG** and **Crawl4AI**, it is engineered with asynchronous fan-out routing, robust anti-blocking protections, advanced in-query parsing, and dynamic web crawling capabilities.

---

## 🆚 Comparison: GoSearX vs. SearXNG

GoSearX introduces key backend features, architectural optimizations, and data gathering pipelines not present in SearXNG:

| Feature / Metric | 🚀 GoSearX | 🔍 SearXNG | Advantage / Why it matters |
| :--- | :--- | :--- | :--- |
| **Language & Footprint** | **Go** (Compiled Binary)<br>Near-zero idle memory, lightweight static executable. | **Python** (Interpreted)<br>Requires virtual envs, complex dependency management. | **High Scalability**: Deploys as a single executable with sub-millisecond goroutine scheduling. |
| **Bulk Search Endpoints** | **Yes** (`POST /search/bulk`) | **No** | **Efficiency**: Queries multiple terms concurrently in a single HTTP request, bypassing gateway rate limit shields. |
| **Automated LLM Web Crawler**| **Yes** (`POST /crawl`) | **No** | **RAG Integration**: Headless SPA dynamic rendering (`chromedp`) & automated Crawl4AI-grade readability-to-markdown parsing. |
| **Self-Healing Failovers** | **Yes** (`FailoverTransport`) | **No** | **Extreme Reliability**: Intercepts blocked proxy requests (timeouts, HTTP `502`/`504`) and instantly retries on alternate proxies. |
| **Client Connection Upgrades**| **Forced HTTP/2 Multiplexing** | **Standard HTTP/1.1 Pools** | **Performance**: Enforces parallel multiplexing even through proxy handshakes to avoid TLS signature fingerprints. |
| **DNS Optimization** | **Local Dial Cache** | **System Resolver** | **Speed**: Eliminates redundant name resolution handshakes across concurrent engine streams. |

---

## 📊 Request Execution Life-Cycle

The diagram below visualizes the architectural flow of a search query through the GoSearX gateway, middleware shield, dynamic query parsing, concurrent fanning out, self-healing proxy failover, and result scoring:

```mermaid
graph TD
    A[Client Request] --> B{LimitRate Middleware}
    B -- Limit Exceeded --> C[429 Too Many Requests]
    B -- Under Limit --> D{RequireAuth Middleware}
    D -- Invalid Token --> E[410 Unauthorized]
    D -- Valid Token --> F[Search Context Controller]
    
    F --> G[ParseQuery Overrides & Exclusions]
    G --> H{Cache Lookup}
    
    H -- Cache Hit --> I[Zero-Allocation Return]
    H -- Cache Miss --> J[Resolve Engines & Overrides]
    
    J --> K[ExecuteSearchConcurrently Goroutines]
    
    subgraph Concurrency Fan-Out Layer
        K --> L1[Engine Driver 1]
        K --> L2[Engine Driver 2]
        K --> L3[Engine Driver N]
        
        L1 --> M1[FailoverTransport Proxy Pool]
        L2 --> M2[FailoverTransport Proxy Pool]
        L3 --> M3[FailoverTransport Proxy Pool]
    end
    
    M1 --> N[AggregateAndScore Ranking Aggregator]
    M2 --> N
    M3 --> N
    
    N --> O[Backend Exclusions Pruning Filter]
    O --> P[Cache Write Layer]
    P --> Q[Unified JSON Response]
```

---

## 🌟 Core Feature Suite

### 1. High-Performance Metasearch Concurrency
* **Parallel Fan-Out Pattern**: Distributes a single query across multiple active search drivers in parallel using Goroutines and channels.
* **Intelligent Result Deduplication & Scoring**: Implements SearXNG's position-based scoring formula:
  $$\text{Contribution} = \frac{\text{Engine Weight}}{\text{Positional Rank}}$$
  Normalizes URLs dynamically (removing tracking query params like `utm_` or `gclid`) and merges duplicate result cards while preserving the most descriptive title and content snippet.
* **Engine Registry**: Modular architecture supporting HTML scraping and API integration (Google, Bing, DuckDuckGo, Brave, Yahoo, Wikipedia, Grokipedia).

### 2. [SearXNG In-Query Syntax Parity](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/features/query_parsing.md)
* **Engine & Shortcut Overrides (`!engine` / `!shortcut`)**: e.g., `!w space` routes search exclusively to Wikipedia, bypassing the categories engine.
* **Thematic Category Overrides (`!!category`)**: e.g., `!!science space` restricts queries to engines in the `science` category.
* **Language Overrides (`:language`)**: e.g., `quantum computing :fr` dynamically forces French localization rules.
* **Negative Exclusions Backend Pruning (`-term`)**: e.g., `quantum -physics`. Result cards are filtered out at the GoSearX scoring level if titles or snippets contain case-insensitive excluded words.

### 3. [Production-Grade Anti-Blocking & Proxy Rotation](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/features/proxy_rotator.md)
* **Lightweight Mobile UA Rotation (Google Bypass)**: Employs a dedicated pool of classic mobile browser signatures (e.g., Opera Mini) along with cookie/consent headers to completely bypass Google's JS Challenge Wall, delivering search results in **<300ms** and eliminating slow `chromedp` browser rendering by default.
* **Dynamic Headless Chrome Layer (`chromedp`)**: Preserves an automated, fingerprint-masked headless browser rendering engine as a fallback for standard engines and as a core driver for dynamic SPA web page crawling (`POST /crawl`).
* **Proxy Rotator Pool**: Fans out concurrent requests through a pool of configured HTTP/HTTPS/SOCKS5 proxies.
* **Self-Healing Proxy Failover (`FailoverTransport`)**: Intercepts networking failures (timeouts, `502`, `504`) and automatically retries requests using alternate proxies from the pool.
* **HTTP/2 Upgrades**: Forces HTTP/2 multiplexing (`ForceAttemptHTTP2`) across all proxy handshakes to improve speeds and match organic client behavior.
* **Tor Integration**: Configurable isolation routing via local Tor SOCKS5 proxies for restricted search engines.

### 4. [Advanced Bot Limiter Shield](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/features/rate_limiter.md)
* **Sliding Window Rate Limiter**: High-performance middleware (`LimitRate`) restricting client traffic to **5 requests per 10 seconds** per client IP.
* **Flexible Cache Backends**: Powered by either a thread-safe local `InMemoryCache` (with a janitor eviction routine) or a premium `RedisCache` cluster.
* **Auth-Shield**: Strict token-based Bearer authentication (`RequireAuth` middleware).

### 5. [High-Performance Bulk Search Endpoint (POST /search/bulk)](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/features/bulk_search.md)
* **DRY Architecture**: Refactored query execution into a reusable `SearchSingle` method.
* **Parallel Query Fan-Out**: Processes lists of queries in concurrent Goroutines synchronized via `sync.WaitGroup` and a thread-safe `sync.Mutex`.
* **Rate-Limit Bypass**: Allows clients to execute dozens of concurrent query searches in a single HTTP request round-trip, fanning them out across different rotated proxy IPs simultaneously.

### 6. [Automated Markdown Content Crawler (POST /crawl)](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/features/crawler.md)
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
    - "gosearx_sec_****_****_****"
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
    - "http://u****:p****@proxy-provider-ip1:8080"
    - "http://u****:p****@proxy-provider-ip2:8080"
  tor_proxy: "socks5://127.0.0.1:9050"
```

---

## 📡 API Endpoints

### 1. POST `/search`
Submit a single search query across fanned-out engines.

* **Headers**:
  ```http
  Authorization: Bearer gosearx_sec_****_****_****
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
  Authorization: Bearer gosearx_sec_****_****_****
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
  Authorization: Bearer gosearx_sec_****_****_****
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

---

## 📖 Deep-Dive Documentation
For detailed guides on utilizing the system and creating custom search engines, check out our docs:
* 📄 [Usage & Query Operations Guide](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/usage.md)
* 📄 [Customization & Engine Configuration Guide](file:///Volumes/Transend/Build/Experimental/contents/gosearx/docs/customization.md)

---

## ⚖️ License

This project is open-sourced under the **MIT License**. Feel free to use, modify, and distribute it in accordance with the license guidelines. See the [LICENSE](file:///Volumes/Transend/Build/Experimental/contents/gosearx/LICENSE) file for more details.
