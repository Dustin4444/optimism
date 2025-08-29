package derive

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-service/eth"
)

// mockL1Fetcher is a simple mock implementation for testing
type mockL1Fetcher struct {
	blockRefs    map[uint64]eth.L1BlockRef
	blockInfos   map[common.Hash]eth.BlockInfo
	receipts     map[common.Hash]types.Receipts
	transactions map[common.Hash]types.Transactions
}

func newMockL1Fetcher() *mockL1Fetcher {
	return &mockL1Fetcher{
		blockRefs:    make(map[uint64]eth.L1BlockRef),
		blockInfos:   make(map[common.Hash]eth.BlockInfo),
		receipts:     make(map[common.Hash]types.Receipts),
		transactions: make(map[common.Hash]types.Transactions),
	}
}

func (m *mockL1Fetcher) L1BlockRefByLabel(ctx context.Context, label eth.BlockLabel) (eth.L1BlockRef, error) {
	// Mock implementation - return a default block ref
	return eth.L1BlockRef{
		Hash:       common.HexToHash("0x123"),
		Number:     100,
		ParentHash: common.HexToHash("0x456"),
		Time:       1000000,
	}, nil
}

func (m *mockL1Fetcher) L1BlockRefByNumber(ctx context.Context, num uint64) (eth.L1BlockRef, error) {
	if ref, exists := m.blockRefs[num]; exists {
		return ref, nil
	}
	// Return a default block ref
	ref := eth.L1BlockRef{
		Hash:       common.HexToHash("0x" + common.Bytes2Hex([]byte{byte(num)})),
		Number:     num,
		ParentHash: common.HexToHash("0x456"),
		Time:       1000000 + num,
	}
	m.blockRefs[num] = ref
	return ref, nil
}

func (m *mockL1Fetcher) L1BlockRefByHash(ctx context.Context, hash common.Hash) (eth.L1BlockRef, error) {
	// Mock implementation - create a block ref from hash
	return eth.L1BlockRef{
		Hash:       hash,
		Number:     100,
		ParentHash: common.HexToHash("0x456"),
		Time:       1000000,
	}, nil
}

func (m *mockL1Fetcher) InfoByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, error) {
	if info, exists := m.blockInfos[hash]; exists {
		return info, nil
	}
	// Return a default block info by creating a mock block
	header := &types.Header{
		ParentHash: common.HexToHash("0x456"),
		Number:     big.NewInt(100),
		Time:       1000000,
		BaseFee:    big.NewInt(1000000000),
		Difficulty: big.NewInt(0),
		Nonce:      types.BlockNonce{},
	}
	// Create a block and use its hash
	block := types.NewBlockWithHeader(header)
	// Use the computed hash instead of the provided hash for consistency
	info := eth.BlockToInfo(block)
	m.blockInfos[block.Hash()] = info
	return info, nil
}

func (m *mockL1Fetcher) FetchReceipts(ctx context.Context, blockHash common.Hash) (eth.BlockInfo, types.Receipts, error) {
	info, err := m.InfoByHash(ctx, blockHash)
	if err != nil {
		return nil, nil, err
	}

	receipts := m.receipts[blockHash]
	if receipts == nil {
		receipts = types.Receipts{}
		m.receipts[blockHash] = receipts
	}

	return info, receipts, nil
}

func (m *mockL1Fetcher) InfoAndTxsByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, types.Transactions, error) {
	info, err := m.InfoByHash(ctx, hash)
	if err != nil {
		return nil, nil, err
	}

	txs := m.transactions[hash]
	if txs == nil {
		txs = types.Transactions{}
		m.transactions[hash] = txs
	}

	return info, txs, nil
}

