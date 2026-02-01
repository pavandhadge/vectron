package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// MockLogger for testing
type MockLogger struct {
	logs []string
	mu   sync.Mutex
}

func (m *MockLogger) Info(msg string, fields map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("INFO: %s %v", msg, fields))
}

func (m *MockLogger) Error(msg string, err error, fields map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("ERROR: %s: %v %v", msg, err, fields))
}

func (m *MockLogger) Debug(msg string, fields map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf("DEBUG: %s %v", msg, fields))
}

func (m *MockLogger) GetLogs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.logs...)
}

// Helper function to create test rerank output
func createTestRerankOutput(id string) *internal.RerankOutput {
	return &internal.RerankOutput{
		Results: []internal.ScoredCandidate{
			{
				ID:            id,
				RerankScore:   0.95,
				OriginalScore: 0.85,
				Explanation:   "test result",
			},
		},
		Latency:  10 * time.Millisecond,
		Metadata: map[string]string{"test": "true"},
	}
}

func TestRobustMemoryCache_BasicOperations(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxSize = 3
	config.DefaultTTL = 100 * time.Millisecond
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Test Set and Get
	result1 := createTestRerankOutput("test1")
	cache.Set("key1", result1, 0)
	
	retrieved, found := cache.Get("key1")
	if !found {
		t.Fatal("Expected to find cached item")
	}
	
	if retrieved.Results[0].ID != "test1" {
		t.Errorf("Expected ID 'test1', got %s", retrieved.Results[0].ID)
	}
	
	// Test non-existent key
	_, found = cache.Get("nonexistent")
	if found {
		t.Error("Expected not to find nonexistent key")
	}
}

func TestRobustMemoryCache_TTL(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.DefaultTTL = 50 * time.Millisecond
	config.CleanupInterval = 20 * time.Millisecond
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Set item with short TTL
	result := createTestRerankOutput("ttl_test")
	cache.Set("ttl_key", result, 30*time.Millisecond)
	
	// Should be available immediately
	_, found := cache.Get("ttl_key")
	if !found {
		t.Fatal("Expected to find item immediately after set")
	}
	
	// Wait for expiration
	time.Sleep(60 * time.Millisecond)
	
	_, found = cache.Get("ttl_key")
	if found {
		t.Error("Expected item to be expired")
	}
}

func TestRobustMemoryCache_EvictionPolicies(t *testing.T) {
	testCases := []struct {
		name   string
		policy EvictionPolicy
	}{
		{"LRU", LRU},
		{"LFU", LFU},
		{"FIFO", FIFO},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &MockLogger{}
			config := DefaultConfig()
			config.MaxSize = 2 // Small size to trigger eviction
			config.EvictionPolicy = tc.policy
			config.DefaultTTL = 5 * time.Second // Long TTL
			cache := NewRobustMemoryCache(config, logger)
			defer cache.Close()
			
			// Fill cache
			cache.Set("key1", createTestRerankOutput("test1"), 0)
			cache.Set("key2", createTestRerankOutput("test2"), 0)
			
			// Access key1 to affect LRU/LFU
			cache.Get("key1")
			
			// Add third item (should trigger eviction)
			cache.Set("key3", createTestRerankOutput("test3"), 0)
			
			// Verify cache size didn't exceed limit
			stats := cache.GetStats()
			if stats.TotalItems > 2 {
				t.Errorf("Cache size exceeded limit: %d", stats.TotalItems)
			}
			
			// For LRU, key2 should be evicted (least recently used)
			// For LFU, either key2 or key3 should be evicted (depending on implementation)
			// For FIFO, key1 should be evicted (first in)
			switch tc.policy {
			case LRU:
				// key2 should be evicted
				if _, found := cache.Get("key2"); found {
					t.Error("LRU: Expected key2 to be evicted")
				}
			case FIFO:
				// key1 should be evicted
				if _, found := cache.Get("key1"); found {
					t.Error("FIFO: Expected key1 to be evicted")
				}
			}
		})
	}
}

func TestRobustMemoryCache_ConcurrentAccess(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxSize = 100
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	const numGoroutines = 10
	const itemsPerGoroutine = 20
	
	var wg sync.WaitGroup
	
	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				result := createTestRerankOutput(fmt.Sprintf("test_%d_%d", id, j))
				cache.Set(key, result, 0)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify all items were stored (up to max size)
	stats := cache.GetStats()
	if stats.TotalItems == 0 {
		t.Error("Expected some items to be cached")
	}
	
	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				cache.Get(key) // Don't care about result, just testing concurrency
			}
		}(i)
	}
	
	wg.Wait()
}

