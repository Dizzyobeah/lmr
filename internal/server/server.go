// Package server provides the HTTP server and API handlers.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kalle/local-model-router/internal/admin"
	"github.com/kalle/local-model-router/internal/classifier"
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/internal/metrics"
	"github.com/kalle/local-model-router/internal/middleware"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/pkg/openai"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// Server is the HTTP server for the Local Model Router.
type Server struct {
	config      *config.Config
	router      chi.Router
	httpServer  *http.Server
	ollamaClient *ollama.Client
	modelCache  *ollama.ModelCache
	keyStore    keystore.Store
	classifier  *classifier.Classifier
	modelRouter *classifier.Router
	adminHandler *admin.Handler
}

// NewServer creates a new HTTP server.
func NewServer(
	cfg *config.Config,
	ollamaClient *ollama.Client,
	modelCache *ollama.ModelCache,
	keyStore keystore.Store,
	classifierInstance *classifier.Classifier,
	modelRouter *classifier.Router,
	adminHandler *admin.Handler,
) *Server {
	s := &Server{
		config:       cfg,
		ollamaClient: ollamaClient,
		modelCache:   modelCache,
		keyStore:     keyStore,
		classifier:   classifierInstance,
		modelRouter:  modelRouter,
		adminHandler: adminHandler,
	}
	
	s.setupRoutes()
	
	return s
}

// setupRoutes configures the HTTP routes.
func (s *Server) setupRoutes() {
	r := chi.NewRouter()
	
	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(loggerMiddleware)
	
	// Prometheus metrics middleware (records request count, duration, errors).
	if s.config.Metrics.Enabled {
		r.Use(metrics.Middleware)
	}
	
	// Health and readiness checks (no auth)
	r.Get("/health", s.handleHealth)
	r.Get("/ready", s.handleReady)
	r.Get("/", s.handleRoot)
	
	// Prometheus metrics endpoint (no auth, standard for scraping)
	if s.config.Metrics.Enabled {
		metricsPath := s.config.Metrics.Path
		if metricsPath == "" {
			metricsPath = "/metrics"
		}
		r.Handle(metricsPath, promhttp.Handler())
	}
	
	// Admin UI (own session-based auth middleware, mounted under its path)
	if s.config.Admin.Enabled && s.adminHandler != nil {
		adminPath := s.config.Admin.Path
		if adminPath == "" {
			adminPath = "/admin"
		}
		r.Mount(adminPath, s.adminHandler.Router())
	}
	
	// API routes (with auth)
	auth := middleware.NewAuth(s.keyStore, s.config.Auth.Enabled)
	rateLimiter := middleware.NewRateLimiter(s.config.Auth.Enabled)
	
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware)
		r.Use(rateLimiter.Middleware)
		
		// OpenAI-compatible endpoints
		r.Post("/v1/chat/completions", s.handleChatCompletions)
		r.Post("/v1/completions", s.handleCompletions)
		r.Post("/v1/embeddings", s.handleEmbeddings)
		r.Get("/v1/models", s.handleListModels)
		r.Get("/v1/models/{model}", s.handleGetModel)
		
		// Compatibility aliases
		r.Post("/chat/completions", s.handleChatCompletions)
		r.Post("/completions", s.handleCompletions)
		r.Post("/embeddings", s.handleEmbeddings)
		r.Get("/models", s.handleListModels)
	})
	
	s.router = r
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.httpServer = &http.Server{
		Addr:         s.config.Server.Address(),
		Handler:      s.router,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
		IdleTimeout:  s.config.Server.IdleTimeout,
	}
	
	log.Info().Str("address", s.config.Server.Address()).Msg("Starting HTTP server")
	
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Router returns the chi router for testing or additional route mounting.
func (s *Server) Router() chi.Router {
	return s.router
}

