package smtpd

import (
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"
)

// connectionLimiter caps concurrent connections per source address. A single
// host opening hundreds of sessions is either broken or hostile, and either way
// it must not be able to occupy the whole listener.
type connectionLimiter struct {
	mu    sync.Mutex
	perIP map[netip.Addr]int
	limit int
}

func newConnectionLimiter(limit int) *connectionLimiter {
	if limit < 1 {
		limit = 1
	}
	return &connectionLimiter{perIP: make(map[netip.Addr]int), limit: limit}
}

// Acquire reserves a connection slot, reporting whether one was available.
func (l *connectionLimiter) Acquire(ip netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perIP[ip] >= l.limit {
		return false
	}
	l.perIP[ip]++
	return true
}

// Release returns a slot. It is safe to call for an address that holds none.
func (l *connectionLimiter) Release(ip netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.perIP[ip]; n <= 1 {
		delete(l.perIP, ip)
	} else {
		l.perIP[ip] = n - 1
	}
}

// rateLimiter is a token bucket per key, refilling continuously.
//
// It is used twice with very different budgets: once for connections, where the
// limit is generous, and once for lazy provisioning of named inboxes, where it
// is the main defence against someone minting a million public addresses from
// one host.
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

// limitedListener enforces the per-source connection cap at accept time.
//
// go-smtp only calls into the backend once a client has sent EHLO, so a cap
// applied there would let a hostile client hold open any number of connections
// simply by never speaking. Refusing at accept closes that gap, and the client
// still gets a 421 telling it why rather than an unexplained reset.
type limitedListener struct {
	net.Listener
	limiter *connectionLimiter
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := remoteAddr(conn)
		if !l.limiter.Acquire(ip) {
			slog.Warn("connection refused, too many from source", "client_ip", ip.String())
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, _ = io.WriteString(conn, "421 4.7.0 Too many connections from your address\r\n")
			_ = conn.Close()
			continue
		}
		return &countedConn{Conn: conn, limiter: l.limiter, ip: ip}, nil
	}
}

// countedConn releases its source's connection slot exactly once, whenever and
// however the connection is closed.
type countedConn struct {
	net.Conn
	limiter *connectionLimiter
	ip      netip.Addr
	once    sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.limiter.Release(c.ip) })
	return c.Conn.Close()
}
