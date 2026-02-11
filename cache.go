package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// CacheMode represents the caching mode
type CacheMode string

const (
	CacheModeNone   CacheMode = "none"
	CacheModeMemory CacheMode = "memory"
	CacheModeGlobal CacheMode = "global"
)

// CacheEntry represents a cached response
type CacheEntry struct {
	ResponseBody    []byte              `json:"response_body"`
	ResponseHeaders map[string][]string `json:"response_headers"`
	StatusCode      int                 `json:"status_code"`
	Duration        time.Duration       `json:"duration"` // Original response duration for latency simulation
	CreatedAt       time.Time           `json:"created_at"`
}

// Cache is the interface for request/response caching
type Cache interface {
	// Get retrieves a cached response for the given key
	Get(key string) (*CacheEntry, bool)
	// Set stores a response in the cache
	Set(key string, entry *CacheEntry) error
	// Close cleans up cache resources
	Close() error
}

// GenerateCacheKey creates a cache key from the request
// It hashes the request body to create a unique identifier
func GenerateCacheKey(path string, requestBody []byte) string {
	// Use Anthropic-specific normalization for /v1/messages endpoints
	if isAnthropicEndpoint(path) {
		var req AnthropicRequest
		if err := json.Unmarshal(requestBody, &req); err != nil {
			hash := sha256.Sum256(requestBody)
			return path + ":" + hex.EncodeToString(hash[:])
		}
		normalized := struct {
			Model     string             `json:"model"`
			Messages  []AnthropicMessage `json:"messages"`
			System    interface{}        `json:"system,omitempty"`
			MaxTokens int                `json:"max_tokens"`
			Stream    bool               `json:"stream"`
		}{
			Model:     req.Model,
			Messages:  req.Messages,
			System:    req.System,
			MaxTokens: req.MaxTokens,
			Stream:    req.Stream,
		}
		data, _ := json.Marshal(normalized)
		hash := sha256.Sum256(data)
		return path + ":" + hex.EncodeToString(hash[:])
	}

	// Default: OpenAI format normalization
	var req OpenAIRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		hash := sha256.Sum256(requestBody)
		return path + ":" + hex.EncodeToString(hash[:])
	}

	normalized := struct {
		Model       string          `json:"model"`
		Messages    []OpenAIMessage `json:"messages"`
		Temperature float64         `json:"temperature"`
		MaxTokens   int             `json:"max_tokens"`
		Stream      bool            `json:"stream"`
	}{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
	}

	data, _ := json.Marshal(normalized)
	hash := sha256.Sum256(data)
	return path + ":" + hex.EncodeToString(hash[:])
}

// --- NoopCache: Does nothing, used when caching is disabled ---

type NoopCache struct{}

func NewNoopCache() *NoopCache {
	return &NoopCache{}
}

func (c *NoopCache) Get(key string) (*CacheEntry, bool) {
	return nil, false
}

func (c *NoopCache) Set(key string, entry *CacheEntry) error {
	return nil
}

func (c *NoopCache) Close() error {
	return nil
}

// --- MemoryCache: In-memory cache with TTL ---

type memoryCacheEntry struct {
	entry     *CacheEntry
	expiresAt time.Time
}

type MemoryCache struct {
	data     sync.Map
	ttl      time.Duration
	stopChan chan struct{}
}

func NewMemoryCache(ttl time.Duration) *MemoryCache {
	c := &MemoryCache{
		ttl:      ttl,
		stopChan: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go c.cleanup()

	return c
}

func (c *MemoryCache) Get(key string) (*CacheEntry, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return nil, false
	}

	entry := val.(*memoryCacheEntry)
	if time.Now().After(entry.expiresAt) {
		// Entry expired, delete and return miss
		c.data.Delete(key)
		return nil, false
	}

	return entry.entry, true
}

func (c *MemoryCache) Set(key string, entry *CacheEntry) error {
	c.data.Store(key, &memoryCacheEntry{
		entry:     entry,
		expiresAt: time.Now().Add(c.ttl),
	})
	return nil
}

func (c *MemoryCache) Close() error {
	close(c.stopChan)
	return nil
}

// cleanup periodically removes expired entries
func (c *MemoryCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			c.data.Range(func(key, value interface{}) bool {
				entry := value.(*memoryCacheEntry)
				if now.After(entry.expiresAt) {
					c.data.Delete(key)
				}
				return true
			})
		case <-c.stopChan:
			return
		}
	}
}

// --- BadgerCache: Persistent cache using BadgerDB ---

type BadgerCache struct {
	db  *badger.DB
	ttl time.Duration
}

func NewBadgerCache(path string, ttl time.Duration) (*BadgerCache, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil // Disable badger's default logging
	opts.SyncWrites = false // Async writes for performance
	opts.NumVersionsToKeep = 1

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	c := &BadgerCache{
		db:  db,
		ttl: ttl,
	}

	// Start background garbage collection
	go c.runGC()

	return c, nil
}

func (c *BadgerCache) Get(key string) (*CacheEntry, bool) {
	var entry CacheEntry

	err := c.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &entry)
		})
	})

	if err != nil {
		return nil, false
	}

	return &entry, true
}

func (c *BadgerCache) Set(key string, entry *CacheEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return c.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), data).WithTTL(c.ttl)
		return txn.SetEntry(e)
	})
}

func (c *BadgerCache) Close() error {
	return c.db.Close()
}

// runGC periodically runs garbage collection on the database
func (c *BadgerCache) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		// Run value log GC until no more rewrite occurs
		for {
			err := c.db.RunValueLogGC(0.5)
			if err != nil {
				break
			}
		}
	}
}

// --- Cache configuration and initialization ---

type CacheConfig struct {
	Mode            CacheMode
	TTL             time.Duration
	SimulateLatency bool
	BadgerPath      string
}

var (
	globalCache Cache
	cacheConfig CacheConfig
)

// InitCache initializes the global cache based on configuration
func InitCache(config CacheConfig) error {
	cacheConfig = config

	switch config.Mode {
	case CacheModeNone:
		globalCache = NewNoopCache()
		log.Printf("Cache: disabled")
	case CacheModeMemory:
		globalCache = NewMemoryCache(config.TTL)
		log.Printf("Cache: in-memory (TTL: %v, simulate latency: %v)", config.TTL, config.SimulateLatency)
	case CacheModeGlobal:
		var err error
		cache, err := NewBadgerCache(config.BadgerPath, config.TTL)
		if err != nil {
			return err
		}
		globalCache = cache
		log.Printf("Cache: global @ %s (TTL: %v, simulate latency: %v)", config.BadgerPath, config.TTL, config.SimulateLatency)
	default:
		globalCache = NewNoopCache()
	}

	return nil
}

// GetCache returns the global cache instance
func GetCache() Cache {
	if globalCache == nil {
		return NewNoopCache()
	}
	return globalCache
}

// GetCacheConfig returns the cache configuration
func GetCacheConfig() CacheConfig {
	return cacheConfig
}

// CloseCache closes the global cache
func CloseCache() error {
	if globalCache != nil {
		return globalCache.Close()
	}
	return nil
}

