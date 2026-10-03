package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Failed-auth limits: an IP with failLimit failures inside failWindow is
// refused until the window ends. Keys are 256-bit, so this guards load and
// noise rather than guessing.
const (
	failLimit   = 10
	failWindow  = 10 * time.Minute
	failMaxIPs  = 10000
	unknownAddr = "unknown"
)

type failEntry struct {
	count int
	reset time.Time
}

// failLimiter counts failed authentications per client IP in fixed windows.
// IPs are kept in memory only and never logged.
type failLimiter struct {
	clock   func() time.Time
	mu      sync.Mutex
	entries map[string]*failEntry
}

func newFailLimiter(clock func() time.Time) *failLimiter {
	if clock == nil {
		clock = time.Now
	}
	return &failLimiter{clock: clock, entries: map[string]*failEntry{}}
}

// blocked returns how long ip must wait, or 0 if it may try.
func (l *failLimiter) blocked(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[ip]
	if !ok || e.count < failLimit {
		return 0
	}
	if wait := e.reset.Sub(l.clock()); wait > 0 {
		return wait
	}
	delete(l.entries, ip)
	return 0
}

// fail records one failed attempt from ip.
func (l *failLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock()
	e, ok := l.entries[ip]
	if ok && !now.Before(e.reset) {
		ok = false
	}
	if !ok {
		if len(l.entries) >= failMaxIPs {
			l.prune(now)
			if len(l.entries) >= failMaxIPs {
				return // full of live entries: stop tracking new IPs rather than grow
			}
		}
		e = &failEntry{reset: now.Add(failWindow)}
		l.entries[ip] = e
	}
	e.count++
}

func (l *failLimiter) prune(now time.Time) {
	for ip, e := range l.entries {
		if !now.Before(e.reset) {
			delete(l.entries, ip)
		}
	}
}

// clientIP is the peer address, or with trustXFF the last X-Forwarded-For
// entry: the address the trusted proxy itself saw, which a client can't forge.
func clientIP(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return unknownAddr
	}
	return host
}
