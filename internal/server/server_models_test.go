package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/gemini"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/pkg/openai"
)

func newModelsTestServer(forceRouting bool, virtualName string) *Server {
	cache := ollama.NewModelCache(nil, 0)
	cache.SetModels([]ollama.Model{
		{Name: "llama3.2"},
		{Name: "deepseek-coder"},
	})

	cfg := config.DefaultConfig()
	cfg.Routing.ForceRouting = forceRouting
	cfg.Routing.VirtualModelName = virtualName

	return &Server{
		config:     cfg,
		modelCache: cache,
	}
}

func TestHandleListModels_ForceRoutingReturnsVirtualModel(t *testing.T) {
	s := newModelsTestServer(true, "auto")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	s.handleListModels(rec, req)

	var list openai.ModelList
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(list.Data) != 1 {
		t.Fatalf("expected exactly 1 virtual model, got %d", len(list.Data))
	}
	if list.Data[0].ID != "auto" {
		t.Fatalf("expected virtual model id 'auto', got %q", list.Data[0].ID)
	}
	if list.Data[0].OwnedBy != "local-model-router" {
		t.Fatalf("expected owned_by 'local-model-router', got %q", list.Data[0].OwnedBy)
	}
}

func TestHandleListModels_NoForceRoutingReturnsAllModels(t *testing.T) {
	s := newModelsTestServer(false, "auto")

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	s.handleListModels(rec, req)

	var list openai.ModelList
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(list.Data) != 2 {
		t.Fatalf("expected all 2 Ollama models, got %d", len(list.Data))
	}
}

func TestHandleListModels_Gemini(t *testing.T) {
	for _, force := range []bool{false, true} {
		s := newModelsTestServer(force, "auto")
		s.config.Gemini.Enabled = true
		s.geminiClient = gemini.NewClient("http://127.0.0.1:1", "k", time.Second)

		rec := httptest.NewRecorder()
		s.handleListModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
		var list openai.ModelList
		if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if force {
			if len(list.Data) != 1 || list.Data[0].ID != "auto" {
				t.Fatalf("force_routing: got %+v, want only virtual model", list.Data)
			}
			continue
		}
		if len(list.Data) != 3 {
			t.Fatalf("got %d models, want 3", len(list.Data))
		}
		last := list.Data[2]
		if last.ID != "gemini-2.5-flash" || last.OwnedBy != "google" {
			t.Fatalf("gemini entry = %+v", last)
		}
	}
}
