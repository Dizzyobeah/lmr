package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Middleware returns HTTP middleware that records request metrics.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Track active connections
		ActiveConnections.Inc()
		defer ActiveConnections.Dec()
		
		// Wrap response writer to capture status code
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		
		// Process request
		next.ServeHTTP(ww, r)
		
		// Record metrics
		duration := time.Since(start).Seconds()
		
		// Get the route pattern if available
		routePattern := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
			routePattern = rctx.RoutePattern()
		}
		
		// Record request
		RecordRequest(routePattern, r.Method, ww.Status())
		
		// Record duration (use route pattern for cardinality control)
		RequestDuration.WithLabelValues(routePattern, "").Observe(duration)
		
		// Record errors
		if ww.Status() >= 400 {
			errorType := "client_error"
			if ww.Status() >= 500 {
				errorType = "server_error"
			}
			RecordError(errorType, routePattern)
		}
	})
}

// RequestMetrics records detailed metrics for a completed request.
type RequestMetrics struct {
	Endpoint         string
	Model            string
	Category         string
	RoutingReason    string
	Fallback         bool
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
	StatusCode       int
	Streaming        bool
	APIKeyID         string
}

// Record records all metrics for a request.
func (m *RequestMetrics) Record() {
	// Request count
	RecordRequest(m.Endpoint, "POST", m.StatusCode)
	
	// Model metrics
	if m.Model != "" {
		RecordModelRequest(m.Model, m.Category)
		ModelLatency.WithLabelValues(m.Model).Observe(m.Duration.Seconds())
	}
	
	// Token metrics
	if m.PromptTokens > 0 || m.CompletionTokens > 0 {
		RecordTokens(m.Model, m.PromptTokens, m.CompletionTokens)
	}
	
	// Routing metrics
	if m.RoutingReason != "" {
		RecordRouting(m.RoutingReason, m.Fallback)
	}
	
	// API key metrics
	if m.APIKeyID != "" {
		APIKeyUsage.WithLabelValues(m.APIKeyID).Inc()
	}
}

// SetHeader adds metrics-related headers to the response.
func SetHeader(w http.ResponseWriter, model string, promptTokens, completionTokens int) {
	w.Header().Set("X-Model-Used", model)
	w.Header().Set("X-Prompt-Tokens", strconv.Itoa(promptTokens))
	w.Header().Set("X-Completion-Tokens", strconv.Itoa(completionTokens))
}