func TestCachedL1Fetcher(t *testing.T) {
	// Create a temporary test directory
	testDir := filepath.Join("testdata", "chain_1")
	defer os.RemoveAll(testDir)

	logger := log.New()
	mockFetcher := newMockL1Fetcher()

	// Create the cached fetcher
	cachedFetcher := NewCachedL1Fetcher(mockFetcher, logger, 1)

	ctx := context.Background()

	t.Run("L1BlockRefByNumber", func(t *testing.T) {
		// First call should fetch from mock and cache
		ref1, err := cachedFetcher.L1BlockRefByNumber(ctx, 100)
		if err != nil {
			t.Fatalf("Failed to get block ref: %v", err)
		}

		// Second call should return from cache
		ref2, err := cachedFetcher.L1BlockRefByNumber(ctx, 100)
		if err != nil {
			t.Fatalf("Failed to get block ref from cache: %v", err)
		}

		if ref1.Hash != ref2.Hash {
			t.Errorf("Cached result differs from original: %v vs %v", ref1, ref2)
		}

		// Verify cache file exists
		cachePath := filepath.Join(testDir, "chain_1_L1BlockRefByNumber_100.json")
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			t.Errorf("Cache file was not created: %s", cachePath)
		}
	})

	t.Run("L1BlockRefByHash", func(t *testing.T) {
		hash := common.HexToHash("0x123456789")

		// First call should fetch from mock and cache
		ref1, err := cachedFetcher.L1BlockRefByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block ref by hash: %v", err)
		}

		// Second call should return from cache
		ref2, err := cachedFetcher.L1BlockRefByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block ref by hash from cache: %v", err)
		}

		if ref1.Hash != ref2.Hash {
			t.Errorf("Cached result differs from original: %v vs %v", ref1, ref2)
		}
	})

	t.Run("InfoByHash", func(t *testing.T) {
		hash := common.HexToHash("0xabcdef")

		// First call should fetch from mock and cache
		info1, err := cachedFetcher.InfoByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block info: %v", err)
		}

		// Second call should return from cache
		info2, err := cachedFetcher.InfoByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block info from cache: %v", err)
		}

		if info1.Hash() != info2.Hash() {
			t.Errorf("Cached result differs from original: %v vs %v", info1, info2)
		}
	})

	t.Run("FetchReceipts", func(t *testing.T) {
		hash := common.HexToHash("0xreceipts")

		// First call should fetch from mock and cache
		info1, receipts1, err := cachedFetcher.FetchReceipts(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to fetch receipts: %v", err)
		}

		// Second call should return from cache
		info2, receipts2, err := cachedFetcher.FetchReceipts(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to fetch receipts from cache: %v", err)
		}

		if info1.Hash() != info2.Hash() {
			t.Errorf("Cached result differs from original: %v vs %v", info1, info2)
		}

		if len(receipts1) != len(receipts2) {
			t.Errorf("Cached receipts length differs: %d vs %d", len(receipts1), len(receipts2))
		}
	})

	t.Run("InfoAndTxsByHash", func(t *testing.T) {
		hash := common.HexToHash("0xtransactions")

		// First call should fetch from mock and cache
		info1, txs1, err := cachedFetcher.InfoAndTxsByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to fetch info and transactions: %v", err)
		}

		// Second call should return from cache
		info2, txs2, err := cachedFetcher.InfoAndTxsByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to fetch info and transactions from cache: %v", err)
		}

		if info1.Hash() != info2.Hash() {
			t.Errorf("Cached result differs from original: %v vs %v", info1, info2)
		}

		if len(txs1) != len(txs2) {
			t.Errorf("Cached transactions length differs: %d vs %d", len(txs1), len(txs2))
		}
	})
}

func TestCachedL1FetcherCacheKeyGeneration(t *testing.T) {
	logger := log.New()
	mockFetcher := newMockL1Fetcher()

	// Test cache key generation for chain 1
	cachedFetcher1 := NewCachedL1Fetcher(mockFetcher, logger, 1)
	key1 := cachedFetcher1.getCacheKey("L1BlockRefByNumber", uint64(100))
	expected1 := "chain_1_L1BlockRefByNumber_100"
	if key1 != expected1 {
		t.Errorf("Expected cache key %s, got %s", expected1, key1)
	}

	key2 := cachedFetcher1.getCacheKey("L1BlockRefByHash", "0x123456789")
	expected2 := "chain_1_L1BlockRefByHash_0x123456789"
	if key2 != expected2 {
		t.Errorf("Expected cache key %s, got %s", expected2, key2)
	}

	// Test cache key generation for chain 999 (different chain ID)
	cachedFetcher999 := NewCachedL1Fetcher(mockFetcher, logger, 999)
	key3 := cachedFetcher999.getCacheKey("L1BlockRefByNumber", uint64(100))
	expected3 := "chain_999_L1BlockRefByNumber_100"
	if key3 != expected3 {
		t.Errorf("Expected cache key %s, got %s", expected3, key3)
	}

	// Verify that different chain IDs produce different cache keys for the same method and parameters
	if key1 == key3 {
		t.Errorf("Cache keys should be different for different chain IDs: %s vs %s", key1, key3)
	}
}

func TestCachedL1FetcherCacheDirectory(t *testing.T) {
	logger := log.New()
	mockFetcher := newMockL1Fetcher()

	// Test with different chain IDs
	chainID := uint64(999)
	cachedFetcher := NewCachedL1Fetcher(mockFetcher, logger, chainID)

	expectedDir := filepath.Join("testdata", "chain_999")
	if cachedFetcher.cacheDir != expectedDir {
		t.Errorf("Expected cache directory %s, got %s", expectedDir, cachedFetcher.cacheDir)
	}

	// Test directory creation
	if err := cachedFetcher.ensureCacheDir(); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	// Verify directory exists
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Cache directory was not created: %s", expectedDir)
	}

	// Clean up
	os.RemoveAll(expectedDir)
}
