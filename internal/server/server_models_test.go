package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kalle/local-model-router/internal/config"
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
