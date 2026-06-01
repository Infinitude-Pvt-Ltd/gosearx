package scoring

import (
	"net/url"
	"sort"
	"strings"
)

// SearchResult represents a unified search result card returned to the API client.
type SearchResult struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	Content  string   `json:"content"`
	Score    float64  `json:"score"`
	Engines  []string `json:"engines"`
	Category string   `json:"category,omitempty"`
}

// CanonicalizeURL normalizes a URL so that identical links from different engines can be deduplicated.
func CanonicalizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		// Fallback to basic string cleanup if URL parsing fails
		return strings.ToLower(strings.TrimSpace(rawURL))
	}

	// Lowercase the host and remove "www." prefix
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")

	// Lowercase the path and clean up trailing slashes/default pages
	path := strings.ToLower(parsed.Path)
	path = strings.TrimSuffix(path, "/")
	if path == "/index.html" || path == "/index.htm" || path == "/index.php" {
		path = ""
	}

	// Keep only non-tracking query parameters
	var cleanQuery []string
	queryParams := parsed.Query()
	for key, values := range queryParams {
		lowerKey := strings.ToLower(key)
		// Strip common tracking and referrer params
		if strings.HasPrefix(lowerKey, "utm_") ||
			lowerKey == "gclid" ||
			lowerKey == "fbclid" ||
			lowerKey == "ref" ||
			lowerKey == "source" {
			continue
		}
		for _, val := range values {
			cleanQuery = append(cleanQuery, url.QueryEscape(key)+"="+url.QueryEscape(val))
		}
	}
	sort.Strings(cleanQuery)

	canonical := host + path
	if len(cleanQuery) > 0 {
		canonical += "?" + strings.Join(cleanQuery, "&")
	}

	return canonical
}

// EngineResult represents a raw result returned from an individual engine with its positional rank.
type EngineResult struct {
	Title    string
	URL      string
	Content  string
	Engine   string
	Category string
	Rank     int // 1-based index representing the order the engine returned this result
	Weight   float64
}

// AggregateAndScore deduplicates and scores results from multiple engines using SearXNG positional logic, and prunes excluded terms.
func AggregateAndScore(rawResults []EngineResult, excludedTerms []string) []SearchResult {
	merged := make(map[string]*SearchResult)

	for _, item := range rawResults {
		if item.URL == "" {
			continue
		}

		// Perform case-insensitive check against excluded terms
		hasExcluded := false
		lowerTitle := strings.ToLower(item.Title)
		lowerContent := strings.ToLower(item.Content)
		for _, term := range excludedTerms {
			if term != "" && (strings.Contains(lowerTitle, term) || strings.Contains(lowerContent, term)) {
				hasExcluded = true
				break
			}
		}
		if hasExcluded {
			continue
		}

		canonical := CanonicalizeURL(item.URL)

		// Calculate the score contribution for this engine result
		// Formula: weight / rank (where rank is 1-based position)
		if item.Rank <= 0 {
			item.Rank = 1
		}
		if item.Weight <= 0 {
			item.Weight = 1.0
		}
		contribution := item.Weight / float64(item.Rank)

		existing, found := merged[canonical]
		if found {
			// Update existing entry
			existing.Score += contribution
			
			// Append engine to source engines if not already listed
			engineFound := false
			for _, eng := range existing.Engines {
				if eng == item.Engine {
					engineFound = true
					break
				}
			}
			if !engineFound {
				existing.Engines = append(existing.Engines, item.Engine)
			}

			// Keep the longer title as it is usually more descriptive
			if len(item.Title) > len(existing.Title) {
				existing.Title = item.Title
			}

			// Keep the longer snippet
			if len(item.Content) > len(existing.Content) {
				existing.Content = item.Content
			}
		} else {
			// Create a new entry
			merged[canonical] = &SearchResult{
				Title:    item.Title,
				URL:      item.URL,
				Content:  item.Content,
				Score:    contribution,
				Engines:  []string{item.Engine},
				Category: item.Category,
			}
		}
	}

	// Convert map to slice
	results := make([]SearchResult, 0, len(merged))
	for _, res := range merged {
		results = append(results, *res)
	}

	// Sort by Google priority first, then by score in descending order
	sort.Slice(results, func(i, j int) bool {
		iHasGoogle := false
		for _, eng := range results[i].Engines {
			if eng == "google" {
				iHasGoogle = true
				break
			}
		}
		jHasGoogle := false
		for _, eng := range results[j].Engines {
			if eng == "google" {
				jHasGoogle = true
				break
			}
		}

		// Google results come first
		if iHasGoogle && !jHasGoogle {
			return true
		}
		if !iHasGoogle && jHasGoogle {
			return false
		}

		// If both have Google, or both do not, sort by score descending
		if results[i].Score == results[j].Score {
			return results[i].Title < results[j].Title
		}
		return results[i].Score > results[j].Score
	})

	return results
}
