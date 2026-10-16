package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kalle/local-model-router/internal/keystore"
)

// doLimited sends one request through rl.Middleware as a key with the given
// ID and limit, the way auth would after re-reading the key from the store.
func doLimited(rl *RateLimiter, keyID string, limit int) int {
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	key := &keystore.APIKey{ID: keyID, RateLimit: limit}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req = req.WithContext(context.WithValue(req.Context(), APIKeyContextKey, key))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRateLimiterLiveLimitChange(t *testing.T) {
	type step struct {
		limit int
		want  int
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"AC1 raise 2 to 4", []step{
			{2, 200}, {2, 200}, {4, 200}, {4, 200}, {4, 429},
		}},
		{"AC2 lower 5 to 2 keeps old requests", []step{
			{5, 200}, {5, 200}, {5, 200}, {2, 429},
		}},
		{"AC3 change to 0 disables limit", []step{
			{1, 200}, {1, 429}, {0, 200}, {0, 200}, {0, 200},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(true)
			for i, s := range tt.steps {
				if got := doLimited(rl, "key-1", s.limit); got != s.want {
					t.Fatalf("step %d (limit %d): got %d, want %d", i, s.limit, got, s.want)
				}
			}
		})
	}
}

// AC4: concurrent requests on one key while the limit changes; run with -race.
func TestRateLimiterLiveLimitConcurrent(t *testing.T) {
	rl := NewRateLimiter(true)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			doLimited(rl, "key-1", 1+i%10)
		}(i)
	}
	wg.Wait()
	// At least one burst request was allowed and still counts, so limit 1 rejects.
	if got := doLimited(rl, "key-1", 1); got != http.StatusTooManyRequests {
		t.Fatalf("after concurrent burst: got %d, want 429", got)
	}
}
