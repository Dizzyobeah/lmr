package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/internal/ollama"
)

func TestHandleListKeysEscapes(t *testing.T) {
	tests := []struct {
		name    string
		req     keystore.CreateKeyRequest
		want    string
		notWant string
	}{
		{"name", keystore.CreateKeyRequest{Name: "<img src=x onerror=alert(1)>"}, "&lt;img src=x onerror=alert(1)&gt;", "<img"},
		{"scopes", keystore.CreateKeyRequest{Name: "k", Scopes: []string{"<script>"}}, "&lt;script&gt;", "<script>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, store := newKeysTestHandler(t)
			if _, _, err := store.Create(context.Background(), tt.req); err != nil {
				t.Fatalf("Create: %v", err)
			}
			body := listKeysBody(t, h)
			if !strings.Contains(body, tt.want) {
				t.Errorf("body missing %q:\n%s", tt.want, body)
			}
			if strings.Contains(body, tt.notWant) {
				t.Errorf("body contains unescaped %q:\n%s", tt.notWant, body)
			}
		})
	}
}

func TestHandleModelsEscapes(t *testing.T) {
	cache := ollama.NewModelCache(nil, 0)
	cache.SetModels([]ollama.Model{{
		Name:    "<b>name",
		Details: ollama.ModelDetails{Family: "<b>fam", QuantizationLevel: "<b>q"},
	}})
	h := NewHandler(&config.AdminConfig{}, nil, cache, nil, nil)
	body := serve(t, h.handleModels)
	for _, want := range []string{"&lt;b&gt;name", "&lt;b&gt;fam", "&lt;b&gt;q"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<b>") {
		t.Errorf("body contains unescaped <b>:\n%s", body)
	}
}

func TestRecentRequestsEscapes(t *testing.T) {
	h := NewHandler(&config.AdminConfig{}, nil, nil, nil, nil)
	h.RecordRequest(RecentRequest{Endpoint: "<e>", Model: "<m>", Category: "<c>"})
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    []string
	}{
		{"requests", h.handleRequests, []string{"&lt;e&gt;", "&lt;m&gt;", "&lt;c&gt;"}},
		{"model usage", h.handleModelUsage, []string{"&lt;m&gt;"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := serve(t, tt.handler)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q:\n%s", want, body)
				}
			}
			for _, raw := range []string{"<e>", "<m>", "<c>"} {
				if strings.Contains(body, raw) {
					t.Errorf("body contains unescaped %q:\n%s", raw, body)
				}
			}
		})
	}
}

func serve(t *testing.T, fn http.HandlerFunc) string {
	t.Helper()
	rec := httptest.NewRecorder()
	fn(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}
