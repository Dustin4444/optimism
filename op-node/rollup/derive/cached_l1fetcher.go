package derive

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-service/eth"
)

// CachedL1Fetcher wraps an L1Fetcher and caches responses locally in /testdata
// for a given ChainID. It stores responses as JSON files and serves them from cache
// if available, otherwise delegates to the underlying fetcher.
type CachedL1Fetcher struct {
	inner    L1Fetcher
	log      log.Logger
	chainID  uint64
	cacheDir string
	mu       sync.RWMutex
}

var _ L1Fetcher = (*CachedL1Fetcher)(nil)

// NewCachedL1Fetcher creates a new CachedL1Fetcher that wraps the given L1Fetcher
// and caches responses in /testdata for the specified chainID.
func NewCachedL1Fetcher(inner L1Fetcher, log log.Logger, chainID uint64) *CachedL1Fetcher {
	cacheDir := filepath.Join("testdata", fmt.Sprintf("chain_%d", chainID))
	return &CachedL1Fetcher{
		inner:    inner,
		log:      log,
		chainID:  chainID,
		cacheDir: cacheDir,
	}
}

// ensureCacheDir ensures the cache directory exists
func (c *CachedL1Fetcher) ensureCacheDir() error {
	return os.MkdirAll(c.cacheDir, 0755)
}

// getCacheKey generates a cache key for the given method and parameters
// The chainID is included in the key to ensure responses from different chains don't interfere
func (c *CachedL1Fetcher) getCacheKey(method string, params ...interface{}) string {
	key := fmt.Sprintf("chain_%d_%s", c.chainID, method)
	for _, param := range params {
		key += "_" + fmt.Sprintf("%v", param)
	}
	return key
}

// getCachePath returns the full path for a cache file
func (c *CachedL1Fetcher) getCachePath(key string) string {
	return filepath.Join(c.cacheDir, key+".json")
}

