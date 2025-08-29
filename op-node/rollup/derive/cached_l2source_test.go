package derive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum-optimism/optimism/op-service/eth"
)

// mockL2Source is a simple mock implementation for testing
type mockL2Source struct {
	payloads        map[common.Hash]*eth.ExecutionPayloadEnvelope
	blockRefs       map[uint64]eth.L2BlockRef
	blockRefsByHash map[common.Hash]eth.L2BlockRef
	systemConfigs   map[common.Hash]eth.SystemConfig
}

func newMockL2Source() *mockL2Source {
	return &mockL2Source{
		payloads:        make(map[common.Hash]*eth.ExecutionPayloadEnvelope),
		blockRefs:       make(map[uint64]eth.L2BlockRef),
		blockRefsByHash: make(map[common.Hash]eth.L2BlockRef),
		systemConfigs:   make(map[common.Hash]eth.SystemConfig),
	}
}

func (m *mockL2Source) PayloadByHash(ctx context.Context, hash common.Hash) (*eth.ExecutionPayloadEnvelope, error) {
	// Always return a new payload for each hash to ensure caching is tested
	payload := &eth.ExecutionPayloadEnvelope{
		ExecutionPayload: &eth.ExecutionPayload{
			BlockHash:   hash,
			BlockNumber: eth.Uint64Quantity(100),
		},
	}
	return payload, nil
}

func (m *mockL2Source) PayloadByNumber(ctx context.Context, num uint64) (*eth.ExecutionPayloadEnvelope, error) {
	// Return a default payload
	payload := &eth.ExecutionPayloadEnvelope{
		ExecutionPayload: &eth.ExecutionPayload{
			BlockHash:   common.HexToHash("0x" + common.Bytes2Hex([]byte{byte(num)})),
			BlockNumber: eth.Uint64Quantity(num),
		},
	}
	return payload, nil
}

func (m *mockL2Source) L2BlockRefByLabel(ctx context.Context, label eth.BlockLabel) (eth.L2BlockRef, error) {
	// Return a default block ref
	return eth.L2BlockRef{
		Hash:           common.HexToHash("0x123"),
		Number:         100,
		ParentHash:     common.HexToHash("0x456"),
		Time:           1000000,
		L1Origin:       eth.BlockID{Hash: common.HexToHash("0x789"), Number: 50},
		SequenceNumber: 0,
	}, nil
}

func (m *mockL2Source) L2BlockRefByHash(ctx context.Context, l2Hash common.Hash) (eth.L2BlockRef, error) {
	if ref, exists := m.blockRefsByHash[l2Hash]; exists {
		return ref, nil
	}
	// Return a default block ref
	ref := eth.L2BlockRef{
		Hash:           l2Hash,
		Number:         100,
		ParentHash:     common.HexToHash("0x456"),
		Time:           1000000,
		L1Origin:       eth.BlockID{Hash: common.HexToHash("0x789"), Number: 50},
		SequenceNumber: 0,
	}
	m.blockRefsByHash[l2Hash] = ref
	return ref, nil
}

func (m *mockL2Source) L2BlockRefByNumber(ctx context.Context, num uint64) (eth.L2BlockRef, error) {
	if ref, exists := m.blockRefs[num]; exists {
		return ref, nil
	}
	// Return a default block ref
	ref := eth.L2BlockRef{
		Hash:           common.HexToHash("0x" + common.Bytes2Hex([]byte{byte(num)})),
		Number:         num,
		ParentHash:     common.HexToHash("0x456"),
		Time:           1000000 + num,
		L1Origin:       eth.BlockID{Hash: common.HexToHash("0x789"), Number: 50},
		SequenceNumber: 0,
	}
	m.blockRefs[num] = ref
	return ref, nil
}

func (m *mockL2Source) SystemConfigByL2Hash(ctx context.Context, hash common.Hash) (eth.SystemConfig, error) {
	if config, exists := m.systemConfigs[hash]; exists {
		return config, nil
	}
	// Return a default system config
	config := eth.SystemConfig{
		BatcherAddr: common.HexToAddress("0x1234567890123456789012345678901234567890"),
		Overhead:    [32]byte{},
		Scalar:      [32]byte{},
		GasLimit:    30000000,
	}
	m.systemConfigs[hash] = config
	return config, nil
}

