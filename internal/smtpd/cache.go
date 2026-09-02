package smtpd

import (
	"container/list"
	"sync"
	"time"
)

// ttlCache is a small LRU with a short expiry, sitting in front of Postgres on
// the RCPT TO path.
//
// It caches only answers that are safe to be slightly stale. Nothing negative
// is ever cached: a name that does not exist yet may be lazily provisioned a
// moment later, and serving a stale "no" would reject mail for an inbox that
// does exist. The entries it does hold are re-validated under a row lock at
// commit time, so the cache is an optimisation and never the authority.
type ttlCache[K comparable, V any] struct {
	mu       sync.Mutex
	entries  map[K]*list.Element
	order    *list.List
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

type cacheEntry[K comparable, V any] struct {
	key     K
	value   V
	expires time.Time
}

func newTTLCache[K comparable, V any](capacity int, ttl time.Duration) *ttlCache[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &ttlCache[K, V]{
		entries:  make(map[K]*list.Element, capacity),
		order:    list.New(),
		capacity: capacity,
		ttl:      ttl,
		now:      time.Now,
	}
}

// Get returns a live value, or the zero value and false.
func (c *ttlCache[K, V]) Get(key K) (V, bool) {
	var zero V
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	entry := element.Value.(*cacheEntry[K, V])
	if c.now().After(entry.expires) {
		c.removeLocked(element)
		return zero, false
	}
	c.order.MoveToFront(element)
	return entry.value, true
}

// Put stores a value, evicting the least recently used entry if the cache is
// full.
func (c *ttlCache[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expires := c.now().Add(c.ttl)
	if element, ok := c.entries[key]; ok {
		entry := element.Value.(*cacheEntry[K, V])
		entry.value, entry.expires = value, expires
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(&cacheEntry[K, V]{key: key, value: value, expires: expires})
	c.entries[key] = element

	for c.order.Len() > c.capacity {
		c.removeLocked(c.order.Back())
	}
}

// Forget drops a key, so a caller that has just changed the underlying row does
// not have to wait out the ttl.
func (c *ttlCache[K, V]) Forget(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.removeLocked(element)
	}
}

func (c *ttlCache[K, V]) removeLocked(element *list.Element) {
	if element == nil {
		return
	}
	c.order.Remove(element)
	delete(c.entries, element.Value.(*cacheEntry[K, V]).key)
}

// Len reports the number of entries held, live or not.
func (c *ttlCache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
