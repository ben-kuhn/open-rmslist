package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clientKey identifies the direct client for fair-use limits. It deliberately
// does not trust X-Forwarded-For from arbitrary clients: a loopback reverse
// proxy (the normal self-hosted deployment) supplies the last X-Forwarded-For
// hop, while direct LAN clients are identified by their own address.
func clientIP(r *http.Request) string {
	remote := remoteIP(r)
	ip := net.ParseIP(remote)
	if ip == nil || !ip.IsLoopback() {
		return remote
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remote
	}
	parts := strings.Split(xff, ",")
	candidate := strings.TrimSpace(parts[len(parts)-1])
	if ip := net.ParseIP(candidate); ip != nil {
		return ip.String()
	}
	return remote
}

// clientKey groups IPv6 clients by /64 so rotating addresses within one
// allocation does not multiply the per-client budget.
func clientKey(r *http.Request) string {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return clientIP(r)
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// ipLimiter is a small token bucket keyed by client address.
type ipLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	rate        float64
	burst       float64
	lastCleanup time.Time
}

func newIPLimiter(rate, burst float64) *ipLimiter {
	return &ipLimiter{buckets: make(map[string]*tokenBucket), rate: rate, burst: burst}
}

func (l *ipLimiter) allow(key string, now time.Time) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		// Bound memory if an attacker rotates source addresses. Scan at most
		// once a minute, and hard-reset if even the cleanup cannot keep up.
		if len(l.buckets) > 10000 && now.Sub(l.lastCleanup) >= time.Minute {
			for k, v := range l.buckets {
				if now.Sub(v.last) > 10*time.Minute {
					delete(l.buckets, k)
				}
			}
			l.lastCleanup = now
			if len(l.buckets) > 50000 {
				l.buckets = make(map[string]*tokenBucket)
			}
		}
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.last).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// clientGate lets a client have at most one active ranking request.
type clientGate struct {
	mu     sync.Mutex
	active map[string]bool
}

func newClientGate() *clientGate { return &clientGate{active: make(map[string]bool)} }

func (g *clientGate) acquire(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active[key] {
		return false
	}
	g.active[key] = true
	return true
}

func (g *clientGate) release(key string) {
	g.mu.Lock()
	delete(g.active, key)
	g.mu.Unlock()
}
