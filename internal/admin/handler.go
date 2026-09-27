// Package admin provides the admin UI handlers.
package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kalle/local-model-router/internal/classifier"
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/crypto"
	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/internal/ollama"
)

//go:embed templates/*
var templatesFS embed.FS

// RecentRequest stores information about a recent request.
type RecentRequest struct {
	Time       time.Time `json:"time"`
	Endpoint   string    `json:"endpoint"`
	Model      string    `json:"model"`
	Category   string    `json:"category"`
	Tokens     int       `json:"tokens"`
	LatencyMs  int64     `json:"latency_ms"`
	StatusCode int       `json:"status_code"`
}

// Handler provides admin UI endpoints.
type Handler struct {
	config         *config.AdminConfig
	keyStore       keystore.Store
	modelCache     *ollama.ModelCache
	ollamaClient   *ollama.Client
	modelRouter    *classifier.Router
	
	// Recent requests buffer
	recentRequests []RecentRequest
	requestsMu     sync.RWMutex
	maxRequests    int
	
	// Statistics
	totalRequests   int64
	totalTokens     int64
	totalLatencyMs  int64
	statsMu         sync.RWMutex
	
	// Session management
	sessions   map[string]time.Time
	sessionsMu sync.RWMutex
}

// NewHandler creates a new admin handler.
func NewHandler(cfg *config.AdminConfig, keyStore keystore.Store, modelCache *ollama.ModelCache, ollamaClient *ollama.Client, modelRouter *classifier.Router) *Handler {
	maxRequests := cfg.RecentRequestsLimit
	if maxRequests <= 0 {
		maxRequests = 1000
	}
	
	return &Handler{
		config:         cfg,
		keyStore:       keyStore,
		modelCache:     modelCache,
		ollamaClient:   ollamaClient,
		modelRouter:    modelRouter,
		recentRequests: make([]RecentRequest, 0, maxRequests),
		maxRequests:    maxRequests,
		sessions:       make(map[string]time.Time),
	}
}

// Router returns the admin routes.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()
	
	// Auth middleware for admin routes
	r.Use(h.authMiddleware)
	
	// Main admin page
	r.Get("/", h.handleIndex)
	
	// Chat testing UI
	r.Get("/chat", h.handleChatPage)
	
	// API endpoints for htmx
	r.Route("/api", func(r chi.Router) {
		r.Get("/status", h.handleStatus)
		r.Get("/stats", h.handleStats)
		r.Get("/models", h.handleModels)
		r.Get("/model-usage", h.handleModelUsage)
		r.Get("/classification-stats", h.handleClassificationStats)
		r.Get("/keys", h.handleListKeys)
		r.Post("/keys", h.handleCreateKey)
		r.Delete("/keys/{id}", h.handleDeleteKey)
		r.Get("/requests", h.handleRequests)
		r.Post("/chat", h.handleChatAPI)
	})
	
	// Login/logout
	r.Get("/login", h.handleLoginPage)
	r.Post("/login", h.handleLogin)
	r.Post("/logout", h.handleLogout)
	
	return r
}

// RecordRequest records a request for display in the admin UI.
func (h *Handler) RecordRequest(req RecentRequest) {
	h.requestsMu.Lock()
	defer h.requestsMu.Unlock()
	
	// Add to front
	h.recentRequests = append([]RecentRequest{req}, h.recentRequests...)
	
	// Trim to max size
	if len(h.recentRequests) > h.maxRequests {
		h.recentRequests = h.recentRequests[:h.maxRequests]
	}
	
	// Update stats
	h.statsMu.Lock()
	h.totalRequests++
	h.totalTokens += int64(req.Tokens)
	h.totalLatencyMs += req.LatencyMs
	h.statsMu.Unlock()
}

