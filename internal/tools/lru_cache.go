package tools

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache is a bounded, time-expiring cache replacing sync.Map.
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	items    map[string]*list.Element
	order    *list.List // front = most recently used
}

type lruEntry struct {
	key       string
	value     string
	timestamp time.Time
}

// NewLRUCache creates a bounded LRU cache.
func NewLRUCache(capacity int, ttl time.Duration) *LRUCache {
	if capacity <= 0 {
		capacity = 500
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &LRUCache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

// Get retrieves a cached value. Returns ("", false) on miss or expiry.
func (c *LRUCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return "", false
	}
	entry := elem.Value.(*lruEntry)
	if time.Since(entry.timestamp) > c.ttl {
		c.removeLocked(elem)
		return "", false
	}
	c.order.MoveToFront(elem)
	return entry.value, true
}

// Set stores a value, evicting the least recently used entry if at capacity.
func (c *LRUCache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*lruEntry)
		entry.value = value
		entry.timestamp = time.Now()
		c.order.MoveToFront(elem)
		return
	}

	// Evict if at capacity
	for c.order.Len() >= c.capacity {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.removeLocked(back)
	}

	entry := &lruEntry{key: key, value: value, timestamp: time.Now()}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
}

// Invalidate clears all cached entries.
func (c *LRUCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element, c.capacity)
	c.order.Init()
}

// Len returns the number of cached entries.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *LRUCache) removeLocked(elem *list.Element) {
	entry := elem.Value.(*lruEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
}