// loadFromCache attempts to load a cached response
func (c *CachedL1Fetcher) loadFromCache(key string, result interface{}) (bool, error) {
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
func (c *CachedL1Fetcher) saveToCache(key string, data interface{}) error {
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

// L1BlockRefByLabel implements L1Fetcher.L1BlockRefByLabel
func (c *CachedL1Fetcher) L1BlockRefByLabel(ctx context.Context, label eth.BlockLabel) (eth.L1BlockRef, error) {
	key := c.getCacheKey("L1BlockRefByLabel", label)

	var result eth.L1BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner fetcher
	result, err := c.inner.L1BlockRefByLabel(ctx, label)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// L1BlockRefByNumber implements L1Fetcher.L1BlockRefByNumber
func (c *CachedL1Fetcher) L1BlockRefByNumber(ctx context.Context, num uint64) (eth.L1BlockRef, error) {
	key := c.getCacheKey("L1BlockRefByNumber", num)

	var result eth.L1BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner fetcher
	result, err := c.inner.L1BlockRefByNumber(ctx, num)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// L1BlockRefByHash implements L1Fetcher.L1BlockRefByHash
func (c *CachedL1Fetcher) L1BlockRefByHash(ctx context.Context, hash common.Hash) (eth.L1BlockRef, error) {
	key := c.getCacheKey("L1BlockRefByHash", hash.Hex())

	var result eth.L1BlockRef
	if cached, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if cached {
		return result, nil
	}

	// Fetch from inner fetcher
	result, err := c.inner.L1BlockRefByHash(ctx, hash)
	if err != nil {
		return result, err
	}

	// Cache the result
	if err := c.saveToCache(key, result); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// InfoByHash implements L1Fetcher.InfoByHash
func (c *CachedL1Fetcher) InfoByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, error) {
	key := c.getCacheKey("InfoByHash", hash.Hex())

	// Try to load from cache - we cache the header data and reconstruct BlockInfo
	type cachedHeader struct {
		Hash   common.Hash   `json:"hash"`
		Header *types.Header `json:"header"`
	}

	var cached cachedHeader
	if found, err := c.loadFromCache(key, &cached); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if found && cached.Hash != (common.Hash{}) {
		// Reconstruct BlockInfo from cached header
		return eth.HeaderBlockInfoTrusted(cached.Hash, cached.Header), nil
	}

	// Fetch from inner fetcher
	result, err := c.inner.InfoByHash(ctx, hash)
	if err != nil {
		return result, err
	}

	// Cache the header data for reconstruction
	cacheData := cachedHeader{
		Hash: result.Hash(),
		Header: &types.Header{
			ParentHash:       result.ParentHash(),
			UncleHash:        types.EmptyUncleHash,
			Coinbase:         result.Coinbase(),
			Root:             result.Root(),
			TxHash:           types.EmptyRootHash,
			ReceiptHash:      result.ReceiptHash(),
			Bloom:            types.Bloom{},
			Difficulty:       big.NewInt(0),
			Number:           big.NewInt(int64(result.NumberU64())),
			GasLimit:         result.GasLimit(),
			GasUsed:          result.GasUsed(),
			Time:             result.Time(),
			Extra:            []byte{},
			MixDigest:        result.MixDigest(),
			Nonce:            types.BlockNonce{},
			BaseFee:          result.BaseFee(),
			WithdrawalsHash:  result.WithdrawalsRoot(),
			BlobGasUsed:      nil,
			ExcessBlobGas:    result.ExcessBlobGas(),
			ParentBeaconRoot: result.ParentBeaconRoot(),
		},
	}

	if err := c.saveToCache(key, cacheData); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return result, nil
}

// FetchReceipts implements L1Fetcher.FetchReceipts
func (c *CachedL1Fetcher) FetchReceipts(ctx context.Context, blockHash common.Hash) (eth.BlockInfo, types.Receipts, error) {
	key := c.getCacheKey("FetchReceipts", blockHash.Hex())

	type cachedResult struct {
		Hash     common.Hash    `json:"hash"`
		Header   *types.Header  `json:"header"`
		Receipts types.Receipts `json:"receipts"`
	}

	var result cachedResult
	if found, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if found && result.Hash != (common.Hash{}) {
		// Reconstruct BlockInfo from cached header
		blockInfo := eth.HeaderBlockInfoTrusted(result.Hash, result.Header)
		return blockInfo, result.Receipts, nil
	}

	// Fetch from inner fetcher
	blockInfo, receipts, err := c.inner.FetchReceipts(ctx, blockHash)
	if err != nil {
		return blockInfo, receipts, err
	}

	// Cache the header data and receipts for reconstruction
	cacheData := cachedResult{
		Hash: blockInfo.Hash(),
		Header: &types.Header{
			ParentHash:       blockInfo.ParentHash(),
			UncleHash:        types.EmptyUncleHash,
			Coinbase:         blockInfo.Coinbase(),
			Root:             blockInfo.Root(),
			TxHash:           types.EmptyRootHash,
			ReceiptHash:      blockInfo.ReceiptHash(),
			Bloom:            types.Bloom{},
			Difficulty:       big.NewInt(0),
			Number:           big.NewInt(int64(blockInfo.NumberU64())),
			GasLimit:         blockInfo.GasLimit(),
			GasUsed:          blockInfo.GasUsed(),
			Time:             blockInfo.Time(),
			Extra:            []byte{},
			MixDigest:        blockInfo.MixDigest(),
			Nonce:            types.BlockNonce{},
			BaseFee:          blockInfo.BaseFee(),
			WithdrawalsHash:  blockInfo.WithdrawalsRoot(),
			BlobGasUsed:      nil,
			ExcessBlobGas:    blockInfo.ExcessBlobGas(),
			ParentBeaconRoot: blockInfo.ParentBeaconRoot(),
		},
		Receipts: receipts,
	}
	if err := c.saveToCache(key, cacheData); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return blockInfo, receipts, nil
}

// InfoAndTxsByHash implements L1Fetcher.InfoAndTxsByHash
func (c *CachedL1Fetcher) InfoAndTxsByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, types.Transactions, error) {
	key := c.getCacheKey("InfoAndTxsByHash", hash.Hex())

	type cachedResult struct {
		Hash   common.Hash        `json:"hash"`
		Header *types.Header      `json:"header"`
		Txs    types.Transactions `json:"transactions"`
	}

	var result cachedResult
	if found, err := c.loadFromCache(key, &result); err != nil {
		c.log.Warn("Failed to load from cache, falling back to inner fetcher", "key", key, "err", err)
	} else if found && result.Hash != (common.Hash{}) {
		// Reconstruct BlockInfo from cached header
		blockInfo := eth.HeaderBlockInfoTrusted(result.Hash, result.Header)
		return blockInfo, result.Txs, nil
	}

	// Fetch from inner fetcher
	blockInfo, txs, err := c.inner.InfoAndTxsByHash(ctx, hash)
	if err != nil {
		return blockInfo, txs, err
	}

	// Cache the header data and transactions for reconstruction
	cacheData := cachedResult{
		Hash: blockInfo.Hash(),
		Header: &types.Header{
			ParentHash:       blockInfo.ParentHash(),
			UncleHash:        types.EmptyUncleHash,
			Coinbase:         blockInfo.Coinbase(),
			Root:             blockInfo.Root(),
			TxHash:           types.EmptyRootHash,
			ReceiptHash:      blockInfo.ReceiptHash(),
			Bloom:            types.Bloom{},
			Difficulty:       big.NewInt(0),
			Number:           big.NewInt(int64(blockInfo.NumberU64())),
			GasLimit:         blockInfo.GasLimit(),
			GasUsed:          blockInfo.GasUsed(),
			Time:             blockInfo.Time(),
			Extra:            []byte{},
			MixDigest:        blockInfo.MixDigest(),
			Nonce:            types.BlockNonce{},
			BaseFee:          blockInfo.BaseFee(),
			WithdrawalsHash:  blockInfo.WithdrawalsRoot(),
			BlobGasUsed:      nil,
			ExcessBlobGas:    blockInfo.ExcessBlobGas(),
			ParentBeaconRoot: blockInfo.ParentBeaconRoot(),
		},
		Txs: txs,
	}
	if err := c.saveToCache(key, cacheData); err != nil {
		c.log.Warn("Failed to save to cache", "key", key, "err", err)
	}

	return blockInfo, txs, nil
}