// authMiddleware checks admin authentication.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow login page
		if r.URL.Path == h.config.Path+"/login" {
			next.ServeHTTP(w, r)
			return
		}
		
		// Check session cookie
		cookie, err := r.Cookie("admin_session")
		if err != nil {
			http.Redirect(w, r, h.config.Path+"/login", http.StatusFound)
			return
		}
		
		h.sessionsMu.RLock()
		expiry, exists := h.sessions[cookie.Value]
		h.sessionsMu.RUnlock()
		
		if !exists || time.Now().After(expiry) {
			http.Redirect(w, r, h.config.Path+"/login", http.StatusFound)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// handleIndex serves the admin dashboard.
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templatesFS, "templates/admin.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, nil)
}

// chatPageData is the template data for the chat testing UI.
type chatPageData struct {
	AdminPath string
	Models    []string
}

// handleChatPage serves the chat testing UI.
func (h *Handler) handleChatPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templatesFS, "templates/chat.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}
	
	data := chatPageData{
		AdminPath: h.config.Path,
		Models:    h.modelCache.GetModelNames(),
	}
	
	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
}

// chatAPIRequest is the request body for the chat testing endpoint.
type chatAPIRequest struct {
	Message string `json:"message"`
	Model   string `json:"model"`
}

// chatAPIResponse is the response body for the chat testing endpoint.
type chatAPIResponse struct {
	Content          string `json:"content"`
	Model            string `json:"model"`
	Category         string `json:"category"`
	Reason           string `json:"reason"`
	Fallback         bool   `json:"fallback"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	LatencyMs        int64  `json:"latency_ms"`
}

// handleChatAPI processes a chat message from the testing UI. It routes the
// message through the model router and returns the response together with the
// routing decision metadata so the user can verify routing behaviour.
func (h *Handler) handleChatAPI(w http.ResponseWriter, r *http.Request) {
	var req chatAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	
	if strings.TrimSpace(req.Message) == "" {
		writeChatError(w, http.StatusBadRequest, "Message cannot be empty")
		return
	}
	
	if h.modelRouter == nil {
		writeChatError(w, http.StatusServiceUnavailable, "Routing is not available")
		return
	}
	
	requestedModel := req.Model
	if requestedModel == "" {
		requestedModel = "auto"
	}
	
	start := time.Now()
	
	// Route the request to a model.
	routeResult := h.modelRouter.Route(req.Message, requestedModel)
	if routeResult.Model == "" {
		writeChatError(w, http.StatusServiceUnavailable, "No models available. Ensure Ollama is running and has models loaded.")
		return
	}
	
	category := "general"
	if routeResult.Classification != nil {
		category = string(routeResult.Classification.Category)
	}
	
	// Build the message list.
	messages := []ollama.ChatMessage{
		{Role: "user", Content: req.Message},
	}
	
	// Call Ollama (non-streaming for simplicity and reliable metadata).
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	
	resp, err := h.ollamaClient.Chat(ctx, &ollama.ChatRequest{
		Model:    routeResult.Model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		h.RecordRequest(RecentRequest{
			Time:       start,
			Endpoint:   "/admin/chat",
			Model:      routeResult.Model,
			Category:   category,
			LatencyMs:  time.Since(start).Milliseconds(),
			StatusCode: http.StatusInternalServerError,
		})
		writeChatError(w, http.StatusInternalServerError, "Chat completion failed: "+err.Error())
		return
	}
	
	latency := time.Since(start)
	totalTokens := resp.PromptEvalCount + resp.EvalCount
	
	// Record for the dashboard and metrics views.
	h.RecordRequest(RecentRequest{
		Time:       start,
		Endpoint:   "/admin/chat",
		Model:      routeResult.Model,
		Category:   category,
		Tokens:     totalTokens,
		LatencyMs:  latency.Milliseconds(),
		StatusCode: http.StatusOK,
	})
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatAPIResponse{
		Content:          resp.Message.Content,
		Model:            routeResult.Model,
		Category:         category,
		Reason:           routeResult.Reason,
		Fallback:         routeResult.Fallback,
		PromptTokens:     resp.PromptEvalCount,
		CompletionTokens: resp.EvalCount,
		TotalTokens:      totalTokens,
		LatencyMs:        latency.Milliseconds(),
	})
}

// writeChatError writes a JSON error response for the chat testing endpoint.
func writeChatError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// handleLoginPage serves the login page.
func (h *Handler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Login - Local Model Router</title>
    <style>
        body { font-family: sans-serif; background: #1a1a2e; color: #eaeaea; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }
        .login-form { background: #16213e; padding: 30px; border-radius: 12px; width: 300px; }
        .login-form h1 { margin: 0 0 20px; font-size: 1.5rem; color: #e94560; }
        .login-form input { width: 100%; padding: 10px; margin-bottom: 15px; border: 1px solid #374151; background: #0f3460; color: #eaeaea; border-radius: 6px; box-sizing: border-box; }
        .login-form button { width: 100%; padding: 10px; background: #e94560; color: white; border: none; border-radius: 6px; cursor: pointer; }
        .login-form button:hover { background: #ff6b6b; }
        .error { color: #ef4444; margin-bottom: 15px; }
    </style>
</head>
<body>
    <form class="login-form" method="POST">
        <h1>Admin Login</h1>
        <input type="text" name="username" placeholder="Username" required>
        <input type="password" name="password" placeholder="Password" required>
        <button type="submit">Login</button>
    </form>
</body>
</html>`))
}

