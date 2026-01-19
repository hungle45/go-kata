package concurrentmapwithshardedlocks

import (
	"fmt"
	"hash/fnv"
	"sync"
)

type ShardedMap[K comparable, V any] interface {
	Get(key K) (V, bool)
	Set(key K, value V)
	Delete(key K)
	Keys() []K
}

type shardedMap[K comparable, V any] struct {
	shards []map[K]V
	locks  []sync.RWMutex
}

func NewShardedMap[K comparable, V any](numShards uint) ShardedMap[K, V] {
	shards := make([]map[K]V, numShards)
	for i := range shards {
		shards[i] = make(map[K]V)
	}
	return &shardedMap[K, V]{
		shards: shards,
		locks:  make([]sync.RWMutex, numShards),
	}
}

func (s *shardedMap[K, V]) Delete(key K) {
	shardIndex := s.shardIndex(key)
	s.locks[shardIndex].Lock()
	defer s.locks[shardIndex].Unlock()
	delete(s.shards[shardIndex], key)
}

func (s *shardedMap[K, V]) Get(key K) (V, bool) {
	shardIndex := s.shardIndex(key)
	s.locks[shardIndex].RLock()
	defer s.locks[shardIndex].RUnlock()
	value, ok := s.shards[shardIndex][key]
	return value, ok
}

func (s *shardedMap[K, V]) Keys() []K {
	keys := make([]K, 0)
	for i := range s.shards {
		s.locks[i].RLock()
		for key := range s.shards[i] {
			keys = append(keys, key)
		}
		s.locks[i].RUnlock()
	}
	return keys
}
func (s *shardedMap[K, V]) Set(key K, value V) {
	shardIndex := s.shardIndex(key)
	s.locks[shardIndex].Lock()
	defer s.locks[shardIndex].Unlock()
	s.shards[shardIndex][key] = value
}

func (s *shardedMap[K, V]) shardIndex(key K) int {
	hashFn := fnv.New64a()
	hashFn.Write([]byte(fmt.Sprintf("%v", key)))
	return int(hashFn.Sum64() % uint64(len(s.shards)))
}
