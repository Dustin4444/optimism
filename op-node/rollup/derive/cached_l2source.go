package derive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-service/eth"
)

// CachedL2Source wraps an L2Source and caches responses locally in /testdata
// for a given ChainID. It stores responses as JSON files and serves them from cache
// if available, otherwise delegates to the underlying source.
type CachedL2Source struct {
	inner    L2Source
	log      log.Logger
	chainID  uint64
	cacheDir string
	mu       sync.RWMutex
}

var _ L2Source = (*CachedL2Source)(nil)

// NewCachedL2Source creates a new CachedL2Source that wraps the given L2Source
// and caches responses in /testdata for the specified chainID.
func NewCachedL2Source(inner L2Source, log log.Logger, chainID uint64) *CachedL2Source {
	cacheDir := filepath.Join("testdata", fmt.Sprintf("chain_%d", chainID))
	return &CachedL2Source{
		inner:    inner,
		log:      log,
		chainID:  chainID,
		cacheDir: cacheDir,
	}
}

// ensureCacheDir ensures the cache directory exists
func (c *CachedL2Source) ensureCacheDir() error {
	return os.MkdirAll(c.cacheDir, 0755)
}

// getCacheKey generates a cache key for the given method and parameters
// The chainID is already included in the cache directory, so we don't need it in the key
func (c *CachedL2Source) getCacheKey(method string, params ...interface{}) string {
	key := method
	for _, param := range params {
		key += "_" + fmt.Sprintf("%v", param)
	}
	return key
}

// getCachePath returns the full path for a cache file
func (c *CachedL2Source) getCachePath(key string) string {
	return filepath.Join(c.cacheDir, key+".json")
}

// loadFromCache attempts to load a cached response
func (c *CachedL2Source) loadFromCache(key string, result interface{}) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.getCachePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Not cached
		}
		return false, fmt.Errorf("failed to read cache file %s: %w", path, err)
	}

	if err := json.Unmarshal(data, result); err != nil {
		return false, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	c.log.Debug("Loaded from cache", "key", key, "path", path)
	return true, nil
}

// saveToCache saves a response to cache
func (c *CachedL2Source) saveToCache(key string, data interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureCacheDir(); err != nil {
		return fmt.Errorf("failed to ensure cache directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data for cache: %w", err)
	}

	path := c.getCachePath(key)
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write cache file %s: %w", path, err)
	}

	c.log.Debug("Saved to cache", "key", key, "path", path)
	return nil
}

// PayloadByHash implements L2Source.PayloadByHash
func (c *CachedL2Source) PayloadByHash(ctx context.Context, hash common.Hash) (*eth.ExecutionPayloadEnvelope, error) {
	key := c.getCacheKey("PayloadByHash", hash.Hex())

	var result *eth.ExecutionPayloadEnvelope
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.PayloadByHash(ctx, hash)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// PayloadByNumber implements L2Source.PayloadByNumber
func (c *CachedL2Source) PayloadByNumber(ctx context.Context, num uint64) (*eth.ExecutionPayloadEnvelope, error) {
	key := c.getCacheKey("PayloadByNumber", num)

	var result *eth.ExecutionPayloadEnvelope
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.PayloadByNumber(ctx, num)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// L2BlockRefByLabel implements L2Source.L2BlockRefByLabel
func (c *CachedL2Source) L2BlockRefByLabel(ctx context.Context, label eth.BlockLabel) (eth.L2BlockRef, error) {
	key := c.getCacheKey("L2BlockRefByLabel", label)

	var result eth.L2BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.L2BlockRefByLabel(ctx, label)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// L2BlockRefByHash implements L2Source.L2BlockRefByHash
func (c *CachedL2Source) L2BlockRefByHash(ctx context.Context, l2Hash common.Hash) (eth.L2BlockRef, error) {
	key := c.getCacheKey("L2BlockRefByHash", l2Hash.Hex())

	var result eth.L2BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.L2BlockRefByHash(ctx, l2Hash)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// L2BlockRefByNumber implements L2Source.L2BlockRefByNumber
func (c *CachedL2Source) L2BlockRefByNumber(ctx context.Context, num uint64) (eth.L2BlockRef, error) {
	key := c.getCacheKey("L2BlockRefByNumber", num)

	var result eth.L2BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.L2BlockRefByNumber(ctx, num)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// SystemConfigByL2Hash implements SystemConfigL2Fetcher.SystemConfigByL2Hash
func (c *CachedL2Source) SystemConfigByL2Hash(ctx context.Context, hash common.Hash) (eth.SystemConfig, error) {
	key := c.getCacheKey("SystemConfigByL2Hash", hash.Hex())

	var result eth.SystemConfig
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner source", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner source
	result, err := c.inner.SystemConfigByL2Hash(ctx, hash)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}
