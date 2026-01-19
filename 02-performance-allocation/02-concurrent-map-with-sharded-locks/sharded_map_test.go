package concurrentmapwithshardedlocks

import (
	"runtime"
	"sync"
	"testing"
)

// =============================================================================
// Functional Tests
// =============================================================================

func TestShardedMap_BasicOperations(t *testing.T) {
	m := NewShardedMap[string, int](16)

	// Test Set and Get
	m.Set("key1", 100)
	m.Set("key2", 200)

	val, ok := m.Get("key1")
	if !ok || val != 100 {
		t.Errorf("Get(key1) = %v, %v; want 100, true", val, ok)
	}

	val, ok = m.Get("key2")
	if !ok || val != 200 {
		t.Errorf("Get(key2) = %v, %v; want 200, true", val, ok)
	}

	// Test non-existent key
	val, ok = m.Get("nonexistent")
	if ok {
		t.Errorf("Get(nonexistent) = %v, %v; want zero, false", val, ok)
	}

	// Test Delete
	m.Delete("key1")
	val, ok = m.Get("key1")
	if ok {
		t.Errorf("After Delete, Get(key1) = %v, %v; want zero, false", val, ok)
	}

	// Test Keys
	keys := m.Keys()
	if len(keys) != 1 {
		t.Errorf("Keys() returned %d keys; want 1", len(keys))
	}
}

func TestShardedMap_Update(t *testing.T) {
	m := NewShardedMap[string, int](8)

	m.Set("counter", 1)
	val, _ := m.Get("counter")
	if val != 1 {
		t.Errorf("Initial Set: got %d, want 1", val)
	}

	m.Set("counter", 42)
	val, _ = m.Get("counter")
	if val != 42 {
		t.Errorf("Update: got %d, want 42", val)
	}
}

func TestShardedMap_IntKeys(t *testing.T) {
	m := NewShardedMap[int, string](16)

	for i := 0; i < 1000; i++ {
		m.Set(i, "value")
	}

	keys := m.Keys()
	if len(keys) != 1000 {
		t.Errorf("Keys() returned %d keys; want 1000", len(keys))
	}

	for i := 0; i < 1000; i++ {
		val, ok := m.Get(i)
		if !ok || val != "value" {
			t.Errorf("Get(%d) = %v, %v; want 'value', true", i, val, ok)
		}
	}
}

// =============================================================================
// Race Test - Run with `go test -race`
// Tests concurrent read/write/delete operations for data races
// =============================================================================

func TestShardedMap_RaceCondition(t *testing.T) {
	m := NewShardedMap[int, int](64)
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // readers, writers, deleters

	// Writers
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := goroutineID*numOperations + i
				m.Set(key, i)
			}
		}(g)
	}

	// Readers
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := goroutineID*numOperations + i
				m.Get(key)
			}
		}(g)
	}

	// Deleters
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := goroutineID*numOperations + i
				m.Delete(key)
			}
		}(g)
	}

	wg.Wait()
}

func TestShardedMap_RaceCondition_Keys(t *testing.T) {
	m := NewShardedMap[int, int](64)
	const numGoroutines = 50
	const numOperations = 500

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Writers
	for g := 0; g < numGoroutines; g++ {
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				key := goroutineID*numOperations + i
				m.Set(key, i)
			}
		}(g)
	}

	// Keys() callers - test concurrent reads with Keys()
	for g := 0; g < numGoroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < numOperations/10; i++ {
				_ = m.Keys()
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// Contention Test (Benchmarks)
// Run with: go test -bench=BenchmarkContention -cpuprofile cpu.out
// Compare 1 shard vs 64 shards: should see near-linear scaling with 64 shards
// =============================================================================

func BenchmarkContention_1Shard(b *testing.B) {
	benchmarkContention(b, 1)
}

func BenchmarkContention_8Shards(b *testing.B) {
	benchmarkContention(b, 8)
}

func BenchmarkContention_64Shards(b *testing.B) {
	benchmarkContention(b, 64)
}

func BenchmarkContention_256Shards(b *testing.B) {
	benchmarkContention(b, 256)
}

func benchmarkContention(b *testing.B, numShards uint) {
	m := NewShardedMap[int, int](numShards)
	const numGoroutines = 8

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Set(i, i)
			i++
		}
	})
}

