package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kalle/local-model-router/internal/keystore"
)

func TestRateLimitHeaders(t *testing.T) {
	rl := &RateLimiter{windows: make(map[string]*slidingWindow), enabled: true}
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	key := &keystore.APIKey{ID: "k1", RateLimit: 60}

	var last *httptest.ResponseRecorder
	for i := 0; i < 61; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), APIKeyContextKey, key))
		last = httptest.NewRecorder()
		h.ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", last.Code)
	}
	if got := last.Header().Get("X-RateLimit-Limit"); got != "60" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", got, "60")
	}
}
