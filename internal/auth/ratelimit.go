package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sweepEvery = time.Minute // how often finished windows are dropped
	maxKeys    = 50_000      // hard cap on tracked addresses per limiter
)

// Limiter allows `max` requests per `window` for each client (fixed window).
// ponytail: counts live in this process's memory; move them to Redis if the
// API ever runs as more than one instance.
type Limiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	hits      map[string]*bucket
	nextSweep time.Time
}

type bucket struct {
	count int
	reset time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, hits: map[string]*bucket{}}
}

// Allow records one request for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.hits[key]
	if !ok {
		l.makeRoom(now) // only a new key can grow the map
	}
	if !ok || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.hits[key] = b
	}
	b.count++
	return b.count <= l.max
}

// makeRoom keeps the map bounded without scanning it on every request:
// finished windows are dropped at most once a minute, and a map that is still
// full after that is emptied.
func (l *Limiter) makeRoom(now time.Time) {
	if now.After(l.nextSweep) {
		for k, b := range l.hits {
			if now.After(b.reset) {
				delete(l.hits, k)
			}
		}
		l.nextSweep = now.Add(sweepEvery)
	}
	// ponytail: a flood of different addresses can fill the map with live
	// windows. Starting over briefly gives everyone a fresh allowance, which
	// is a better trade than letting memory grow until the process dies.
	if len(l.hits) >= maxKeys {
		l.hits = map[string]*bucket{}
	}
}

// Limit wraps a handler; over the limit it answers 429 with message.
func (l *Limiter) Limit(message string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(limitKey(ClientIP(r)), time.Now()) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds()))) // upper bound
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{"error": message})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP is the visitor's address. The first X-Forwarded-For entry is used
// only when it is a real IP; anything else falls back to RemoteAddr.
// ponytail: Next.js rewrites do NOT add X-Forwarded-For, and they pass on
// whatever value the visitor sent. So this is only accurate when a proxy in
// front of Next (nginx, Cloudflare, Vercel…) overwrites X-Forwarded-For with
// the real client address. Without one, every visitor shares Next's address
// and gets one bucket for the whole site. See README → Going live.
// The Go API itself must never be reachable from the internet.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limitKey is what a limit counts against. IPv6 addresses count per /64,
// because one home or server usually owns a whole /64 and could otherwise
// step past the limit by rotating addresses.
func limitKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
