package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"forge/internal/types"
)

type limiter struct {
	mu     sync.Mutex
	tokens int
	last   time.Time
}

type RateLimiter struct {
	mu          sync.Mutex
	limits      map[string]*limiter
	rate        int
	capacity    int
	windowReset time.Duration
}

func NewRateLimiter(capacity int, windowReset time.Duration) *RateLimiter {
	return &RateLimiter{
		limits:      make(map[string]*limiter),
		capacity:    capacity,
		windowReset: windowReset,
	}
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	l, exists := rl.limits[key]
	if !exists {
		rl.limits[key] = &limiter{tokens: rl.capacity - 1, last: time.Now()}
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.last)

	// Refill based on elapsed time and window reset
	// Simple implementation: full refill after windowReset
	if elapsed > rl.windowReset {
		l.tokens = rl.capacity
		l.last = now
	}

	if l.tokens > 0 {
		l.tokens--
		return true
	}

	return false
}

// Middleware creates a rate limiting middleware based on a key extraction function
func (rl *RateLimiter) Middleware(extractKey func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := extractKey(r)
			if !rl.allow(key) {
				w.Header().Set("Retry-After", "60") // Simplified, ideally calculated
				types.WriteError(w, types.ErrRateLimited)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ExtractIP gets the IP address from the request
func ExtractIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}

	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		return host
	}
	return ip
}