// handleLogin processes login requests.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")
	
	if username != h.config.Username || !crypto.VerifyPassword(password, h.config.Password) {
		// For simplicity, do direct comparison if password isn't hashed
		if password != h.config.Password {
			http.Redirect(w, r, h.config.Path+"/login?error=1", http.StatusFound)
			return
		}
	}
	
	// Create session
	token, _ := crypto.GenerateSecureToken(32)
	expiry := time.Now().Add(h.config.SessionTTL)
	
	h.sessionsMu.Lock()
	h.sessions[token] = expiry
	h.sessionsMu.Unlock()
	
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     h.config.Path,
		Expires:  expiry,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	
	http.Redirect(w, r, h.config.Path, http.StatusFound)
}

// handleLogout processes logout requests.
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("admin_session")
	if err == nil {
		h.sessionsMu.Lock()
		delete(h.sessions, cookie.Value)
		h.sessionsMu.Unlock()
	}
	
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     h.config.Path,
		MaxAge:   -1,
		HttpOnly: true,
	})
	
	http.Redirect(w, r, h.config.Path+"/login", http.StatusFound)
}

// handleStatus returns system status HTML.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	
	ollamaHealthy := h.ollamaClient.Health(ctx) == nil
	modelCount := h.modelCache.Count()
	
	statusClass := "status-healthy"
	statusText := "Healthy"
	if !ollamaHealthy {
		statusClass = "status-unhealthy"
		statusText = "Ollama Unavailable"
	}
	
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<div style="display: flex; align-items: center; margin-bottom: 20px;">
			<span class="status-indicator %s"></span>
			<span style="font-size: 1.25rem;">%s</span>
		</div>
		<div class="stat-grid">
			<div class="stat-item">
				<div class="value">%d</div>
				<div class="label">Models</div>
			</div>
			<div class="stat-item">
				<div class="value">%s</div>
				<div class="label">Ollama</div>
			</div>
		</div>
	`, statusClass, statusText, modelCount, map[bool]string{true: "Connected", false: "Disconnected"}[ollamaHealthy])
}

// handleStats returns request statistics HTML.
func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	h.statsMu.RLock()
	totalReqs := h.totalRequests
	totalToks := h.totalTokens
	totalLat := h.totalLatencyMs
	h.statsMu.RUnlock()
	
	avgLatency := int64(0)
	if totalReqs > 0 {
		avgLatency = totalLat / totalReqs
	}
	
	// Simple rate calculation (would need proper time windowing in production)
	reqsPerMin := totalReqs // Placeholder
	toksPerMin := totalToks // Placeholder
	
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
		<div class="stat-item">
			<div class="value">%d</div>
			<div class="label">Total Requests</div>
		</div>
		<div class="stat-item">
			<div class="value">%d</div>
			<div class="label">Requests/min</div>
		</div>
		<div class="stat-item">
			<div class="value">%dms</div>
			<div class="label">Avg Latency</div>
		</div>
		<div class="stat-item">
			<div class="value">%d</div>
			<div class="label">Tokens/min</div>
		</div>
	`, totalReqs, reqsPerMin, avgLatency, toksPerMin)
}

