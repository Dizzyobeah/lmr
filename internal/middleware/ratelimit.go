package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/kalle/local-model-router/pkg/openai"
)

// RateLimiter provides per-key rate limiting.
type RateLimiter struct {
	windows map[string]*slidingWindow
	mu      sync.RWMutex
	enabled bool
}

// slidingWindow implements a simple sliding window rate limiter.
type slidingWindow struct {
	requests []time.Time
	limit    int
	mu       sync.Mutex
}

// NewRateLimiter creates a new RateLimiter.
func NewRateLimiter(enabled bool) *RateLimiter {
	rl := &RateLimiter{
		windows: make(map[string]*slidingWindow),
		enabled: enabled,
	}
	
	// Start cleanup goroutine
	go rl.cleanup()
	
	return rl
}

// Middleware returns the rate limiting middleware handler.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.enabled {
			next.ServeHTTP(w, r)
			return
		}
		
		// Get API key from context
		key := GetAPIKey(r.Context())
		if key == nil || key.RateLimit == 0 {
			// No rate limit configured
			next.ServeHTTP(w, r)
			return
		}
		
		// Check rate limit
		if !rl.allow(key.ID, key.RateLimit) {
			w.Header().Set("X-RateLimit-Limit", string(rune(key.RateLimit)))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Retry-After", "60")
			openai.WriteError(w, openai.ErrRateLimit("Rate limit exceeded"))
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// allow checks if a request is allowed under the rate limit.
func (rl *RateLimiter) allow(keyID string, limit int) bool {
	rl.mu.Lock()
	window, exists := rl.windows[keyID]
	if !exists {
		window = &slidingWindow{
			requests: make([]time.Time, 0, limit),
			limit:    limit,
		}
		rl.windows[keyID] = window
	}
	rl.mu.Unlock()
	
	return window.allow()
}

// allow checks if a request is allowed in this window.
func (sw *slidingWindow) allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	
	now := time.Now()
	windowStart := now.Add(-time.Minute)
	
	// Remove old requests outside the window
	validRequests := make([]time.Time, 0, len(sw.requests))
	for _, t := range sw.requests {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}
	sw.requests = validRequests
	
	// Check if we're at the limit
	if len(sw.requests) >= sw.limit {
		return false
	}
	
	// Add this request
	sw.requests = append(sw.requests, now)
	return true
}

// cleanup periodically removes stale windows.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		windowStart := now.Add(-time.Minute)
		
		for keyID, window := range rl.windows {
			window.mu.Lock()
			// Remove window if no recent requests
			hasRecent := false
			for _, t := range window.requests {
				if t.After(windowStart) {
					hasRecent = true
					break
				}
			}
			if !hasRecent {
				delete(rl.windows, keyID)
			}
			window.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}
