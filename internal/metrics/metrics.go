// Package metrics provides Prometheus metrics for the Local Model Router.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal is the total number of requests processed.
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "requests_total",
			Help:      "Total number of requests processed",
		},
		[]string{"endpoint", "method", "status"},
	)

	// RequestDuration is the request duration in seconds.
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lmr",
			Name:      "request_duration_seconds",
			Help:      "Request duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
		},
		[]string{"endpoint", "model"},
	)

	// TokensProcessed is the total number of tokens processed.
	TokensProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "tokens_processed_total",
			Help:      "Total number of tokens processed",
		},
		[]string{"model", "type"}, // type: prompt, completion
	)

	// ModelsAvailable is the number of models available in Ollama.
	ModelsAvailable = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lmr",
			Name:      "models_available",
			Help:      "Number of models available in Ollama",
		},
	)

	// ModelRequests is the total requests per model.
	ModelRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "model_requests_total",
			Help:      "Total requests per model",
		},
		[]string{"model", "category"},
	)

	// ModelLatency is the model response latency in seconds.
	ModelLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "lmr",
			Name:      "model_latency_seconds",
			Help:      "Model response latency in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 12), // 100ms to ~200s
		},
		[]string{"model"},
	)

	// ClassificationResults tracks classification results by category.
	ClassificationResults = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "classification_results_total",
			Help:      "Classification results by category",
		},
		[]string{"category", "confidence_bucket"},
	)

	// RoutingDecisions tracks routing decisions.
	RoutingDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "routing_decisions_total",
			Help:      "Routing decisions by reason",
		},
		[]string{"reason", "fallback"},
	)

	// ForceRoutingEnabled indicates whether force routing is enabled
	// (1 = enabled, 0 = disabled). When enabled, the model named in a request
	// is ignored and the router always selects the best model by content.
	ForceRoutingEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lmr",
			Name:      "force_routing_enabled",
			Help:      "Whether force routing is enabled (1 = enabled, 0 = disabled)",
		},
	)

	// ModelOverrides counts requests where the router ignored the client's
	// requested model and selected a different one due to force routing.
	ModelOverrides = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "model_overrides_total",
			Help:      "Requests where the requested model was overridden by force routing",
		},
		[]string{"selected_model", "category"},
	)

	// APIKeyUsage tracks API key usage.
	APIKeyUsage = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "api_key_usage_total",
			Help:      "API key usage by key ID",
		},
		[]string{"key_id"},
	)

	// ActiveConnections is the number of active connections.
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lmr",
			Name:      "active_connections",
			Help:      "Number of active connections",
		},
	)

	// StreamingRequests is the number of active streaming requests.
	StreamingRequests = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lmr",
			Name:      "streaming_requests",
			Help:      "Number of active streaming requests",
		},
	)

	// OllamaHealth indicates Ollama health status (1 = healthy, 0 = unhealthy).
	OllamaHealth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "lmr",
			Name:      "ollama_health",
			Help:      "Ollama health status (1 = healthy, 0 = unhealthy)",
		},
	)

	// ErrorsTotal is the total number of errors.
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "errors_total",
			Help:      "Total number of errors",
		},
		[]string{"type", "endpoint"},
	)

	// RateLimitHits is the number of rate limit hits.
	RateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "rate_limit_hits_total",
			Help:      "Number of rate limit hits",
		},
		[]string{"key_id"},
	)

	// CacheHits tracks model cache hits.
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "cache_hits_total",
			Help:      "Number of model cache hits",
		},
	)

	// CacheMisses tracks model cache misses.
	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "lmr",
			Name:      "cache_misses_total",
			Help:      "Number of model cache misses",
		},
	)
)

// ConfidenceBucket returns a bucket label for a confidence value.
func ConfidenceBucket(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence >= 0.7:
		return "medium"
	case confidence >= 0.5:
		return "low"
	default:
		return "very_low"
	}
}

// RecordRequest records a request with its metrics.
func RecordRequest(endpoint, method string, statusCode int) {
	status := "2xx"
	switch {
	case statusCode >= 500:
		status = "5xx"
	case statusCode >= 400:
		status = "4xx"
	case statusCode >= 300:
		status = "3xx"
	}
	RequestsTotal.WithLabelValues(endpoint, method, status).Inc()
}

// RecordModelRequest records a model request.
func RecordModelRequest(model, category string) {
	ModelRequests.WithLabelValues(model, category).Inc()
}

// RecordTokens records token usage.
func RecordTokens(model string, promptTokens, completionTokens int) {
	TokensProcessed.WithLabelValues(model, "prompt").Add(float64(promptTokens))
	TokensProcessed.WithLabelValues(model, "completion").Add(float64(completionTokens))
}

// RecordClassification records a classification result.
func RecordClassification(category string, confidence float64) {
	ClassificationResults.WithLabelValues(category, ConfidenceBucket(confidence)).Inc()
}

// RecordRouting records a routing decision.
func RecordRouting(reason string, fallback bool) {
	fb := "false"
	if fallback {
		fb = "true"
	}
	RoutingDecisions.WithLabelValues(reason, fb).Inc()
}

// SetForceRouting records whether force routing is enabled.
func SetForceRouting(enabled bool) {
	if enabled {
		ForceRoutingEnabled.Set(1)
	} else {
		ForceRoutingEnabled.Set(0)
	}
}

// RecordModelOverride records that the requested model was overridden by force
// routing in favor of the selected model.
func RecordModelOverride(selectedModel, category string) {
	ModelOverrides.WithLabelValues(selectedModel, category).Inc()
}

// RecordError records an error.
func RecordError(errorType, endpoint string) {
	ErrorsTotal.WithLabelValues(errorType, endpoint).Inc()
}
