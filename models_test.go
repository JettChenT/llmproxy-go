package main

import (
	"fmt"
	"testing"
)

func TestSuffixMatching(t *testing.T) {
	// Load the models database first
	if err := fetchModelsDB(); err != nil {
		t.Fatalf("Failed to fetch models DB: %v", err)
	}

	testCases := []string{
		"claude-sonnet-4-20250514-v1:0",
		"us.anthropic.claude-sonnet-4-20250514-v1:0",
	}

	for _, modelSlug := range testCases {
		// Try to find it
		cost := GetModelCost("", modelSlug)

		if cost != nil {
			fmt.Printf("✓ Found cost for %q:\n", modelSlug)
			fmt.Printf("  Input:  $%.2f / 1M tokens\n", cost.Input)
			fmt.Printf("  Output: $%.2f / 1M tokens\n", cost.Output)
		} else {
			t.Errorf("✗ No cost found for %q", modelSlug)

			// Debug: print all models that contain "claude-sonnet-4"
			fmt.Println("\nModels containing 'claude-sonnet-4':")
			modelsDB.mu.RLock()
			for slug := range modelsDB.globalModels {
				if contains(slug, "claude-sonnet-4") {
					fmt.Printf("  - %s\n", slug)
				}
			}
			modelsDB.mu.RUnlock()
		}
		fmt.Println()
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
