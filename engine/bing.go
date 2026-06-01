package engine

import (
	"context"
	"net/http"

	"gosearx/config"
	"gosearx/scoring"
)

type BingEngine struct {
	*ScraperEngine
}

// NewBingEngine constructs a new Bing search driver.
func NewBingEngine(cfg config.EngineConfig, client *http.Client, proxies []string) *BingEngine {
	return &BingEngine{
		ScraperEngine: NewScraperEngine(cfg, client, proxies),
	}
}

// Search executes a Bing query. Bing-specific request overrides can be defined here.
func (b *BingEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	return b.ScraperEngine.Search(ctx, query, opts)
}