// handleRoot handles GET /
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"service": "Local Model Router",
		"version": "1.0.0",
		"status":  "ok",
	})
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	
	ollamaHealthy := s.ollamaClient.Health(ctx) == nil
	
	status := "healthy"
	if !ollamaHealthy {
		status = "degraded"
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  status,
		"ollama":  ollamaHealthy,
		"models":  s.modelCache.Count(),
	})
}

// handleReady handles GET /ready. It returns 200 only when Ollama is reachable
// and at least one model is available, making it suitable for readiness probes.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	
	ollamaHealthy := s.ollamaClient.Health(ctx) == nil
	modelCount := s.modelCache.Count()
	ready := ollamaHealthy && modelCount > 0
	
	// Update health gauge for observability.
	if ollamaHealthy {
		metrics.OllamaHealth.Set(1)
	} else {
		metrics.OllamaHealth.Set(0)
	}
	
	w.Header().Set("Content-Type", "application/json")
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":  ready,
		"ollama": ollamaHealthy,
		"models": modelCount,
	})
}

// recordRequest records request information for the admin UI and Prometheus
// metrics. It is safe to call with a nil routeResult.
func (s *Server) recordRequest(endpoint string, routeResult *classifier.RouteResult, promptTokens, completionTokens, statusCode int, start time.Time) {
	latency := time.Since(start)
	
	model := ""
	category := "general"
	if routeResult != nil {
		model = routeResult.Model
		if routeResult.Classification != nil {
			category = string(routeResult.Classification.Category)
		}
		if routeResult.Reason != "" {
			metrics.RecordRouting(routeResult.Reason, routeResult.Fallback)
		}
		if routeResult.Overridden && model != "" {
			metrics.RecordModelOverride(model, category)
		}
	}
	
	if model != "" {
		metrics.RecordModelRequest(model, category)
		metrics.RecordTokens(model, promptTokens, completionTokens)
		metrics.ModelLatency.WithLabelValues(model).Observe(latency.Seconds())
	}
	
	if s.adminHandler != nil {
		s.adminHandler.RecordRequest(admin.RecentRequest{
			Time:       start,
			Endpoint:   endpoint,
			Model:      model,
			Category:   category,
			Tokens:     promptTokens + completionTokens,
			LatencyMs:  latency.Milliseconds(),
			StatusCode: statusCode,
		})
	}
}

// handleChatCompletions handles POST /v1/chat/completions
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	
	// Parse request
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openai.WriteError(w, openai.ErrInvalidRequest("Invalid JSON: "+err.Error()))
		return
	}
	
	// Extract content for classification
	content := extractChatContent(req.Messages)
	
	// Route to model
	routeResult := s.modelRouter.Route(content, req.Model)
	if routeResult.Model == "" {
		openai.WriteError(w, openai.ErrModelNotFound("No models available"))
		s.recordRequest("/v1/chat/completions", routeResult, 0, 0, http.StatusNotFound, start)
		return
	}
	
	category := "general"
	if routeResult.Classification != nil {
		category = string(routeResult.Classification.Category)
	}
	log.Debug().
		Str("requested_model", req.Model).
		Str("selected_model", routeResult.Model).
		Str("reason", routeResult.Reason).
		Str("category", category).
		Msg("Request routed")
	
	// Convert to Ollama request
	ollamaReq := &ollama.ChatRequest{
		Model:    routeResult.Model,
		Messages: convertMessages(req.Messages),
		Stream:   req.Stream,
		Tools:    convertTools(req.Tools),
		Options:  buildOllamaOptions(&req),
	}
	
	// Handle streaming vs non-streaming
	if req.Stream {
		s.handleChatStream(w, r, ollamaReq, routeResult, start)
	} else {
		s.handleChatNonStream(w, r, ollamaReq, routeResult, start)
	}
}

