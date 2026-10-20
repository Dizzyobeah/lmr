package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/pkg/openai"
)

func TestRequestBodyLimit(t *testing.T) {
	const limit = 1024
	big := `{"model":"nomic-embed-text","input":"` + strings.Repeat("a", 2*limit) + `"}`

	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantMsg    string // substring of error message; "" for success
	}{
		{"embeddings over limit", "/v1/embeddings", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"embeddings alias over limit", "/embeddings", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"chat over limit", "/v1/chat/completions", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"chat alias over limit", "/chat/completions", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"completions over limit", "/v1/completions", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"completions alias over limit", "/completions", big, http.StatusRequestEntityTooLarge, "1024 bytes"},
		{"malformed json under limit", "/v1/embeddings", `{"model":`, http.StatusBadRequest, "Invalid JSON"},
		{"valid embeddings under limit", "/v1/embeddings", `{"model":"nomic-embed-text","input":"hi"}`, http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls, _ := fakeOllamaEmbed(t, `{"embeddings":[[0.1]],"prompt_eval_count":1}`)

			cfg := config.DefaultConfig()
			cfg.Auth.Enabled = false
			cfg.Admin.Enabled = false
			cfg.Metrics.Enabled = false
			cfg.Server.MaxRequestBodyBytes = limit
			cache := ollama.NewModelCache(nil, 0)
			cache.SetModels([]ollama.Model{{Name: "nomic-embed-text"}})
			s := NewServer(cfg, ollama.NewClient(srv.URL, 5*time.Second, 1), nil, cache, nil, nil, nil, nil)

			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			s.Router().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusOK {
				if got := atomic.LoadInt32(calls); got != 1 {
					t.Fatalf("ollama calls = %d, want 1", got)
				}
				return
			}
			if got := atomic.LoadInt32(calls); got != 0 {
				t.Fatalf("ollama calls = %d, want 0", got)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}
			var apiErr openai.APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("decode body: %v; body %s", err, rec.Body)
			}
			if !strings.Contains(apiErr.Detail.Message, tt.wantMsg) {
				t.Fatalf("message = %q, want it to contain %q", apiErr.Detail.Message, tt.wantMsg)
			}
			if apiErr.Detail.Type != openai.ErrorTypeInvalidRequest {
				t.Fatalf("type = %q, want %q", apiErr.Detail.Type, openai.ErrorTypeInvalidRequest)
			}
			if tt.wantStatus == http.StatusRequestEntityTooLarge {
				if apiErr.Detail.Code != "request_too_large" {
					t.Fatalf("code = %q, want request_too_large", apiErr.Detail.Code)
				}
				if strings.Contains(apiErr.Detail.Message, "Invalid JSON") {
					t.Fatalf("413 message mentions Invalid JSON: %q", apiErr.Detail.Message)
				}
			}
		})
	}
}
