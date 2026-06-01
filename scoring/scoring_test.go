package scoring

import (
	"testing"
)

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Basic HTTPS URL",
			input:    "https://example.com/some/path",
			expected: "example.com/some/path",
		},
		{
			name:     "HTTP with www",
			input:    "http://www.example.com/some/path/",
			expected: "example.com/some/path",
		},
		{
			name:     "Default index file",
			input:    "https://example.com/index.html",
			expected: "example.com",
		},
		{
			name:     "Tracking parameters",
			input:    "https://example.com/path?utm_source=google&utm_medium=cpc&q=gosearx&gclid=123",
			expected: "example.com/path?q=gosearx",
		},
		{
			name:     "Host trailing slash",
			input:    "https://example.com/",
			expected: "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := CanonicalizeURL(tt.input)
			if actual != tt.expected {
				t.Errorf("CanonicalizeURL(%s) = %s; want %s", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestAggregateAndScore(t *testing.T) {
	raw := []EngineResult{
		{
			Title:    "Go Programming Language",
			URL:      "https://go.dev/",
			Content:  "The Go programming language is an open source project.",
			Engine:   "google",
			Category: "general",
			Rank:     1,
			Weight:   1.0,
		},
		{
			Title:    "Go (programming language)",
			URL:      "https://go.dev", // Canonicalized as identical to the first link!
			Content:  "Go is an open-source programming language created at Google.",
			Engine:   "bing",
			Category: "general",
			Rank:     2,
			Weight:   0.8,
		},
		{
			Title:    "Brave Search Engine",
			URL:      "https://search.brave.com",
			Content:  "Search privately with Brave Search.",
			Engine:   "brave",
			Category: "general",
			Rank:     1,
			Weight:   1.0,
		},
	}

	aggregated := AggregateAndScore(raw, nil)

	if len(aggregated) != 2 {
		t.Errorf("expected 2 aggregated results, got %d", len(aggregated))
		return
	}

	// First item should be go.dev since it appeared in 2 engines and has higher merged score
	// Score for go.dev: (1.0 / 1) + (0.8 / 2) = 1.0 + 0.4 = 1.4
	// Score for brave: (1.0 / 1) = 1.0
	first := aggregated[0]
	if first.URL != "https://go.dev/" {
		t.Errorf("expected first result to be go.dev, got %s", first.URL)
	}

	if first.Score != 1.4 {
		t.Errorf("expected go.dev score to be 1.4, got %f", first.Score)
	}

	if len(first.Engines) != 2 {
		t.Errorf("expected go.dev to have 2 engines registered, got %d", len(first.Engines))
	}

	// The title should be the longer one ("Go (programming language)")
	expectedTitle := "Go (programming language)"
	if first.Title != expectedTitle {
		t.Errorf("expected merged title to be '%s', got '%s'", expectedTitle, first.Title)
	}
}

func TestAggregateAndScoreWithExclusions(t *testing.T) {
	raw := []EngineResult{
		{
			Title:    "Go Programming Language",
			URL:      "https://go.dev/",
			Content:  "The Go programming language is an open source project.",
			Engine:   "google",
			Category: "general",
			Rank:     1,
			Weight:   1.0,
		},
		{
			Title:    "Brave Search Engine",
			URL:      "https://search.brave.com",
			Content:  "Search privately with Brave Search.",
			Engine:   "brave",
			Category: "general",
			Rank:     1,
			Weight:   1.0,
		},
	}

	// Exclude "brave"
	aggregated := AggregateAndScore(raw, []string{"brave"})

	if len(aggregated) != 1 {
		t.Errorf("expected 1 aggregated result after exclusion, got %d", len(aggregated))
		return
	}

	if aggregated[0].URL != "https://go.dev/" {
		t.Errorf("expected remaining result to be go.dev, got %s", aggregated[0].URL)
	}
}