// handleChatNonStream handles non-streaming chat completions.
func (s *Server) handleChatNonStream(w http.ResponseWriter, r *http.Request, ollamaReq *ollama.ChatRequest, routeResult *classifier.RouteResult, start time.Time) {
	ollamaReq.Stream = false
	
	resp, err := s.ollamaClient.Chat(r.Context(), ollamaReq)
	if err != nil {
		log.Error().Err(err).Msg("Ollama chat failed")
		openai.WriteError(w, openai.ErrInternal("Chat completion failed"))
		s.recordRequest("/v1/chat/completions", routeResult, 0, 0, http.StatusInternalServerError, start)
		return
	}
	
	// Convert to OpenAI response
	toolCalls := convertToolCalls(resp.Message.ToolCalls)
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	openaiResp := &openai.ChatCompletionResponse{
		ID:      generateID("chatcmpl"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   routeResult.Model,
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatMessage{
					Role:      resp.Message.Role,
					Content:   resp.Message.Content,
					ToolCalls: toolCalls,
				},
				FinishReason: &finishReason,
			},
		},
		Usage: &openai.Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)
	
	s.recordRequest("/v1/chat/completions", routeResult, resp.PromptEvalCount, resp.EvalCount, http.StatusOK, start)
}

// handleChatStream handles streaming chat completions.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request, ollamaReq *ollama.ChatRequest, routeResult *classifier.RouteResult, start time.Time) {
	ollamaReq.Stream = true
	
	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		openai.WriteError(w, openai.ErrInternal("Streaming not supported"))
		s.recordRequest("/v1/chat/completions", routeResult, 0, 0, http.StatusInternalServerError, start)
		return
	}
	
	metrics.StreamingRequests.Inc()
	defer metrics.StreamingRequests.Dec()
	
	responseChan, errorChan := s.ollamaClient.ChatStream(r.Context(), ollamaReq)
	
	id := generateID("chatcmpl")
	var promptTokens, completionTokens int
	var sawToolCalls bool
	
	for {
		select {
		case resp, ok := <-responseChan:
			if !ok {
				// Channel closed, send done message
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				s.recordRequest("/v1/chat/completions", routeResult, promptTokens, completionTokens, http.StatusOK, start)
				return
			}
			
			// Capture token counts from the final message.
			if resp.PromptEvalCount > 0 {
				promptTokens = resp.PromptEvalCount
			}
			if resp.EvalCount > 0 {
				completionTokens = resp.EvalCount
			}
			
			chunk := &openai.ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   routeResult.Model,
				Choices: []openai.ChatCompletionChunkChoice{
					{
						Index: 0,
						Delta: openai.ChatMessageDelta{
							Role:      resp.Message.Role,
							Content:   resp.Message.Content,
							ToolCalls: convertToolCallDeltas(resp.Message.ToolCalls),
						},
					},
				},
			}
			
			if len(resp.Message.ToolCalls) > 0 {
				sawToolCalls = true
			}
			
			if resp.Done {
				reason := "stop"
				if sawToolCalls {
					reason = "tool_calls"
				}
				chunk.Choices[0].FinishReason = &reason
			}
			
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			
		case err := <-errorChan:
			if err != nil {
				log.Error().Err(err).Msg("Stream error")
				// Can't send proper error in SSE, just close
				s.recordRequest("/v1/chat/completions", routeResult, promptTokens, completionTokens, http.StatusInternalServerError, start)
				return
			}
			
		case <-r.Context().Done():
			return
		}
	}
}

// handleCompletions handles POST /v1/completions (legacy completions API)
func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	
	var req openai.CompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openai.WriteError(w, openai.ErrInvalidRequest("Invalid JSON: "+err.Error()))
		return
	}
	
	// Route to model
	routeResult := s.modelRouter.Route(req.Prompt, req.Model)
	if routeResult.Model == "" {
		openai.WriteError(w, openai.ErrModelNotFound("No models available"))
		s.recordRequest("/v1/completions", routeResult, 0, 0, http.StatusNotFound, start)
		return
	}
	
	// Convert to Ollama generate request
	ollamaReq := &ollama.GenerateRequest{
		Model:  routeResult.Model,
		Prompt: req.Prompt,
		Stream: req.Stream,
		Options: map[string]interface{}{
			"temperature": req.Temperature,
			"num_predict": req.MaxTokens,
		},
	}
	
	if req.Stream {
		s.handleCompletionStream(w, r, ollamaReq, routeResult, start)
	} else {
		s.handleCompletionNonStream(w, r, ollamaReq, routeResult, start)
	}
}

