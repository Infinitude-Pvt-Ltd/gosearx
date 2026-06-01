package engine

import (
	"context"
	"sync"
	"time"

	"gosearx/scoring"
)

// SearchOptions represents query parameters for language, country, and locale constraints.
type SearchOptions struct {
	Locale   string // e.g. "en-US"
	Language string // e.g. "en"
	Country  string // e.g. "US"
}

// SearchEngine defines the interface that all GoSearX engine drivers must satisfy.
type SearchEngine interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]scoring.EngineResult, error)
	Name() string
	Weight() float64
	Categories() []string
	Disabled() bool
	Suspend(duration time.Duration, reason string)
	SuspendedReason() (string, bool)
	Timeout() time.Duration
}

// Registry maintains the active search engines.
type Registry struct {
	mu      sync.RWMutex
	engines map[string]SearchEngine
}

// NewRegistry creates a new Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		engines: make(map[string]SearchEngine),
	}
}

// Register adds an engine to the registry.
func (r *Registry) Register(e SearchEngine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engines[e.Name()] = e
}

// Exists checks if an engine is registered.
func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, found := r.engines[name]
	return found
}

// GetEngine retrieves a registered engine by name.
func (r *Registry) GetEngine(name string) (SearchEngine, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	eng, found := r.engines[name]
	return eng, found
}

// GetActiveAndSuspendedEnginesForCategories resolves active engines and maps suspended engines for the given categories.
func (r *Registry) GetActiveAndSuspendedEnginesForCategories(categories []string) ([]SearchEngine, [][]string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []SearchEngine
	var suspended [][]string

	categoryMap := make(map[string]bool)
	for _, cat := range categories {
		categoryMap[cat] = true
	}

	for _, eng := range r.engines {
		if eng.Disabled() {
			continue
		}

		// Match category (if no categories defined for search, assume "general")
		match := false
		for _, cat := range eng.Categories() {
			if categoryMap[cat] {
				match = true
				break
			}
		}

		if match {
			if reason, isSuspended := eng.SuspendedReason(); isSuspended {
				suspended = append(suspended, []string{eng.Name(), reason})
			} else {
				active = append(active, eng)
			}
		}
	}
	return active, suspended
}

// EngineMetrics records timing and status results of a single engine search execution.
type EngineMetrics struct {
	Engine     string `json:"engine"`
	DurationMS int64  `json:"duration_ms"`
	Count      int    `json:"result_count"`
	Error      string `json:"error,omitempty"`
}

// ExecuteSearchConcurrently performs a parallel search across multiple engines (Fan-Out Pattern).
func ExecuteSearchConcurrently(ctx context.Context, engines []SearchEngine, query string, opts SearchOptions) ([]scoring.EngineResult, []EngineMetrics) {
	if len(engines) == 0 {
		return nil, nil
	}

	type engineResponse struct {
		results []scoring.EngineResult
		metrics EngineMetrics
	}

	startTime := time.Now()
	responseChan := make(chan engineResponse, len(engines))

	for _, eng := range engines {
		go func(e SearchEngine) {
			start := time.Now()
			
			// Create a child context with the engine's individual timeout
			engineCtx, cancel := context.WithTimeout(ctx, e.Timeout())
			defer cancel()

			// Execute engine query under the engine-specific child context
			results, err := e.Search(engineCtx, query, opts)
			duration := time.Since(start)

			metric := EngineMetrics{
				Engine:     e.Name(),
				DurationMS: duration.Milliseconds(),
				Count:      len(results),
			}

			if err != nil {
				metric.Error = err.Error()
				responseChan <- engineResponse{
					results: nil,
					metrics: metric,
				}
			} else {
				responseChan <- engineResponse{
					results: results,
					metrics: metric,
				}
			}
		}(eng)
	}

	var allResults []scoring.EngineResult
	var metrics []EngineMetrics
	completedEngines := 0

loop:
	for completedEngines < len(engines) {
		select {
		case resp := <-responseChan:
			completedEngines++
			if resp.results != nil {
				allResults = append(allResults, resp.results...)
			}
			metrics = append(metrics, resp.metrics)
		case <-ctx.Done():
			// The global setup timeout has been reached or client cancelled request!
			// Stop waiting and return whatever we have collected so far.
			break loop
		}
	}

	// For any engines that did not complete before we stopped waiting, log a context timeout/canceled metric
	if completedEngines < len(engines) {
		completedMap := make(map[string]bool)
		for _, m := range metrics {
			completedMap[m.Engine] = true
		}

		for _, e := range engines {
			if !completedMap[e.Name()] {
				metrics = append(metrics, EngineMetrics{
					Engine:     e.Name(),
					DurationMS: time.Since(startTime).Milliseconds(),
					Count:      0,
					Error:      ctx.Err().Error(),
				})
			}
		}
	}

	return allResults, metrics
}
