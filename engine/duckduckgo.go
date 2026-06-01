package engine

import (
	"context"
	"net/http"

	"gosearx/config"
	"gosearx/scoring"
)

type DuckDuckGoEngine struct {
	*ScraperEngine
}

// NewDuckDuckGoEngine constructs a new DuckDuckGo search driver.
func NewDuckDuckGoEngine(cfg config.EngineConfig, client *http.Client, proxies []string) *DuckDuckGoEngine {
	return &DuckDuckGoEngine{
		ScraperEngine: NewScraperEngine(cfg, client, proxies),
	}
}

// Search executes a DuckDuckGo HTML-only query.
func (d *DuckDuckGoEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	return d.ScraperEngine.Search(ctx, query, opts)
}
