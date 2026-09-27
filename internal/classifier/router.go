package classifier

import (
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
)

// Router selects the best model for a request.
type Router struct {
	classifier  *Classifier
	modelCache  *ollama.ModelCache
	config      config.RoutingConfig
	defaultModel string
	forceRouting bool
	virtualModelName string
}

// NewRouter creates a new router.
func NewRouter(classifier *Classifier, modelCache *ollama.ModelCache, routingConfig config.RoutingConfig) *Router {
	virtualModelName := routingConfig.VirtualModelName
	if virtualModelName == "" {
		virtualModelName = "auto"
	}
	return &Router{
		classifier:   classifier,
		modelCache:   modelCache,
		config:       routingConfig,
		defaultModel: routingConfig.DefaultModel,
		forceRouting: routingConfig.ForceRouting,
		virtualModelName: virtualModelName,
	}
}

// RouteResult contains the result of routing a request.
type RouteResult struct {
	// Model is the selected model name.
	Model string
	
	// Classification is the classification result.
	Classification *ClassificationResult
	
	// Reason explains why this model was selected.
	Reason string
	
	// Fallback indicates if this was a fallback selection.
	Fallback bool

	// Overridden indicates that the client named a concrete model but force
	// routing (or a request for the virtual model) caused the router to ignore
	// it and select a model by content instead.
	Overridden bool
}

// Route selects the best model for the given request content.
func (r *Router) Route(content string, requestedModel string) *RouteResult {
	result := &RouteResult{}

	// Determine whether to honor the requested model. When force routing is
	// enabled, or when the client asked for the virtual model name, we ignore
	// the requested model entirely and always classify/select the best one.
	honorRequested := requestedModel != "" &&
		requestedModel != "auto" &&
		!r.forceRouting &&
		requestedModel != r.virtualModelName

	// Detect an override: the client named a concrete model but we are not
	// going to honor it because of force routing / virtual model selection.
	if !honorRequested &&
		requestedModel != "" &&
		requestedModel != "auto" &&
		requestedModel != r.virtualModelName {
		result.Overridden = true
	}

	// If a specific model was requested, try to use it
	if honorRequested {
		if r.modelCache.HasModel(requestedModel) {
			result.Model = requestedModel
			result.Reason = "requested_model"
			return result
		}
		// Requested model not available, continue with auto-routing
		result.Fallback = true
	}
	
	// Classify the request
	result.Classification = r.classifier.Classify(content)
	
	// Check custom rules first
	if model, matched := r.classifier.MatchRule(content); matched {
		if r.modelCache.HasModel(model) {
			result.Model = model
			result.Reason = "custom_rule"
			return result
		}
	}
	
	// Get preferred models for this category
	preferredModels := r.getPreferredModels(result.Classification.Category)
	
	// Find the first available preferred model
	if model, found := r.modelCache.FindFirstAvailable(preferredModels); found {
		result.Model = model
		result.Reason = "category_preference:" + string(result.Classification.Category)
		return result
	}
	
	// Try the default model
	if r.defaultModel != "" && r.defaultModel != "auto" {
		if r.modelCache.HasModel(r.defaultModel) {
			result.Model = r.defaultModel
			result.Reason = "default_model"
			result.Fallback = true
			return result
		}
	}
	
	// Fall back to any available model
	models := r.modelCache.GetModelNames()
	if len(models) > 0 {
		result.Model = models[0]
		result.Reason = "any_available"
		result.Fallback = true
		return result
	}
	
	// No models available
	result.Model = ""
	result.Reason = "no_models_available"
	result.Fallback = true
	return result
}

// getPreferredModels returns the preferred models for a category.
func (r *Router) getPreferredModels(category TaskCategory) []string {
	if cap, ok := r.config.Capabilities[string(category)]; ok {
		return cap.PreferModels
	}
	return nil
}

// SetDefaultModel updates the default model.
func (r *Router) SetDefaultModel(model string) {
	r.defaultModel = model
}
