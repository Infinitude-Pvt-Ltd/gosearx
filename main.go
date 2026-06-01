package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gosearx/cache"
	"gosearx/config"
	"gosearx/engine"
	"gosearx/proxy"
	"gosearx/server"
)

func main() {
	// Premium colored logs setup
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	fmt.Println(`
 ██████╗  ██████╗ ███████╗███████╗ █████╗ ██████╗ ██╗  ██╗
██╔════╝ ██╔═══██╗██╔════╝██╔════╝██╔══██╗██╔══██╗╚██╗██╔╝
██║  ███╗██║   ██║███████╗█████╗  ███████║██████╔╝ ╚███╔╝ 
██║   ██║██║   ██║╚════██║██╔══╝  ██╔══██║██╔══██╗ ██╔██╗ 
╚██████╔╝╚██████╔╝███████║███████╗██║  ██║██║  ██║██╔╝ ██╗
 ╚═════╝  ╚═════╝ ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝
                      High-Performance Metasearch Server in Go
	`)

	// 1. Load configuration
	fmt.Printf("[INFO] Loading configuration from config/settings.yml...\n")
	cfg, err := config.FindAndLoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Configuration load failed: %v", err)
	}
	fmt.Printf("[INFO] Configuration loaded successfully. Port: %d, Bind: %s, Debug: %t\n", cfg.General.Port, cfg.General.BindAddress, cfg.General.Debug)

	// 2. Setup dynamic Proxy Rotator
	fmt.Printf("[INFO] Initializing Proxy Rotator with %d configured proxies...\n", len(cfg.Outgoing.Proxies))
	rotator, err := proxy.NewRotator(cfg.Outgoing.Proxies)
	if err != nil {
		log.Fatalf("[FATAL] Proxy rotator initialization failed: %v", err)
	}

	// 3. Build global optimized HTTP Client with dynamic proxy support
	clientTimeout := time.Duration(cfg.Outgoing.MaxRequestTimeout * float64(time.Second))
	client := rotator.CreateClient(
		cfg.Outgoing.PoolMaxIdleConns,
		cfg.Outgoing.PoolMaxConnsPerHost,
		clientTimeout,
	)
	fmt.Printf("[INFO] Dynamic proxy-enabled HTTP client initialized. Pool Size: %d, Host Max Conns: %d, Hard Timeout: %v\n",
		cfg.Outgoing.PoolMaxIdleConns, cfg.Outgoing.PoolMaxConnsPerHost, clientTimeout)

	// 4. Initialize and populate Search Engine Registry
	registry := engine.NewRegistry()
	activeCount := 0

	fmt.Printf("[INFO] Initializing Search Engine drivers...\n")
	for _, engCfg := range cfg.Engines {
		var driver engine.SearchEngine

		// Determine the active HTTP client for this specific engine
		engineClient := client
		if engCfg.UsingTorProxy {
			torClient, err := proxy.CreateSingleProxyClient(
				cfg.Outgoing.TorProxy,
				cfg.Outgoing.PoolMaxIdleConns,
				cfg.Outgoing.PoolMaxConnsPerHost,
				clientTimeout,
			)
			if err != nil {
				fmt.Printf("[ERROR] Failed to initialize Tor proxy client for engine '%s': %v. Falling back to default client.\n", engCfg.Name, err)
			} else {
				engineClient = torClient
				fmt.Printf("[INFO] Configured engine '%s' to route requests exclusively through Tor SOCKS5 proxy: %s\n", engCfg.Name, cfg.Outgoing.TorProxy)
			}
		}

		switch engCfg.Type {
		case "custom":
			if engCfg.Name == "wikipedia" {
				driver = engine.NewWikipediaEngine(engCfg, engineClient)
			} else if engCfg.Name == "grokipedia" {
				driver = engine.NewGrokipediaEngine(engCfg, engineClient)
			} else {
				fmt.Printf("[WARNING] Engine '%s' specifies unknown custom driver. Skipping.\n", engCfg.Name)
				continue
			}
		case "html":
			switch engCfg.Name {
			case "google":
				driver = engine.NewGoogleEngine(engCfg, engineClient, cfg.Outgoing.Proxies)
			case "bing":
				driver = engine.NewBingEngine(engCfg, engineClient, cfg.Outgoing.Proxies)
			case "duckduckgo":
				driver = engine.NewDuckDuckGoEngine(engCfg, engineClient, cfg.Outgoing.Proxies)
			case "brave":
				driver = engine.NewBraveEngine(engCfg, engineClient, cfg.Outgoing.Proxies)
			default:
				// Fallback to generic selector-based scraper driver
				driver = engine.NewScraperEngine(engCfg, engineClient, cfg.Outgoing.Proxies)
			}
		default:
			fmt.Printf("[WARNING] Engine '%s' has unknown type '%s'. Skipping.\n", engCfg.Name, engCfg.Type)
			continue
		}

		if driver != nil {
			registry.Register(driver)
			status := "ENABLED"
			if driver.Disabled() {
				status = "DISABLED"
			} else {
				activeCount++
			}
			fmt.Printf("  -> Registered Engine [%-12s] | Type: %-6s | Weight: %.1f | Status: %s\n",
				driver.Name(), engCfg.Type, driver.Weight(), status)
		}
	}
	fmt.Printf("[INFO] Engine Registry populated. Active Engines: %d/%d\n", activeCount, len(cfg.Engines))

	// 5. Setup Router & Server Context
	var searchCache cache.SearchCache
	if cfg.General.Cache.Enabled {
		if cfg.General.Cache.Type == "redis" {
			searchCache = cache.NewRedisCache(
				cfg.General.Cache.Redis.Address,
				cfg.General.Cache.Redis.Password,
				cfg.General.Cache.Redis.DB,
			)
			fmt.Printf("[INFO] Redis Caching Layer enabled. Address: %s, DB: %d, TTL: %d seconds\n",
				cfg.General.Cache.Redis.Address, cfg.General.Cache.Redis.DB, cfg.General.Cache.TTL)
		} else {
			searchCache = cache.NewInMemoryCache(30 * time.Second)
			fmt.Printf("[INFO] In-Memory Caching Layer enabled. TTL: %d seconds\n", cfg.General.Cache.TTL)
		}
		defer searchCache.Close()
	} else {
		fmt.Printf("[INFO] Caching layer is disabled in settings.\n")
	}

	handlerCtx := server.NewHandlerContext(cfg, registry, searchCache, client)
	mux := http.NewServeMux()
	
	mux.HandleFunc("POST /search", handlerCtx.LimitRate(handlerCtx.RequireAuth(handlerCtx.HandleSearch)))
	mux.HandleFunc("POST /search/bulk", handlerCtx.LimitRate(handlerCtx.RequireAuth(handlerCtx.HandleBulkSearch)))
	mux.HandleFunc("POST /crawl", handlerCtx.LimitRate(handlerCtx.RequireAuth(handlerCtx.HandleCrawl)))
	mux.HandleFunc("GET /health", handlerCtx.HandleHealth)

	// Configure server
	addr := fmt.Sprintf("%s:%d", cfg.General.BindAddress, cfg.General.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// 6. Start HTTP Server in a background goroutine
	go func() {
		fmt.Printf("[INFO] Starting server on http://%s...\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] Server listen error: %v", err)
		}
	}()

	// 7. Setup Trapping for Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Block until a termination signal is caught
	sig := <-stop
	fmt.Printf("\n[INFO] Termination signal caught (%v). Starting graceful shutdown...\n", sig)

	// Context with 5-second timeout for server cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("[FATAL] Server forced shutdown error: %v", err)
	}

	fmt.Printf("[INFO] GoSearX server terminated successfully. Goodbye!\n")
}
