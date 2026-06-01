# 🛠️ GoSearX Customization & Engine Configuration Guide

This guide details how to customize GoSearX, add new search engines, configure selector scrapers, tweak positional scoring weights, adjust caches, and deploy Tor network routing.

---

## ⚙️ Configuration Properties (`config/settings.yml`)

The system configuration is split into three main layers: `general`, `outgoing`, and `engines`.

### 1. General Settings
Contains binding configurations, token authentication, and cache settings:
* `debug`: Enables detailed logs (e.g. crawler activity, proxy changes).
* `port`: Binding port (default: `8888`).
* `api_keys`: A list of strings used as Bearer auth tokens. If this array is empty, authentication is bypassed entirely.
* `cache`:
  * `enabled`: Globally toggle the caching layer.
  * `type`: Cache driver type: `"redis"` (recommended for cluster scale) or `"inmemory"` (lightweight sliding window).
  * `ttl`: Cache duration in seconds (default: `300`).

### 2. Outgoing Net Connections
Manages dialing metrics, proxy pools, and keep-alive parameters:
* `request_timeout`: Single engine request duration limit.
* `max_request_timeout`: Bounded maximum connection duration limit.
* `pool_max_idle_conns`: Max idle TCP connections in pool.
* `pool_max_conns_per_host`: Max TCP connections per host.
* `proxies`: A pool of strings representing HTTP/HTTPS/SOCKS5 proxies. GoSearX rotates requests through these proxies automatically to bypass rate blocks.
* `tor_proxy`: The SOCKS5 address of your local Tor router (default: `"socks5://127.0.0.1:9050"`).

---

## 🔍 Customizing and Adding Search Engines

Every engine driver in GoSearX resides in the `engines` list inside `config/settings.yml`. Engines belong to one of three `type` classes: `html` (scrapers), `json` (REST API), or `custom` (special Go code implementation).

### Scraper Selector Config (Type: `html`)
To add a new scraper engine (e.g. Yahoo or Qwant), you define CSS selector fallbacks matching the engine's current HTML DOM.

```yaml
  - name: mynewengine
    type: html
    weight: 1.0
    categories: ["general", "web"]
    search_url: "https://mysearch.com/search?q={query}&lang={language}"
    timeout: 2.5
    disabled: false
    using_tor_proxy: false
    selectors:
      result: ["div.search-result", "li.algo-card"]
      title: ["h3.result-title", "a.title-link"]
      url: ["a.title-link", "a[href]"]
      content: ["p.snippet-description", "span.snippet"]
```

#### Selector Rules:
1. **`result`**: The outer CSS container wrap of a single result card on the search engine page.
2. **`title`**: The title element relative to the result container.
3. **`url`**: The URL link element relative to the result container. The engine automatically parses the `href` attribute.
4. **`content`**: The description text element relative to the result container.

---

## 🛡️ Routing Restricted Traffic Through Tor SOCKS5

If an engine (like Google or DuckDuckGo) is aggressive in issuing IP challenges or captcha blocks, you can route all connection attempts for that specific engine exclusively through the Tor network.

1. Ensure Tor is running locally on port `9050` or set your proxy address under `outgoing.tor_proxy` in `settings.yml`.
2. Toggle `using_tor_proxy: true` under the targeted engine's configuration properties:

```yaml
  - name: google
    type: html
    ...
    using_tor_proxy: true # Routes Google engine searches strictly through Tor
```

Each Tor engine connection establishes an isolated clean circuit, fanning out queries safely without exposing your server's primary IP.

---

## 📈 Tweaking Result Scoring Weights

GoSearX aggregates results fanned in from all active drivers and merges duplicate URLs. The final score is affected by the individual search engine's **weight** (configured via `weight: <float>` in `settings.yml`).

If you want a highly specialized metasearch server where Wikipedia or Grokipedia results are prioritized over Google, increase their weight:

* Set Wikipedia `weight: 2.0`
* Set Google `weight: 1.0`

During positional sorting, a result cards list containing links from both Google and Wikipedia will rank the Wikipedia-sourced card higher even if it occupied a lower positional rank in the raw Wikipedia return stream!
