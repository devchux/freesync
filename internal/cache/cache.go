package cache

import (
	"sync"
	"time"
)

// Config controls the lifetime of mount-local file data and directory listings.
type Config struct {
	DataTTL     time.Duration
	MetadataTTL time.Duration
}

type cacheEntry[V any] struct {
	value V
	until time.Time
}

type Cache[K comparable, V any] struct {
	mu         sync.Mutex
	ttl        time.Duration
	limit      int
	generation uint64
	entries    map[K]cacheEntry[V]
}

func New[K comparable, V any](ttl time.Duration, limit int) *Cache[K, V] {
	return &Cache[K, V]{ttl: ttl, limit: limit}
}

func (c *Cache[K, V]) Enabled() bool {
	return c != nil && c.ttl > 0
}

func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	clear(c.entries)
}

func (c *Cache[K, V]) Load(key K, fetch func() (V, error)) (V, error) {
	if !c.Enabled() {
		return fetch()
	}
	c.mu.Lock()
	entry, found := c.entries[key]
	generation := c.generation
	c.mu.Unlock()
	if found && time.Now().Before(entry.until) {
		return entry.value, nil
	}

	value, err := fetch()
	if err != nil {
		return value, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation == c.generation {
		if c.entries == nil || len(c.entries) >= c.limit {
			c.entries = make(map[K]cacheEntry[V])
		}
		c.entries[key] = cacheEntry[V]{value: value, until: time.Now().Add(c.ttl)}
	}
	return value, nil
}
