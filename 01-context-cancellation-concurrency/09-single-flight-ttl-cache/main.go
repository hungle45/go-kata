package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type Cache[K comparable, V any] struct {
	g   *singleflight.Group
	c   map[K]*Item[V]
	mu  sync.RWMutex
	ttl time.Duration
}

func NewCache[K comparable, V any](ttl time.Duration) *Cache[K, V] {
	return &Cache[K, V]{
		g:   new(singleflight.Group),
		c:   make(map[K]*Item[V]),
		ttl: ttl,
	}
}

func (c *Cache[K, V]) Get(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	c.mu.RLock()
	item, ok := c.c[key]
	c.mu.RUnlock()

	if ok && !item.isExpired() {
		return item.value, nil
	}

	select {
	case <-ctx.Done():
		return *new(V), fmt.Errorf("failed to load key %v: %w", key, ctx.Err())
	case res := <-c.g.DoChan(keyToString(key), c.newLoaderFunc(ctx, key, loader)):
		if res.Err != nil {
			return *new(V), fmt.Errorf("failed to load key %v: %w", key, res.Err)
		}
		return res.Val.(V), nil
	}
}

func (c *Cache[K, V]) newLoaderFunc(ctx context.Context, key K, loader func(context.Context) (V, error)) func() (interface{}, error) {
	return func() (interface{}, error) {
		v, err := loader(context.WithoutCancel(ctx))
		if err == nil {
			c.mu.Lock()
			c.c[key] = NewCacheItem(v, c.ttl)
			c.mu.Unlock()
		}
		return v, err
	}
}

type Item[V any] struct {
	value V
	exp   time.Time
}

func NewCacheItem[V any](value V, ttl time.Duration) *Item[V] {
	return &Item[V]{
		value: value,
		exp:   time.Now().Add(ttl),
	}
}

func (i *Item[V]) isExpired() bool {
	return time.Now().After(i.exp)
}

func keyToString[K comparable](key K) string {
	if s, ok := any(key).(string); ok {
		return s
	}
	return fmt.Sprintf("%#v", key)
}
