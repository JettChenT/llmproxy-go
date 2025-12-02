package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Models.dev types
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning,omitempty"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

type ModelInfo struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Cost ModelCost `json:"cost"`
}

type Provider struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	API    string               `json:"api,omitempty"`
	NPM    string               `json:"npm"`
	Models map[string]ModelInfo `json:"models"`
}

// ModelsDB holds the models.dev data
type ModelsDB struct {
	mu           sync.RWMutex
	providers    map[string]Provider
	globalModels map[string]ModelCost // Global slug -> cost mapping for fallback
	loaded       bool
	lastFetch    time.Time
}

var modelsDB = &ModelsDB{
	providers:    make(map[string]Provider),
	globalModels: make(map[string]ModelCost),
}

const modelsDevURL = "https://models.dev/api.json"

// LoadModelsDB fetches models.dev data
func LoadModelsDB() {
	go func() {
		if err := fetchModelsDB(); err != nil {
			// Silently fail - cost calculation is optional
			return
		}
	}()
}

func fetchModelsDB() error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var providers map[string]Provider
	if err := json.Unmarshal(body, &providers); err != nil {
		return err
	}

	// Build global slug -> cost mapping for fallback lookups
	globalModels := make(map[string]ModelCost)
	for _, provider := range providers {
		for slug, model := range provider.Models {
			// Only add if not already present (first provider wins)
			// or if the new cost is non-zero and existing is zero
			if existing, ok := globalModels[slug]; !ok || (existing.Input == 0 && model.Cost.Input > 0) {
				globalModels[slug] = model.Cost
			}
		}
	}

	modelsDB.mu.Lock()
	modelsDB.providers = providers
	modelsDB.globalModels = globalModels
	modelsDB.loaded = true
	modelsDB.lastFetch = time.Now()
	modelsDB.mu.Unlock()

	return nil
}

// FindProviderByURL finds a provider by matching the routed URL against API endpoints
func FindProviderByURL(routedURL string) (string, *Provider) {
	modelsDB.mu.RLock()
	defer modelsDB.mu.RUnlock()

	if !modelsDB.loaded {
		return "", nil
	}

	// Normalize the URL for matching
	routedURL = strings.TrimSuffix(routedURL, "/")

	// First, try exact API prefix matches
	for id, provider := range modelsDB.providers {
		if provider.API != "" {
			apiBase := strings.TrimSuffix(provider.API, "/")
			if strings.HasPrefix(routedURL, apiBase) {
				return id, &provider
			}
		}
	}

	// Fallback: try to match by host name patterns
	lowerURL := strings.ToLower(routedURL)

	// Common provider patterns
	providerPatterns := map[string][]string{
		"openai":     {"api.openai.com"},
		"anthropic":  {"api.anthropic.com"},
		"google":     {"generativelanguage.googleapis.com"},
		"openrouter": {"openrouter.ai"},
		"groq":       {"api.groq.com"},
		"together":   {"api.together.xyz", "together.ai"},
		"fireworks":  {"fireworks.ai"},
		"mistral":    {"api.mistral.ai"},
		"cohere":     {"api.cohere.ai", "cohere.ai"},
		"deepseek":   {"api.deepseek.com"},
		"xai":        {"api.x.ai"},
	}

	for providerID, patterns := range providerPatterns {
		for _, pattern := range patterns {
			if strings.Contains(lowerURL, pattern) {
				if provider, ok := modelsDB.providers[providerID]; ok {
					return providerID, &provider
				}
			}
		}
	}

	return "", nil
}

// GetModelCost retrieves the cost info for a specific model
func GetModelCost(providerID, modelSlug string) *ModelCost {
	modelsDB.mu.RLock()
	defer modelsDB.mu.RUnlock()

	if !modelsDB.loaded {
		return nil
	}

	// Try provider-specific lookup first if providerID is available
	if providerID != "" {
		if provider, ok := modelsDB.providers[providerID]; ok {
			// Try exact match first
			if model, ok := provider.Models[modelSlug]; ok {
				return &model.Cost
			}

			// Try without provider prefix (e.g., "openai/gpt-4" -> "gpt-4")
			if idx := strings.Index(modelSlug, "/"); idx != -1 {
				shortSlug := modelSlug[idx+1:]
				if model, ok := provider.Models[shortSlug]; ok {
					return &model.Cost
				}
			}

			// Try with common variations
			variations := []string{
				strings.ReplaceAll(modelSlug, ".", "-"),
				strings.ReplaceAll(modelSlug, "-", "."),
				strings.ToLower(modelSlug),
			}

			for _, variant := range variations {
				if model, ok := provider.Models[variant]; ok {
					return &model.Cost
				}
			}
		}
	}

	// Fallback: use global slug -> cost mapping
	return lookupGlobalModelCost(modelSlug)
}

// lookupGlobalModelCost searches the global model mapping for cost info
func lookupGlobalModelCost(modelSlug string) *ModelCost {
	// Try exact match first
	if cost, ok := modelsDB.globalModels[modelSlug]; ok {
		return &cost
	}

	// Try without provider prefix (e.g., "openai/gpt-4" -> "gpt-4")
	if idx := strings.Index(modelSlug, "/"); idx != -1 {
		shortSlug := modelSlug[idx+1:]
		if cost, ok := modelsDB.globalModels[shortSlug]; ok {
			return &cost
		}
	}

	// Try with common variations
	variations := []string{
		strings.ReplaceAll(modelSlug, ".", "-"),
		strings.ReplaceAll(modelSlug, "-", "."),
		strings.ToLower(modelSlug),
	}

	for _, variant := range variations {
		if cost, ok := modelsDB.globalModels[variant]; ok {
			return &cost
		}
	}

	// Try suffix matching in both directions:
	// 1. modelSlug is suffix of dbSlug (e.g., "claude-sonnet-4-20250514-v1:0" matches "anthropic.claude-sonnet-4-20250514-v1:0")
	// 2. dbSlug is suffix of modelSlug (e.g., "us.anthropic.claude-sonnet-4-20250514-v1:0" matches "anthropic.claude-sonnet-4-20250514-v1:0")
	suffix := "." + modelSlug
	for dbSlug, cost := range modelsDB.globalModels {
		if strings.HasSuffix(dbSlug, suffix) || strings.HasSuffix(modelSlug, "."+dbSlug) {
			return &cost
		}
	}

	return nil
}

// CalculateCost calculates the cost based on token usage and model pricing
// Costs are in USD per million tokens, so we divide by 1,000,000
func CalculateCost(cost *ModelCost, inputTokens, outputTokens int) float64 {
	if cost == nil {
		return 0
	}

	inputCost := (float64(inputTokens) / 1_000_000) * cost.Input
	outputCost := (float64(outputTokens) / 1_000_000) * cost.Output

	return inputCost + outputCost
}

// EstimateInputTokens estimates token count from string length
// Rule of thumb: ~4 characters per token for English text
func EstimateInputTokens(content string) int {
	if len(content) == 0 {
		return 0
	}
	return len(content) / 4
}

// IsModelsDBLoaded returns whether the models database is loaded
func IsModelsDBLoaded() bool {
	modelsDB.mu.RLock()
	defer modelsDB.mu.RUnlock()
	return modelsDB.loaded
}
