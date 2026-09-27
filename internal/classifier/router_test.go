package classifier

import (
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
)

// newTestRouter builds a Router backed by a seeded model cache and a classifier
// that recognizes "code" requests.
func newTestRouter(t *testing.T, routingCfg config.RoutingConfig) *Router {
	t.Helper()

	clf, err := NewClassifier(Config{
		Capabilities: map[string]CapabilityConfig{
			"code": {Keywords: []string{"code", "function", "implement"}},
		},
	})
	if err != nil {
		t.Fatalf("NewClassifier: %v", err)
	}

	cache := ollama.NewModelCache(nil, 0)
	cache.SetModels([]ollama.Model{
		{Name: "llama3.2"},
		{Name: "deepseek-coder"},
	})

	if routingCfg.Capabilities == nil {
		routingCfg.Capabilities = map[string]config.CapabilityConfig{
			"code": {PreferModels: []string{"deepseek-coder"}},
		}
	}

	return NewRouter(clf, cache, routingCfg)
}

func TestRoute_ForceRoutingIgnoresRequestedModel(t *testing.T) {
	r := newTestRouter(t, config.RoutingConfig{
		DefaultModel:     "auto",
		ForceRouting:     true,
		VirtualModelName: "auto",
	})

	// Client asks for a valid, available model, but content is a code task.
	res := r.Route("please implement a function to sort a list", "llama3.2")

	if res.Model != "deepseek-coder" {
		t.Fatalf("expected forced routing to deepseek-coder, got %q (reason %q)", res.Model, res.Reason)
	}
	if res.Reason == "requested_model" {
		t.Fatalf("force routing must not honor the requested model")
	}
	if !res.Overridden {
		t.Fatalf("expected Overridden to be true when force routing ignores a concrete model")
	}
}

func TestRoute_HonorsRequestedModelWhenForceRoutingOff(t *testing.T) {
	r := newTestRouter(t, config.RoutingConfig{
		DefaultModel:     "auto",
		ForceRouting:     false,
		VirtualModelName: "auto",
	})

	res := r.Route("please implement a function to sort a list", "llama3.2")

	if res.Model != "llama3.2" {
		t.Fatalf("expected requested model llama3.2, got %q", res.Model)
	}
	if res.Reason != "requested_model" {
		t.Fatalf("expected reason requested_model, got %q", res.Reason)
	}
	if res.Overridden {
		t.Fatalf("expected Overridden to be false when honoring the requested model")
	}
}

func TestRoute_VirtualModelNameAlwaysRoutes(t *testing.T) {
	// Even with force routing off, asking for the virtual model name must route.
	r := newTestRouter(t, config.RoutingConfig{
		DefaultModel:     "auto",
		ForceRouting:     false,
		VirtualModelName: "router",
	})

	res := r.Route("please implement a function to sort a list", "router")

	if res.Reason == "requested_model" {
		t.Fatalf("virtual model name must trigger routing, got reason %q", res.Reason)
	}
	if res.Model != "deepseek-coder" {
		t.Fatalf("expected routed model deepseek-coder, got %q", res.Model)
	}
}