// handleCompletionNonStream handles non-streaming completions.
func (s *Server) handleCompletionNonStream(w http.ResponseWriter, r *http.Request, ollamaReq *ollama.GenerateRequest, routeResult *classifier.RouteResult, start time.Time) {
	ollamaReq.Stream = false
	
	resp, err := s.ollamaClient.Generate(r.Context(), ollamaReq)
	if err != nil {
		log.Error().Err(err).Msg("Ollama generate failed")
		openai.WriteError(w, openai.ErrInternal("Completion failed"))
		s.recordRequest("/v1/completions", routeResult, 0, 0, http.StatusInternalServerError, start)
		return
	}
	
	openaiResp := &openai.CompletionResponse{
		ID:      generateID("cmpl"),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   routeResult.Model,
		Choices: []openai.CompletionChoice{
			{
				Index:        0,
				Text:         resp.Response,
				FinishReason: "stop",
			},
		},
		Usage: &openai.Usage{
			PromptTokens:     resp.PromptEvalCount,
			CompletionTokens: resp.EvalCount,
			TotalTokens:      resp.PromptEvalCount + resp.EvalCount,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)
	
	s.recordRequest("/v1/completions", routeResult, resp.PromptEvalCount, resp.EvalCount, http.StatusOK, start)
}

// handleCompletionStream handles streaming completions.
func (s *Server) handleCompletionStream(w http.ResponseWriter, r *http.Request, ollamaReq *ollama.GenerateRequest, routeResult *classifier.RouteResult, start time.Time) {
	ollamaReq.Stream = true
	
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		openai.WriteError(w, openai.ErrInternal("Streaming not supported"))
		s.recordRequest("/v1/completions", routeResult, 0, 0, http.StatusInternalServerError, start)
		return
	}
	
	metrics.StreamingRequests.Inc()
	defer metrics.StreamingRequests.Dec()
	
	responseChan, errorChan := s.ollamaClient.GenerateStream(r.Context(), ollamaReq)
	
	id := generateID("cmpl")
	var promptTokens, completionTokens int
	
	for {
		select {
		case resp, ok := <-responseChan:
			if !ok {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				s.recordRequest("/v1/completions", routeResult, promptTokens, completionTokens, http.StatusOK, start)
				return
			}
			
			if resp.PromptEvalCount > 0 {
				promptTokens = resp.PromptEvalCount
			}
			if resp.EvalCount > 0 {
				completionTokens = resp.EvalCount
			}
			
			chunk := map[string]interface{}{
				"id":      id,
				"object":  "text_completion",
				"created": time.Now().Unix(),
				"model":   routeResult.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"text":  resp.Response,
					},
				},
			}
			
			if resp.Done {
				chunk["choices"].([]map[string]interface{})[0]["finish_reason"] = "stop"
			}
			
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			
		case err := <-errorChan:
			if err != nil {
				log.Error().Err(err).Msg("Stream error")
				s.recordRequest("/v1/completions", routeResult, promptTokens, completionTokens, http.StatusInternalServerError, start)
				return
			}
			
		case <-r.Context().Done():
			return
		}
	}
}

