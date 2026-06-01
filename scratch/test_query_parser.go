package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"gosearx/engine"
	"gosearx/scoring"
)

func main() {
	fmt.Println("=== Testing Query Parser ===")

	// Mock registry
	reg := engine.NewRegistry()

	// Mock Wikipedia engine for the parser to find the engine override
	mockWiki := &mockSearchEngine{name: "wikipedia"}
	reg.Register(mockWiki)

	tests := []struct {
		input            string
		expectedClean    string
		expectedEngines  []string
		expectedCategory string
		expectedLanguage string
		expectedExclude  []string
	}{
		{
			input:            "!w hello :ja !!science -physics",
			expectedClean:    "hello",
			expectedEngines:  []string{"wikipedia"},
			expectedCategory: "science",
			expectedLanguage: "ja",
			expectedExclude:  []string{"physics"},
		},
		{
			input:            "quantum computing -physics -chemistry !!science",
			expectedClean:    "quantum computing",
			expectedEngines:  nil,
			expectedCategory: "science",
			expectedLanguage: "",
			expectedExclude:  []string{"physics", "chemistry"},
		},
	}

	failed := false
	for i, tt := range tests {
		parsed := engine.ParseQuery(tt.input, reg)
		fmt.Printf("Test Case %d:\n", i+1)
		fmt.Printf("  Input:    %q\n", tt.input)
		fmt.Printf("  Clean:    %q (expected: %q)\n", parsed.CleanQuery, tt.expectedClean)
		fmt.Printf("  Engines:  %v (expected: %v)\n", parsed.EngineOverrides, tt.expectedEngines)
		fmt.Printf("  Category: %q (expected: %q)\n", parsed.CategoryOverride, tt.expectedCategory)
		fmt.Printf("  Language: %q (expected: %q)\n", parsed.LanguageOverride, tt.expectedLanguage)
		fmt.Printf("  Exclude:  %v (expected: %v)\n", parsed.ExcludedTerms, tt.expectedExclude)

		if parsed.CleanQuery != tt.expectedClean {
			failed = true
			fmt.Printf("  [FAIL] CleanQuery mismatch\n")
		}
		if fmt.Sprintf("%v", parsed.EngineOverrides) != fmt.Sprintf("%v", tt.expectedEngines) {
			failed = true
			fmt.Printf("  [FAIL] EngineOverrides mismatch\n")
		}
		if parsed.CategoryOverride != tt.expectedCategory {
			failed = true
			fmt.Printf("  [FAIL] CategoryOverride mismatch\n")
		}
		if parsed.LanguageOverride != tt.expectedLanguage {
			failed = true
			fmt.Printf("  [FAIL] LanguageOverride mismatch\n")
		}
		if fmt.Sprintf("%v", parsed.ExcludedTerms) != fmt.Sprintf("%v", tt.expectedExclude) {
			failed = true
			fmt.Printf("  [FAIL] ExcludedTerms mismatch\n")
		}
		fmt.Println()
	}

	if failed {
		fmt.Println("Some tests failed!")
		os.Exit(1)
	} else {
		fmt.Println("All query parser tests passed successfully!")
	}
}

// Simple mock SearchEngine
type mockSearchEngine struct {
	name string
}

func (m *mockSearchEngine) Search(ctx context.Context, query string, opts engine.SearchOptions) ([]scoring.EngineResult, error) {
	return nil, nil
}
func (m *mockSearchEngine) Name() string                                     { return m.name }
func (m *mockSearchEngine) Weight() float64                                 { return 1.0 }
func (m *mockSearchEngine) Categories() []string                             { return []string{"general"} }
func (m *mockSearchEngine) Disabled() bool                                  { return false }
func (m *mockSearchEngine) Suspend(duration time.Duration, reason string)    {}
func (m *mockSearchEngine) SuspendedReason() (string, bool)                 { return "", false }
func (m *mockSearchEngine) Timeout() time.Duration                           { return 5 * time.Second }
