# 📖 GoSearX Usage & Query Operations Guide

This guide describes how to execute single, bulk, and crawling requests using GoSearX, along with complete rules for inline operators, localization restrict rules, and content scraping features.

---

## 🔑 Authentication
All API endpoints (except `/health`) require Bearer token authentication. Ensure that the target client sets the HTTP `Authorization` header exactly as follows:

```http
Authorization: Bearer gosearx_sec_****_****_****
```

The tokens are configured inside `config/settings.yml` under the `general.api_keys` list.

---

## ⚡ SearXNG-Style Query Operators
You can pass custom override syntax directly in the query strings of either `/search` or `/search/bulk` endpoints. The query syntax engine tokenizes and parses these values at the backend level.

### 1. Engine Override (`!engine` or `!shortcut`)
Routes requests exclusively to targeted search drivers, completely bypassing standard category defaults.
* **Format**: Prefix the engine name or its shortcut with a single `!` character.
* **Shortcut Maps**:
  * `!g` $\rightarrow$ Google
  * `!b` $\rightarrow$ Bing
  * `!ddg` $\rightarrow$ DuckDuckGo
  * `!br` $\rightarrow$ Brave
  * `!w` $\rightarrow$ Wikipedia
  * `!gk` $\rightarrow$ Grokipedia
  * `!y` $\rightarrow$ Yahoo
* **Examples**:
  * `!w space exploration` (queries only Wikipedia for "space exploration")
  * `!g gosearx metasearch` (queries only Google for "gosearx metasearch")

### 2. Category Override (`!!category`)
Overrides requested categories and routes queries to all active search engines registered under that specific category name.
* **Format**: Prefix the category name with `!!`.
* **Example**: `!!science quantum physics` (queries all engines categorized as "science")

### 3. Language Override (`:language`)
Sets regional localization restrict rules on engine scrape URLs (e.g. Google's `lr` lang restrict rules).
* **Format**: Prefix a ISO 639-1 language code with `:` (e.g. `:ja` for Japanese, `:fr` for French, `:de` for German).
* **Example**: `artificial intelligence :ja` (filters results localized for Japan)

### 4. Negative Exclusions (`-term`)
Filters result cards directly at the backend level during positional scoring.
* **Format**: Prefix any word you want to exclude with `-`.
* **Example**: `quantum computing -physics` (prunes any result cards whose title or content snippet contains "physics")

---

## 📡 API Usage Guides

### 1. Single Search (`POST /search`)
Used to submit a single search query fanned out in parallel.

* **Payload Properties**:
  * `q` (string, Required): The search query, supporting inline operators.
  * `categories` (array of strings, Optional): Search domains to query. Defaults to `["general"]`.
  * `locale` (string, Optional): Query locale rules, e.g. `en-US`.
  * `language` (string, Optional): Defaults to language extracted from locale.
  * `country` (string, Optional): Defaults to country extracted from locale.
  * `timeout` (float, Optional): Search duration soft deadline.

* **Curl Example**:
  ```bash
  curl -X POST http://localhost:8888/search \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer gosearx_sec_****_****_****" \
    -d '{"q": "!w quantum computing -physics", "timeout": 2.5}'
  ```

---

### 2. Concurrent Bulk Search (`POST /search/bulk`)
Executes an array of distinct search queries in parallel. This bypasses client-side gateway rate limits because it counts as only one request, and dynamically fans them out across rotated proxies to prevent engine-side IP blocking.

* **Payload Properties**:
  * `queries` (array of strings, Required): List of distinct queries to execute concurrently.
  * `categories` (array of strings, Optional): Search domains to query. Defaults to `["general"]`.
  * `locale` (string, Optional): Query locale rules.
  * `timeout` (float, Optional): Soft timeout threshold.

* **Curl Example**:
  ```bash
  curl -X POST http://localhost:8888/search/bulk \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer gosearx_sec_****_****_****" \
    -d '{"queries": ["!w space exploration", "gosearx metasearch", "!gk quantum computing"]}'
  ```

---

### 3. Readability & Markdown Web Crawler (`POST /crawl`)
Fetches single or multiple target URLs in parallel and extracts their readability content in clean, consolidated markdown format designed for ingestion by LLM/RAG engines.

* **Payload Properties**:
  * `url` (string, Optional): Single target webpage URL.
  * `urls` (array of strings, Optional): Multiple target webpage URLs.
  * `js_render` (boolean, Optional): Enables dynamic headless browser page execution (via `chromedp`) for SPA frameworks.
  * `wait_ms` (integer, Optional): Headless wait duration for page load hydration.
  * `wait_selector` (string, Optional): Headless CSS element selector to wait for visibility before DOM extraction.
  * `extract_selector` (string, Optional): Extracts only targeted element selectors from the DOM instead of full readability parsing.
  * `markdown` (boolean, Optional): Automatically converts HTML structure to clean, formatted Markdown.

* **Curl Example**:
  ```bash
  curl -X POST http://localhost:8888/crawl \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer gosearx_sec_****_****_****" \
    -d '{"urls": ["https://example.com/blog/1", "https://example.com/blog/2"], "js_render": true, "markdown": true}'
  ```