// handleEmbeddings handles POST /v1/embeddings
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req openai.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		openai.WriteError(w, openai.ErrInvalidRequest("Invalid JSON: "+err.Error()))
		return
	}
	
	// Get input text
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	default:
		openai.WriteError(w, openai.ErrInvalidRequest("Invalid input format"))
		return
	}
	
	// Use requested model or find an embedding model
	model := req.Model
	if model == "" || model == "auto" {
		// Try to find an embedding model
		embeddingModels := []string{"nomic-embed-text", "mxbai-embed-large", "all-minilm"}
		if found, ok := s.modelCache.FindFirstAvailable(embeddingModels); ok {
			model = found
		} else {
			// Fall back to any model
			models := s.modelCache.GetModelNames()
			if len(models) > 0 {
				model = models[0]
			} else {
				openai.WriteError(w, openai.ErrModelNotFound("No embedding models available"))
				return
			}
		}
	}
	
	// Generate embeddings
	var embeddings []openai.Embedding
	var totalTokens int
	
	for i, input := range inputs {
		resp, err := s.ollamaClient.Embedding(r.Context(), &ollama.EmbeddingRequest{
			Model:  model,
			Prompt: input,
		})
		if err != nil {
			log.Error().Err(err).Msg("Ollama embedding failed")
			openai.WriteError(w, openai.ErrInternal("Embedding generation failed"))
			return
		}
		
		embeddings = append(embeddings, openai.Embedding{
			Object:    "embedding",
			Index:     i,
			Embedding: resp.Embedding,
		})
		
		totalTokens += len(input) / 4 // Rough estimate
	}
	
	openaiResp := &openai.EmbeddingResponse{
		Object: "list",
		Data:   embeddings,
		Model:  model,
		Usage: openai.EmbeddingUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)
}

