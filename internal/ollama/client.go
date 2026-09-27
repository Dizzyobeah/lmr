// Package ollama provides a client for interacting with the Ollama API.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client is an Ollama API client.
type Client struct {
	endpoint   string
	httpClient *http.Client
	
	// Cached model list
	models      []Model
	modelsMu    sync.RWMutex
	modelsError error
}

// Model represents an Ollama model.
type Model struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	Details    ModelDetails `json:"details,omitempty"`
}

// ModelDetails contains detailed model information.
type ModelDetails struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// GenerateRequest is a request to the Ollama generate endpoint.
type GenerateRequest struct {
	Model    string                 `json:"model"`
	Prompt   string                 `json:"prompt"`
	System   string                 `json:"system,omitempty"`
	Template string                 `json:"template,omitempty"`
	Context  []int                  `json:"context,omitempty"`
	Stream   bool                   `json:"stream"`
	Raw      bool                   `json:"raw,omitempty"`
	Format   string                 `json:"format,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// GenerateResponse is a response from the Ollama generate endpoint.
type GenerateResponse struct {
	Model              string `json:"model"`
	CreatedAt          string `json:"created_at"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	Context            []int  `json:"context,omitempty"`
	TotalDuration      int64  `json:"total_duration,omitempty"`
	LoadDuration       int64  `json:"load_duration,omitempty"`
	PromptEvalCount    int    `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64  `json:"prompt_eval_duration,omitempty"`
	EvalCount          int    `json:"eval_count,omitempty"`
	EvalDuration       int64  `json:"eval_duration,omitempty"`
}

// ChatRequest is a request to the Ollama chat endpoint.
type ChatRequest struct {
	Model    string                 `json:"model"`
	Messages []ChatMessage          `json:"messages"`
	Stream   bool                   `json:"stream"`
	Format   string                 `json:"format,omitempty"`
	Tools    []Tool                 `json:"tools,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// Tool describes a function tool that the model may call. It mirrors the
// Ollama /api/chat tool schema, which is compatible with the OpenAI format.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolName identifies the tool a tool-result message responds to
	// (used when Role == "tool").
	ToolName string `json:"tool_name,omitempty"`
}

// ToolCall is a tool call emitted by the model or replayed back to it.
// Ollama represents arguments as a JSON object rather than a string.
type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction holds the name and arguments of a tool call.
type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ChatResponse is a response from the Ollama chat endpoint.
type ChatResponse struct {
	Model              string      `json:"model"`
	CreatedAt          string      `json:"created_at"`
	Message            ChatMessage `json:"message"`
	Done               bool        `json:"done"`
	TotalDuration      int64       `json:"total_duration,omitempty"`
	LoadDuration       int64       `json:"load_duration,omitempty"`
	PromptEvalCount    int         `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64       `json:"prompt_eval_duration,omitempty"`
	EvalCount          int         `json:"eval_count,omitempty"`
	EvalDuration       int64       `json:"eval_duration,omitempty"`
}

// EmbeddingRequest is a request to the Ollama embedding endpoint.
type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingResponse is a response from the Ollama embedding endpoint.
type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// NewClient creates a new Ollama client.
func NewClient(endpoint string, timeout time.Duration) *Client {
	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// ListModels returns the list of available models.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	resp, err := c.doRequest(ctx, "GET", "/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	var result struct {
		Models []Model `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	// Update cache
	c.modelsMu.Lock()
	c.models = result.Models
	c.modelsError = nil
	c.modelsMu.Unlock()
	
	return result.Models, nil
}

// GetCachedModels returns the cached model list.
func (c *Client) GetCachedModels() []Model {
	c.modelsMu.RLock()
	defer c.modelsMu.RUnlock()
	return c.models
}

// HasModel checks if a model is available.
func (c *Client) HasModel(name string) bool {
	c.modelsMu.RLock()
	defer c.modelsMu.RUnlock()
	
	for _, m := range c.models {
		if m.Name == name {
			return true
		}
	}
	return false
}

// Generate sends a generate request to Ollama.
func (c *Client) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	resp, err := c.doRequest(ctx, "POST", "/api/generate", req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("generate failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	var result GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &result, nil
}

// GenerateStream sends a streaming generate request to Ollama.
func (c *Client) GenerateStream(ctx context.Context, req *GenerateRequest) (<-chan GenerateResponse, <-chan error) {
	req.Stream = true
	responseChan := make(chan GenerateResponse)
	errorChan := make(chan error, 1)
	
	go func() {
		defer close(responseChan)
		defer close(errorChan)
		
		resp, err := c.doRequest(ctx, "POST", "/api/generate", req)
		if err != nil {
			errorChan <- fmt.Errorf("failed to generate: %w", err)
			return
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("generate failed with status %d: %s", resp.StatusCode, string(body))
			return
		}
		
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var result GenerateResponse
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				errorChan <- fmt.Errorf("failed to decode response: %w", err)
				return
			}
			
			select {
			case responseChan <- result:
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			}
			
			if result.Done {
				return
			}
		}
		
		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("stream error: %w", err)
		}
	}()
	
	return responseChan, errorChan
}

// Chat sends a chat request to Ollama.
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	resp, err := c.doRequest(ctx, "POST", "/api/chat", req)
	if err != nil {
		return nil, fmt.Errorf("failed to chat: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chat failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &result, nil
}

// ChatStream sends a streaming chat request to Ollama.
func (c *Client) ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatResponse, <-chan error) {
	req.Stream = true
	responseChan := make(chan ChatResponse)
	errorChan := make(chan error, 1)
	
	go func() {
		defer close(responseChan)
		defer close(errorChan)
		
		resp, err := c.doRequest(ctx, "POST", "/api/chat", req)
		if err != nil {
			errorChan <- fmt.Errorf("failed to chat: %w", err)
			return
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("chat failed with status %d: %s", resp.StatusCode, string(body))
			return
		}
		
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var result ChatResponse
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				errorChan <- fmt.Errorf("failed to decode response: %w", err)
				return
			}
			
			select {
			case responseChan <- result:
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			}
			
			if result.Done {
				return
			}
		}
		
		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("stream error: %w", err)
		}
	}()
	
	return responseChan, errorChan
}

// Embedding generates embeddings for the given input.
func (c *Client) Embedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	resp, err := c.doRequest(ctx, "POST", "/api/embeddings", req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	var result EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	return &result, nil
}

// Health checks if Ollama is healthy and reachable.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.doRequest(ctx, "GET", "/", nil)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}
	
	return nil
}

// doRequest performs an HTTP request to Ollama.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	
	return c.httpClient.Do(req)
}
