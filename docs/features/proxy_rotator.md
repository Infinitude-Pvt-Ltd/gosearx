# 🛡️ Anti-Blocking, Proxy Rotation & Failover

This component provides GoSearX with dynamic request routing, forced modern browser multiplexing, self-healing retries, and SOCKS5 Tor integration to completely eliminate IP blocking and captcha traps.

---

## 🏗️ Architecture

The proxy infrastructure resides in the `proxy` package and intercepts requests at the connection layer of Go's `http.Client`:

```mermaid
graph TD
    A[Engine HTTP Request] --> B[http.Client]
    B --> C[FailoverTransport RoundTripper]
    C --> D[Select Next Proxy IP from Rotator Pool]
    D --> E{Execute TCP Handshake}
    
    E -- Error / Timeout / 502 / 504 --> F[Rotate to Next Proxy IP]
    F --> D
    
    E -- Success 200 OK --> G[Return Search Page HTML DOM]
```

---

## ⚙️ How It Works

1. **Proxy Rotation (`proxy.Rotator`)**:
   * A pool of proxies (HTTP, HTTPS, SOCKS5) is loaded from `config/settings.yml`.
   * Each concurrent request calls `GetNextProxyURL()`, which rotates through the pool using thread-safe index increments.
2. **Forced HTTP/2 Multiplexing**:
   * When using custom dialers or proxy setups in Go, the standard HTTP client automatically falls back to HTTP/1.1.
   * GoSearX forces HTTP/2 protocol upgrades (`ForceAttemptHTTP2: true` on the `http.Transport`) to maintain concurrent performance, prevent latency spikes, and mimic authentic browser fingerprints.
3. **Self-Healing Proxy Failover (`FailoverTransport`)**:
   * A custom `http.RoundTripper` wraps our HTTP connections.
   * If a connection times out, returns connection errors, or receives gateway status blocks (like `502 Bad Gateway`, `504 Gateway Timeout`), `FailoverTransport` intercepts it, rotates to the next proxy, and retries the request seamlessly (up to `maxAttempts = 3`).
4. **Tor Network Isolation**:
   * Critical engines (e.g. Google) can be configured to route exclusively through Tor.
   * When `using_tor_proxy: true` is set, GoSearX creates an isolated client routing connection strictly through local Tor SOCKS5 sockets, giving each engine access to clean, dynamic circuits.

---

## 💻 How to Use

Configure proxy settings inside `config/settings.yml` under `outgoing`:

```yaml
outgoing:
  request_timeout: 2.0
  max_request_timeout: 3.5
  # Configure your proxy server list here:
  proxies:
    - "http://username:password@proxy-provider-ip1:8080"
    - "socks5://username:password@proxy-provider-ip2:1080"
  tor_proxy: "socks5://127.0.0.1:9050"
```

To route specific engines through Tor, configure:
```yaml
engines:
  - name: google
    type: html
    using_tor_proxy: true
```