// handleListModels handles GET /v1/models
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	// When force routing is enabled, present a single virtual model so that
	// clients (e.g. OpenCode) select it once and all requests are routed
	// intelligently rather than pinned to a specific Ollama model.
	if s.config.Routing.ForceRouting {
		resp := &openai.ModelList{
			Object: "list",
			Data: []openai.Model{
				{
					ID:      s.virtualModelName(),
					Object:  "model",
					Created: time.Now().Unix(),
					OwnedBy: "local-model-router",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	models := s.modelCache.GetModels()
	
	var modelList []openai.Model
	for _, m := range models {
		modelList = append(modelList, openai.Model{
			ID:      m.Name,
			Object:  "model",
			Created: m.ModifiedAt.Unix(),
			OwnedBy: "ollama",
		})
	}
	
	resp := &openai.ModelList{
		Object: "list",
		Data:   modelList,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGetModel handles GET /v1/models/{model}
func (s *Server) handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelName := chi.URLParam(r, "model")

	// When force routing is enabled, resolve the virtual model name so clients
	// can verify the single advertised model.
	if s.config.Routing.ForceRouting && modelName == s.virtualModelName() {
		resp := &openai.Model{
			ID:      s.virtualModelName(),
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "local-model-router",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	model, ok := s.modelCache.GetModel(modelName)
	if !ok {
		openai.WriteError(w, openai.ErrModelNotFound(modelName))
		return
	}
	
	resp := &openai.Model{
		ID:      model.Name,
		Object:  "model",
		Created: model.ModifiedAt.Unix(),
		OwnedBy: "ollama",
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Helper functions

// virtualModelName returns the configured virtual model name, defaulting to
// "auto" when unset.
func (s *Server) virtualModelName() string {
	if name := s.config.Routing.VirtualModelName; name != "" {
		return name
	}
	return "auto"
}

func extractChatContent(messages []openai.ChatMessage) string {
	var parts []string
	for _, msg := range messages {
		if msg.Content != "" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func convertMessages(messages []openai.ChatMessage) []ollama.ChatMessage {
	var result []ollama.ChatMessage
	for _, msg := range messages {
		om := ollama.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Forward any image parts to the backend. Ollama's /api/chat expects
		// raw base64 in the `images` array, so strip the data-URI prefix
		// (e.g. "data:image/png;base64,") that OpenAI-style clients include.
		for _, img := range msg.Images {
			if b64 := stripDataURIPrefix(img); b64 != "" {
				om.Images = append(om.Images, b64)
			}
		}
		
		// Assistant messages may carry tool calls that must be replayed to
		// the model. OpenAI encodes arguments as a JSON string; Ollama expects
		// a JSON value, so pass the string through as raw JSON.
		for _, tc := range msg.ToolCalls {
			args := json.RawMessage(tc.Function.Arguments)
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			om.ToolCalls = append(om.ToolCalls, ollama.ToolCall{
				Function: ollama.ToolCallFunction{
					Name:      tc.Function.Name,
					Arguments: args,
				},
			})
		}
		
		// Tool-result messages: OpenAI identifies the call by tool_call_id.
		// Ollama identifies it by tool_name; we don't have the original name
		// here, so fall back to the call id which keeps the association stable.
		if msg.Role == "tool" && msg.ToolCallID != "" {
			om.ToolName = msg.ToolCallID
		}
		
		result = append(result, om)
	}
	return result
}

// stripDataURIPrefix returns the raw base64 payload of a data URI. Ollama's
// image field wants base64 without the "data:<mime>;base64," scheme prefix.
// If the input carries no such prefix it is returned unchanged (already raw
// base64); plain remote URLs, which Ollama cannot fetch, are also passed
// through as-is so the backend can decide how to handle them.
func stripDataURIPrefix(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "data:") {
		return s
	}
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[i+1:]
	}
	return s
}

// convertTools maps OpenAI tool definitions to the Ollama tool schema.
func convertTools(tools []openai.Tool) []ollama.Tool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]ollama.Tool, 0, len(tools))
	for _, t := range tools {
		result = append(result, ollama.Tool{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}
	return result
}

// toolCallArguments returns the tool-call arguments as a JSON string, defaulting
// to "{}" when empty. Ollama returns arguments as a JSON object; OpenAI clients
// expect a JSON-encoded string.
func toolCallArguments(raw json.RawMessage) string {
	args := strings.TrimSpace(string(raw))
	if args == "" {
		return "{}"
	}
	return args
}

// convertToolCalls maps Ollama tool calls to OpenAI tool calls, synthesizing
// stable IDs (Ollama does not provide them).
func convertToolCalls(calls []ollama.ToolCall) []openai.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	result := make([]openai.ToolCall, 0, len(calls))
	for i, c := range calls {
		result = append(result, openai.ToolCall{
			ID:   fmt.Sprintf("call_%d", i),
			Type: "function",
			Function: openai.ToolCallFunction{
				Name:      c.Function.Name,
				Arguments: toolCallArguments(c.Function.Arguments),
			},
		})
	}
	return result
}

// convertToolCallDeltas maps Ollama tool calls to OpenAI streaming tool-call
// deltas.
func convertToolCallDeltas(calls []ollama.ToolCall) []openai.ToolCallDelta {
	if len(calls) == 0 {
		return nil
	}
	result := make([]openai.ToolCallDelta, 0, len(calls))
	for i, c := range calls {
		result = append(result, openai.ToolCallDelta{
			Index: i,
			ID:    fmt.Sprintf("call_%d", i),
			Type:  "function",
			Function: &openai.ToolCallFunctionDelta{
				Name:      c.Function.Name,
				Arguments: toolCallArguments(c.Function.Arguments),
			},
		})
	}
	return result
}

func buildOllamaOptions(req *openai.ChatCompletionRequest) map[string]interface{} {
	opts := make(map[string]interface{})
	
	if req.Temperature != nil {
		opts["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		opts["top_p"] = *req.TopP
	}
	if req.MaxTokens != nil {
		opts["num_predict"] = *req.MaxTokens
	}
	if req.Stop != nil {
		opts["stop"] = req.Stop
	}
	if req.PresencePenalty != nil {
		opts["presence_penalty"] = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		opts["frequency_penalty"] = *req.FrequencyPenalty
	}
	
	// Agent clients (e.g. OpenCode) send large system prompts and tool schemas.
	// Ollama's default context window is small and silently truncates them, so
	// raise it to a sane default unless the caller specified otherwise.
	opts["num_ctx"] = 32768
	
	return opts
}

func generateID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		
		defer func() {
			log.Debug().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("duration", time.Since(start)).
				Msg("Request completed")
		}()
		
		next.ServeHTTP(ww, r)
	})
}