func TestRobustMemoryCache_Statistics(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.StatsEnabled = true
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Generate some hits and misses
	result := createTestRerankOutput("stats_test")
	cache.Set("stats_key", result, 0)
	
	// Hit
	cache.Get("stats_key")
	
	// Miss
	cache.Get("nonexistent")
	
	stats := cache.GetStats()
	
	if stats.Hits != 1 {
		t.Errorf("Expected 1 hit, got %d", stats.Hits)
	}
	
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	
	if stats.TotalItems != 1 {
		t.Errorf("Expected 1 total item, got %d", stats.TotalItems)
	}
	
	stats.CalculateHitRate()
	if stats.HitRate != 0.5 {
		t.Errorf("Expected hit rate 0.5, got %f", stats.HitRate)
	}
}

func TestRobustMemoryCache_Clear(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Add some items
	cache.Set("key1", createTestRerankOutput("test1"), 0)
	cache.Set("key2", createTestRerankOutput("test2"), 0)
	cache.Set("prefix:key3", createTestRerankOutput("test3"), 0)
	
	// Clear specific pattern
	cleared := cache.Clear("prefix:*")
	if cleared != 1 {
		t.Errorf("Expected to clear 1 item, got %d", cleared)
	}
	
	// Verify specific item was cleared
	if _, found := cache.Get("prefix:key3"); found {
		t.Error("Expected prefix:key3 to be cleared")
	}
	
	// Verify other items still exist
	if _, found := cache.Get("key1"); !found {
		t.Error("Expected key1 to still exist")
	}
	
	// Clear all
	cleared = cache.Clear("")
	if cleared < 2 {
		t.Errorf("Expected to clear at least 2 items, got %d", cleared)
	}
}

func TestRobustMemoryCache_Delete(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Add and delete
	cache.Set("delete_test", createTestRerankOutput("test"), 0)
	
	if _, found := cache.Get("delete_test"); !found {
		t.Fatal("Expected item to exist before delete")
	}
	
	cache.Delete("delete_test")
	
	if _, found := cache.Get("delete_test"); found {
		t.Error("Expected item to be deleted")
	}
}

func TestCacheFactory(t *testing.T) {
	logger := &MockLogger{}
	factory := NewCacheFactory(logger)
	
	// Test memory cache creation
	cache, err := factory.CreateCache("memory")
	if err != nil {
		t.Fatalf("Failed to create memory cache: %v", err)
	}
	defer cache.Close()
	
	// Test basic functionality
	result := createTestRerankOutput("factory_test")
	cache.Set("test", result, 0)
	
	retrieved, found := cache.Get("test")
	if !found {
		t.Error("Expected to find cached item")
	}
	
	if retrieved.Results[0].ID != "factory_test" {
		t.Error("Retrieved item doesn't match original")
	}
	
	// Test unsupported backend
	_, err = factory.CreateCache("unsupported")
	if err == nil {
		t.Error("Expected error for unsupported backend")
	}
}

func TestMemoryPressure(t *testing.T) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxMemoryMB = 1 // 1MB limit
	config.MaxSize = 1000  // Large item count but small memory
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Try to exceed memory limit
	for i := 0; i < 100; i++ {
		largeResult := &internal.RerankOutput{
			Results: make([]internal.ScoredCandidate, 100), // Large structure
			Metadata: map[string]string{
				"large_data": string(make([]byte, 1024)), // 1KB per item
			},
		}
		
		cache.Set(fmt.Sprintf("large_%d", i), largeResult, 0)
	}
	
	// Cache should have automatically evicted items to stay within memory limit
	stats := cache.GetStats()
	
	// We can't test exact memory usage without runtime.MemStats integration,
	// but we can verify the cache is still functional
	if stats.TotalItems == 0 {
		t.Error("Cache should contain some items")
	}
}

// Benchmark tests
func BenchmarkRobustMemoryCache_Set(b *testing.B) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxSize = 10000
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	result := createTestRerankOutput("benchmark")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(fmt.Sprintf("bench_key_%d", i), result, 0)
	}
}

func BenchmarkRobustMemoryCache_Get(b *testing.B) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxSize = 10000
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	// Populate cache
	result := createTestRerankOutput("benchmark")
	for i := 0; i < 1000; i++ {
		cache.Set(fmt.Sprintf("bench_key_%d", i), result, 0)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(fmt.Sprintf("bench_key_%d", i%1000))
	}
}

func BenchmarkRobustMemoryCache_Concurrent(b *testing.B) {
	logger := &MockLogger{}
	config := DefaultConfig()
	config.MaxSize = 10000
	cache := NewRobustMemoryCache(config, logger)
	defer cache.Close()

	result := createTestRerankOutput("benchmark")
	
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				cache.Set(fmt.Sprintf("concurrent_key_%d", i), result, 0)
			} else {
				cache.Get(fmt.Sprintf("concurrent_key_%d", i-1))
			}
			i++
		}
	})
}