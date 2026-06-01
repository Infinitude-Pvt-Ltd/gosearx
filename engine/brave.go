package engine

import (
	"context"
	"net/http"

	"gosearx/config"
	"gosearx/scoring"
)

type BraveEngine struct {
	*ScraperEngine
}

// NewBraveEngine constructs a new Brave search driver.
func NewBraveEngine(cfg config.EngineConfig, client *http.Client, proxies []string) *BraveEngine {
	return &BraveEngine{
		ScraperEngine: NewScraperEngine(cfg, client, proxies),
	}
}

// Search executes a Brave search query.
func (br *BraveEngine) Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error) {
	return br.ScraperEngine.Search(ctx, query, opts)
}
