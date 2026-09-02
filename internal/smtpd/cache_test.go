package smtpd

import (
	"testing"
	"time"
)

func TestTTLCacheStoresAndExpires(t *testing.T) {
	cache := newTTLCache[string, int](4, time.Minute)
	now := time.Now()
	cache.now = func() time.Time { return now }

	cache.Put("a", 1)
	if got, ok := cache.Get("a"); !ok || got != 1 {
		t.Fatalf("Get = %v, %v", got, ok)
	}

	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get("a"); ok {
		t.Fatal("an expired entry was served")
	}
	if cache.Len() != 0 {
		t.Fatalf("an expired entry was left behind: %d", cache.Len())
	}
}

func TestTTLCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newTTLCache[string, int](2, time.Minute)

	cache.Put("a", 1)
	cache.Put("b", 2)
	// Touching "a" makes "b" the least recently used.
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should still be cached")
	}
	cache.Put("c", 3)

	if _, ok := cache.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := cache.Get("a"); !ok {
		t.Fatal("a should have survived")
	}
	if _, ok := cache.Get("c"); !ok {
		t.Fatal("c should be cached")
	}
	if cache.Len() != 2 {
		t.Fatalf("cache holds %d entries, want 2", cache.Len())
	}
}

func TestTTLCacheOverwriteRefreshesExpiry(t *testing.T) {
	cache := newTTLCache[string, int](4, time.Minute)
	now := time.Now()
	cache.now = func() time.Time { return now }

	cache.Put("a", 1)
	now = now.Add(50 * time.Second)
	cache.Put("a", 2)
	now = now.Add(50 * time.Second)

	got, ok := cache.Get("a")
	if !ok || got != 2 {
		t.Fatalf("Get = %v, %v: overwriting should refresh both value and expiry", got, ok)
	}
}

func TestTTLCacheForget(t *testing.T) {
	cache := newTTLCache[string, int](4, time.Minute)
	cache.Put("a", 1)
	cache.Forget("a")
	if _, ok := cache.Get("a"); ok {
		t.Fatal("a forgotten entry was served")
	}
	cache.Forget("missing") // harmless
}

func TestTTLCacheIsConcurrencySafe(t *testing.T) {
	cache := newTTLCache[int, int](64, time.Minute)
	done := make(chan struct{})
	for w := 0; w < 8; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 500; i++ {
				cache.Put(i%128, i)
				cache.Get(i % 128)
				if i%50 == 0 {
					cache.Forget(i % 128)
				}
			}
		}(w)
	}
	for w := 0; w < 8; w++ {
		<-done
	}
	if cache.Len() > 64 {
		t.Fatalf("cache grew past its capacity: %d", cache.Len())
	}
}
