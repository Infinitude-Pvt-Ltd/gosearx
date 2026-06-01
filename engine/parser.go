package engine

import (
	"strings"
)

type ParsedQuery struct {
	CleanQuery       string
	EngineOverrides  []string
	CategoryOverride string
	LanguageOverride string
	ExcludedTerms    []string
}

// ParseQuery tokenizes a search query, extracts inline SearXNG-style operators,
// and returns a cleaned query string alongside active overrides and exclusions.
func ParseQuery(query string, registry *Registry) ParsedQuery {
	tokens := strings.Fields(query)
	var cleanTokens []string
	var engines []string
	var category string
	var language string
	var exclusions []string

	// Engine shortcuts map (SearXNG style)
	shortcuts := map[string]string{
		"g":   "google",
		"b":   "bing",
		"ddg": "duckduckgo",
		"br":  "brave",
		"w":   "wikipedia",
		"gk":  "grokipedia",
		"y":   "yahoo",
	}

	for _, token := range tokens {
		// Category override: starts with "!!"
		if strings.HasPrefix(token, "!!") && len(token) > 2 {
			cat := strings.ToLower(token[2:])
			category = cat
			continue
		}

		// Engine override: starts with "!"
		if strings.HasPrefix(token, "!") && len(token) > 1 {
			engName := strings.ToLower(token[1:])
			if full, found := shortcuts[engName]; found {
				engName = full
			}
			if registry.Exists(engName) {
				engines = append(engines, engName)
				continue
			}
		}

		// Language override: starts with ":"
		if strings.HasPrefix(token, ":") && len(token) > 1 {
			lang := strings.ToLower(token[1:])
			language = lang
			continue
		}

		// Exclusion term: starts with "-"
		if strings.HasPrefix(token, "-") && len(token) > 1 {
			exTerm := strings.ToLower(token[1:])
			exclusions = append(exclusions, exTerm)
			continue
		}

		// Standard query token
		cleanTokens = append(cleanTokens, token)
	}

	return ParsedQuery{
		CleanQuery:       strings.Join(cleanTokens, " "),
		EngineOverrides:  engines,
		CategoryOverride: category,
		LanguageOverride: language,
		ExcludedTerms:    exclusions,
	}
}