// handleModels returns models table HTML.
func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	models := h.modelCache.GetModels()
	
	w.Header().Set("Content-Type", "text/html")
	
	if len(models) == 0 {
		w.Write([]byte(`<thead><tr><th>Name</th><th>Size</th><th>Family</th><th>Quantization</th><th>Last Modified</th></tr></thead><tbody><tr><td colspan="5">No models available</td></tr></tbody>`))
		return
	}
	
	fmt.Fprint(w, `<thead><tr><th>Name</th><th>Size</th><th>Family</th><th>Quantization</th><th>Last Modified</th></tr></thead><tbody>`)
	for _, m := range models {
		sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
		fmt.Fprintf(w, `<tr><td>%s</td><td>%.1f GB</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			m.Name, sizeGB, m.Details.Family, m.Details.QuantizationLevel, m.ModifiedAt.Format("2006-01-02 15:04"))
	}
	fmt.Fprint(w, `</tbody>`)
}

// handleModelUsage returns model usage stats HTML.
func (h *Handler) handleModelUsage(w http.ResponseWriter, r *http.Request) {
	h.requestsMu.RLock()
	defer h.requestsMu.RUnlock()
	
	// Count by model
	counts := make(map[string]int)
	for _, req := range h.recentRequests {
		counts[req.Model]++
	}
	
	// Sort by count
	type modelCount struct {
		Model string
		Count int
	}
	var sorted []modelCount
	for m, c := range counts {
		sorted = append(sorted, modelCount{m, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})
	
	w.Header().Set("Content-Type", "text/html")
	
	if len(sorted) == 0 {
		w.Write([]byte(`<div style="color: var(--text-secondary);">No data yet</div>`))
		return
	}
	
	for _, mc := range sorted[:min(5, len(sorted))] {
		pct := float64(mc.Count) / float64(len(h.recentRequests)) * 100
		fmt.Fprintf(w, `
			<div style="margin-bottom: 10px;">
				<div style="display: flex; justify-content: space-between; margin-bottom: 5px;">
					<span>%s</span>
					<span>%d (%.0f%%)</span>
				</div>
				<div style="background: var(--bg-card); border-radius: 4px; overflow: hidden;">
					<div style="width: %.0f%%; background: var(--accent); height: 8px;"></div>
				</div>
			</div>
		`, mc.Model, mc.Count, pct, pct)
	}
}

// handleClassificationStats returns classification distribution HTML.
func (h *Handler) handleClassificationStats(w http.ResponseWriter, r *http.Request) {
	h.requestsMu.RLock()
	defer h.requestsMu.RUnlock()
	
	counts := make(map[string]int)
	for _, req := range h.recentRequests {
		counts[req.Category]++
	}
	
	categories := []string{"code", "reasoning", "simple", "creative", "math", "general"}
	
	w.Header().Set("Content-Type", "text/html")
	
	total := len(h.recentRequests)
	if total == 0 {
		w.Write([]byte(`<div style="color: var(--text-secondary);">No data yet</div>`))
		return
	}
	
	fmt.Fprint(w, `<div style="display: flex; flex-wrap: wrap; gap: 10px;">`)
	for _, cat := range categories {
		count := counts[cat]
		pct := float64(count) / float64(total) * 100
		fmt.Fprintf(w, `<span class="badge badge-%s">%s: %d (%.0f%%)</span>`, cat, cat, count, pct)
	}
	fmt.Fprint(w, `</div>`)
}

// handleListKeys returns API keys table HTML.
func (h *Handler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.keyStore.List(r.Context())
	if err != nil {
		http.Error(w, "Failed to list keys", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	
	fmt.Fprint(w, `<thead><tr><th>ID</th><th>Name</th><th>Scopes</th><th>Rate Limit</th><th>Usage</th><th>Status</th><th>Actions</th></tr></thead><tbody>`)
	
	if len(keys) == 0 {
		fmt.Fprint(w, `<tr><td colspan="7">No API keys yet</td></tr>`)
	}
	
	for _, key := range keys {
		status := "Active"
		statusStyle := "color: var(--success);"
		if !key.Enabled {
			status = "Disabled"
			statusStyle = "color: var(--error);"
		} else if key.IsExpired() {
			status = "Expired"
			statusStyle = "color: var(--warning);"
		}
		
		rateLimit := "Unlimited"
		if key.RateLimit > 0 {
			rateLimit = fmt.Sprintf("%d/min", key.RateLimit)
		}
		
		fmt.Fprintf(w, `<tr>
			<td><code>%s</code></td>
			<td>%s</td>
			<td>%v</td>
			<td>%s</td>
			<td>%d</td>
			<td style="%s">%s</td>
			<td><button class="btn btn-danger" hx-delete="/admin/api/keys/%s" hx-confirm="Delete this key?" hx-swap="none" hx-on::after-request="document.body.dispatchEvent(new CustomEvent('keyDeleted'))">Delete</button></td>
		</tr>`, key.ID, key.Name, key.Scopes, rateLimit, key.UsageCount, statusStyle, status, key.ID)
	}
	
	fmt.Fprint(w, `</tbody>`)
}

// handleCreateKey creates a new API key.
func (h *Handler) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	
	rateLimit := 0
	fmt.Sscanf(r.FormValue("rate_limit"), "%d", &rateLimit)
	
	scopes := r.Form["scopes"]
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}
	
	fullKey, _, err := h.keyStore.Create(r.Context(), keystore.CreateKeyRequest{
		Name:      name,
		Scopes:    scopes,
		RateLimit: rateLimit,
	})
	if err != nil {
		http.Error(w, "Failed to create key", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"key": fullKey})
}

// handleDeleteKey deletes an API key.
func (h *Handler) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	
	if err := h.keyStore.Delete(r.Context(), id); err != nil {
		http.Error(w, "Failed to delete key", http.StatusInternalServerError)
		return
	}
	
	w.WriteHeader(http.StatusOK)
}

// handleRequests returns recent requests table HTML.
func (h *Handler) handleRequests(w http.ResponseWriter, r *http.Request) {
	h.requestsMu.RLock()
	requests := make([]RecentRequest, len(h.recentRequests))
	copy(requests, h.recentRequests)
	h.requestsMu.RUnlock()
	
	w.Header().Set("Content-Type", "text/html")
	
	fmt.Fprint(w, `<thead><tr><th>Time</th><th>Endpoint</th><th>Model</th><th>Category</th><th>Tokens</th><th>Latency</th><th>Status</th></tr></thead><tbody>`)
	
	if len(requests) == 0 {
		fmt.Fprint(w, `<tr><td colspan="7">No requests yet</td></tr>`)
	}
	
	for _, req := range requests[:min(100, len(requests))] {
		statusStyle := ""
		if req.StatusCode >= 400 {
			statusStyle = "color: var(--error);"
		}
		
		fmt.Fprintf(w, `<tr>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td><span class="badge badge-%s">%s</span></td>
			<td>%d</td>
			<td>%dms</td>
			<td style="%s">%d</td>
		</tr>`, req.Time.Format("15:04:05"), req.Endpoint, req.Model, req.Category, req.Category, req.Tokens, req.LatencyMs, statusStyle, req.StatusCode)
	}
	
	fmt.Fprint(w, `</tbody>`)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