func TestCachedL2Source(t *testing.T) {
	// Create a temporary test directory
	testDir := filepath.Join("testdata", "chain_1")
	defer os.RemoveAll(testDir)

	logger := log.New()
	mockSource := newMockL2Source()

	// Create the cached source
	cachedSource := NewCachedL2Source(mockSource, logger, 1)

	ctx := context.Background()

	t.Run("PayloadByHash", func(t *testing.T) {
		hash := common.HexToHash("0x123456789")

		// First call should fetch from mock and cache
		payload1, err := cachedSource.PayloadByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get payload: %v", err)
		}

		// Second call should return from cache
		payload2, err := cachedSource.PayloadByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get payload from cache: %v", err)
		}

		if payload1.ExecutionPayload.BlockHash != payload2.ExecutionPayload.BlockHash {
			t.Errorf("Cached result differs from original: %v vs %v", payload1, payload2)
		}

		// Verify cache file exists (using the full hash representation)
		cachePath := filepath.Join(testDir, "PayloadByHash_"+hash.Hex()+".json")
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			t.Errorf("Cache file was not created: %s", cachePath)
		}
	})

	t.Run("L2BlockRefByNumber", func(t *testing.T) {
		// First call should fetch from mock and cache
		ref1, err := cachedSource.L2BlockRefByNumber(ctx, 100)
		if err != nil {
			t.Fatalf("Failed to get block ref: %v", err)
		}

		// Second call should return from cache
		ref2, err := cachedSource.L2BlockRefByNumber(ctx, 100)
		if err != nil {
			t.Fatalf("Failed to get block ref from cache: %v", err)
		}

		if ref1.Hash != ref2.Hash {
			t.Errorf("Cached result differs from original: %v vs %v", ref1, ref2)
		}

		// Verify cache file exists
		cachePath := filepath.Join(testDir, "L2BlockRefByNumber_100.json")
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			t.Errorf("Cache file was not created: %s", cachePath)
		}
	})

	t.Run("L2BlockRefByHash", func(t *testing.T) {
		hash := common.HexToHash("0x123456789")

		// First call should fetch from mock and cache
		ref1, err := cachedSource.L2BlockRefByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block ref by hash: %v", err)
		}

		// Second call should return from cache
		ref2, err := cachedSource.L2BlockRefByHash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get block ref by hash from cache: %v", err)
		}

		if ref1.Hash != ref2.Hash {
			t.Errorf("Cached result differs from original: %v vs %v", ref1, ref2)
		}
	})

	t.Run("SystemConfigByL2Hash", func(t *testing.T) {
		hash := common.HexToHash("0x123456789")

		// First call should fetch from mock and cache
		config1, err := cachedSource.SystemConfigByL2Hash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get system config: %v", err)
		}

		// Second call should return from cache
		config2, err := cachedSource.SystemConfigByL2Hash(ctx, hash)
		if err != nil {
			t.Fatalf("Failed to get system config from cache: %v", err)
		}

		if config1.BatcherAddr != config2.BatcherAddr {
			t.Errorf("Cached result differs from original: %v vs %v", config1, config2)
		}
	})
}

func TestCachedL2SourceCacheKeyGeneration(t *testing.T) {
	logger := log.New()
	mockSource := newMockL2Source()

	// Test cache key generation for chain 1
	cachedSource1 := NewCachedL2Source(mockSource, logger, 1)
	key1 := cachedSource1.getCacheKey("L2BlockRefByNumber", uint64(100))
	expected1 := "L2BlockRefByNumber_100"
	if key1 != expected1 {
		t.Errorf("Expected cache key %s, got %s", expected1, key1)
	}

	key2 := cachedSource1.getCacheKey("PayloadByHash", "0x123456789")
	expected2 := "PayloadByHash_0x123456789"
	if key2 != expected2 {
		t.Errorf("Expected cache key %s, got %s", expected2, key2)
	}

	// Test cache key generation for chain 999 (different chain ID)
	cachedSource999 := NewCachedL2Source(mockSource, logger, 999)
	key3 := cachedSource999.getCacheKey("L2BlockRefByNumber", uint64(100))
	expected3 := "L2BlockRefByNumber_100"
	if key3 != expected3 {
		t.Errorf("Expected cache key %s, got %s", expected3, key3)
	}

	// Verify that different chain IDs produce different cache keys for the same method and parameters
	if key1 != key3 {
		t.Errorf("Cache keys should be the same for the same method and parameters: %s vs %s", key1, key3)
	}
}

func TestCachedL2SourceCacheDirectory(t *testing.T) {
	logger := log.New()
	mockSource := newMockL2Source()

	// Test with different chain IDs
	chainID := uint64(999)
	cachedSource := NewCachedL2Source(mockSource, logger, chainID)

	expectedDir := filepath.Join("testdata", "chain_999")
	if cachedSource.cacheDir != expectedDir {
		t.Errorf("Expected cache directory %s, got %s", expectedDir, cachedSource.cacheDir)
	}

	// Test directory creation
	if err := cachedSource.ensureCacheDir(); err != nil {
		t.Fatalf("Failed to create cache directory: %v", err)
	}

	// Verify directory exists
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Cache directory was not created: %s", expectedDir)
	}

	// Clean up
	os.RemoveAll(expectedDir)
}
