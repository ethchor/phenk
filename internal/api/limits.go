package api

import (
	"sync"
	"time"
)

// rateLimiter is a token bucket per key, refilling continuously.
//
// The HTTP path mints public inboxes exactly as the SMTP path does, so it is
// limited exactly as strictly: the two together are the whole defence against
// someone minting a million public addresses from one host.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
	swept   time.Time
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// newRateLimiter allows burst events immediately and refills to that budget
// over the given window.
func newRateLimiter(burst int, window time.Duration) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(burst) / window.Seconds(),
		burst:   float64(burst),
		now:     time.Now,
	}
}

// Allow consumes one token for key, reporting whether one was available.
func (l *rateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, seen: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.seen).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.seen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked drops buckets that have refilled completely, so an attacker
// cycling through source addresses cannot grow the map without bound.
func (l *rateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.swept) < time.Minute {
		return
	}
	l.swept = now
	full := time.Duration(l.burst/l.rate) * time.Second
	for key, b := range l.buckets {
		if now.Sub(b.seen) > full {
			delete(l.buckets, key)
		}
	}
}