// Benchmark for sequential key access pattern (worst case for sharding)
func BenchmarkContention_SequentialKeys_1Shard(b *testing.B) {
	benchmarkContentionSequential(b, 1)
}

func BenchmarkContention_SequentialKeys_64Shards(b *testing.B) {
	benchmarkContentionSequential(b, 64)
}

func benchmarkContentionSequential(b *testing.B, numShards uint) {
	m := NewShardedMap[int, int](numShards)
	const numGoroutines = 8

	var wg sync.WaitGroup
	opsPerGoroutine := b.N / numGoroutines

	b.ResetTimer()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(startKey int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := startKey + i
				m.Set(key, i)
			}
		}(g * opsPerGoroutine)
	}

	wg.Wait()
}

// =============================================================================
// Memory Test
// Store 1 million int keys with interface{} values
// Fail Condition: If solution uses more than 50MB extra memory vs baseline map
// =============================================================================

func TestShardedMap_MemoryUsage(t *testing.T) {
	const numKeys = 1_000_000

	// Measure sharded map memory usage
	runtime.GC()
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	m := NewShardedMap[int, any](64)
	for i := 0; i < numKeys; i++ {
		m.Set(i, i) // Store int as interface{}
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	// Use TotalAlloc which only increases (cumulative allocations)
	shardedMapAlloc := memAfter.TotalAlloc - memBefore.TotalAlloc
	shardedMapMB := float64(shardedMapAlloc) / (1024 * 1024)

	// Keep reference to prevent GC
	_ = m.Keys()

	t.Logf("ShardedMap memory for %d entries: %.2f MB (TotalAlloc)", numKeys, shardedMapMB)

	// Measure baseline map memory usage
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	baselineMap := make(map[int]any)
	for i := 0; i < numKeys; i++ {
		baselineMap[i] = i
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	baselineAlloc := memAfter.TotalAlloc - memBefore.TotalAlloc
	baselineMB := float64(baselineAlloc) / (1024 * 1024)

	// Keep reference to prevent GC
	_ = len(baselineMap)

	t.Logf("Baseline map memory for %d entries: %.2f MB (TotalAlloc)", numKeys, baselineMB)

	extraMemoryMB := shardedMapMB - baselineMB
	t.Logf("Extra memory vs baseline: %.2f MB", extraMemoryMB)

	// Fail if more than 50MB extra memory
	if extraMemoryMB > 50 {
		t.Errorf("Memory usage too high! Extra memory: %.2f MB (limit: 50MB)", extraMemoryMB)
	}
}

func BenchmarkMemory_ShardedMap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := NewShardedMap[int, any](64)
		for j := 0; j < 100_000; j++ {
			m.Set(j, j)
		}
	}
}

func BenchmarkMemory_BaselineMap(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := make(map[int]any)
		for j := 0; j < 100_000; j++ {
			m[j] = j
		}
	}
}

// =============================================================================
// Read-Heavy Workload Benchmark (95% reads, 5% writes as per README scenario)
// =============================================================================

func BenchmarkReadHeavyWorkload_1Shard(b *testing.B) {
	benchmarkReadHeavyWorkload(b, 1)
}

func BenchmarkReadHeavyWorkload_64Shards(b *testing.B) {
	benchmarkReadHeavyWorkload(b, 64)
}

func benchmarkReadHeavyWorkload(b *testing.B, numShards uint) {
	m := NewShardedMap[int, int](numShards)

	// Pre-populate with data
	for i := 0; i < 10000; i++ {
		m.Set(i, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := i % 10000
			if i%20 == 0 { // 5% writes
				m.Set(key, i)
			} else { // 95% reads
				m.Get(key)
			}
			i++
		}
	})
}

// =============================================================================
// Allocation Benchmarks (verify zero-allocation hot-path)
// =============================================================================

func BenchmarkGet_Allocations(b *testing.B) {
	m := NewShardedMap[int, int](64)
	for i := 0; i < 1000; i++ {
		m.Set(i, i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		m.Get(i % 1000)
	}
}

func BenchmarkSet_Allocations(b *testing.B) {
	m := NewShardedMap[int, int](64)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		m.Set(i%1000, i)
	}
}
