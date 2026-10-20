package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalle/local-model-router/internal/classifier"
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/gemini"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/pkg/openai"
)

const testGeminiKey = "secret-gemini-key"

func TestGeminiFirst(t *testing.T) {
	auto := &classifier.RouteResult{Model: "llama3.2", Reason: "category_preference:general"}
	requested := &classifier.RouteResult{Model: "llama3.2", Reason: "requested_model"}
	tests := []struct {
		name     string
		enabled  bool
		priority int
		force    bool
		model    string
		route    *classifier.RouteResult
		want     bool
	}{
		{"priority 1 auto-routed", true, 1, true, "auto", auto, true},
		{"priority 0", true, 0, true, "auto", auto, false},
		{"priority 1 honoured requested_model", true, 1, false, "llama3.2", requested, false},
		{"force off and gemini model requested", true, 0, false, "gemini-2.5-flash", auto, true},
		{"force on ignores gemini model name", true, 0, true, "gemini-2.5-flash", auto, false},
		{"not configured priority 1", false, 1, true, "auto", auto, false},
		{"not configured gemini model requested", false, 1, false, "gemini-2.5-flash", auto, false},
		{"not configured requested_model", false, 1, false, "llama3.2", requested, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Gemini.Enabled = tt.enabled
			cfg.Gemini.Priority = tt.priority
			cfg.Routing.ForceRouting = tt.force
			req := &openai.ChatCompletionRequest{Model: tt.model}
			if got := geminiFirst(cfg, req, tt.route); got != tt.want {
				t.Fatalf("geminiFirst = %v, want %v", got, tt.want)
			}
		})
	}
}

type geminiCapture struct {
	calls  int32
	method string
	path   string
	auth   string
	body   []byte
}

// fakeGemini answers every request with status and respBody (SSE when stream).
func fakeGemini(t *testing.T, status int, respBody string, stream bool) (*httptest.Server, *geminiCapture) {
	t.Helper()
	c := &geminiCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&c.calls, 1)
		c.method, c.path, c.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		c.body, _ = io.ReadAll(r.Body)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// fakeOllamaChat answers /api/chat with a fixed non-stream reply.
func fakeOllamaChat(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"llama3.2","message":{"role":"assistant","content":"from ollama"},"done":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newGeminiTestServer(t *testing.T, geminiURL, ollamaURL string, priority int, models ...string) *Server {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Gemini.Enabled = true
	cfg.Gemini.Endpoint = geminiURL
	cfg.Gemini.APIKey = testGeminiKey
	cfg.Gemini.Priority = priority

	cache := ollama.NewModelCache(nil, 0)
	var ms []ollama.Model
	for _, m := range models {
		ms = append(ms, ollama.Model{Name: m})
	}
	cache.SetModels(ms)
	cl, err := classifier.NewClassifier(classifier.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		config:       cfg,
		modelCache:   cache,
		ollamaClient: ollama.NewClient(ollamaURL, 5*time.Second, 1),
		geminiClient: gemini.NewClient(geminiURL, testGeminiKey, 5*time.Second),
		modelRouter:  classifier.NewRouter(cl, cache, cfg.Routing),
	}
}

func postChat(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleChatCompletions(rec, req)
	return rec
}

const imageChat = `{"model":"auto","tool_choice":"auto","messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`

func TestGeminiProxy(t *testing.T) {
	const upstreamJSON = `{"id":"x","object":"chat.completion","model":"gemini-2.5-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

	t.Run("non-stream", func(t *testing.T) {
		g, c := fakeGemini(t, http.StatusOK, upstreamJSON, false)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 1, "llama3.2")
		rec := postChat(s, imageChat)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		if got := rec.Body.String(); got != upstreamJSON {
			t.Fatalf("body = %s, want upstream JSON", got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q", ct)
		}
		if c.method != http.MethodPost || c.path != "/chat/completions" {
			t.Fatalf("upstream got %s %s", c.method, c.path)
		}
		if c.auth != "Bearer "+testGeminiKey {
			t.Fatalf("auth header = %q", c.auth)
		}
		var sent struct {
			Model      string          `json:"model"`
			ToolChoice string          `json:"tool_choice"`
			Messages   json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(c.body, &sent); err != nil {
			t.Fatal(err)
		}
		if sent.Model != "gemini-2.5-flash" {
			t.Fatalf("model = %q", sent.Model)
		}
		if sent.ToolChoice != "auto" {
			t.Fatalf("tool_choice lost: %s", c.body)
		}
		if !strings.Contains(string(sent.Messages), `"type":"image_url"`) || !strings.Contains(string(sent.Messages), "data:image/png;base64,AAAA") {
			t.Fatalf("image part lost: %s", sent.Messages)
		}
	})

	t.Run("stream", func(t *testing.T) {
		sse := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
		g, _ := fakeGemini(t, http.StatusOK, sse, true)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 1, "llama3.2")
		rec := postChat(s, `{"model":"auto","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content-type = %q", ct)
		}
		if got := rec.Body.String(); got != sse {
			t.Fatalf("body = %q, want %q", got, sse)
		}
	})
}

func TestGeminiFallback(t *testing.T) {
	const upstreamErr = `{"error":{"message":"upstream-secret-detail"}}`

	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		t.Run("gemini first fails to ollama "+http.StatusText(status), func(t *testing.T) {
			g, gc := fakeGemini(t, status, upstreamErr, false)
			o, oc := fakeOllamaChat(t)
			s := newGeminiTestServer(t, g.URL, o.URL, 1, "llama3.2")
			rec := postChat(s, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if atomic.LoadInt32(&gc.calls) != 1 || atomic.LoadInt32(oc) != 1 {
				t.Fatalf("gemini calls %d, ollama calls %d", gc.calls, *oc)
			}
			if !strings.Contains(rec.Body.String(), "from ollama") {
				t.Fatalf("body = %s", rec.Body)
			}
		})
	}

	t.Run("gemini 429 and no ollama model", func(t *testing.T) {
		g, _ := fakeGemini(t, http.StatusTooManyRequests, upstreamErr, false)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 1)
		rec := postChat(s, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d", rec.Code)
		}
		var apiErr openai.APIError
		if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
			t.Fatal(err)
		}
		if apiErr.Detail.Type != openai.ErrorTypeRateLimit {
			t.Fatalf("type = %q", apiErr.Detail.Type)
		}
		body := rec.Body.String()
		if strings.Contains(body, testGeminiKey) || strings.Contains(body, "upstream-secret-detail") {
			t.Fatalf("body leaks key or upstream body: %s", body)
		}
	})

	t.Run("gemini 500 and no ollama model", func(t *testing.T) {
		g, _ := fakeGemini(t, http.StatusInternalServerError, upstreamErr, false)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 1)
		rec := postChat(s, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "upstream-secret-detail") {
			t.Fatalf("body leaks upstream body: %s", rec.Body)
		}
	})

	t.Run("null body does not panic", func(t *testing.T) {
		g, gc := fakeGemini(t, http.StatusOK, `{"id":"x","choices":[]}`, false)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 1)
		rec := postChat(s, `null`)
		if rec.Code != http.StatusOK || atomic.LoadInt32(&gc.calls) != 1 {
			t.Fatalf("status %d, gemini calls %d", rec.Code, gc.calls)
		}
		if string(gc.body) != `{"model":"gemini-2.5-flash"}` {
			t.Fatalf("upstream body = %s", gc.body)
		}
	})

	t.Run("priority 0 and empty ollama cache goes to gemini", func(t *testing.T) {
		g, gc := fakeGemini(t, http.StatusOK, `{"id":"x","choices":[]}`, false)
		s := newGeminiTestServer(t, g.URL, "http://127.0.0.1:1", 0)
		rec := postChat(s, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		if atomic.LoadInt32(&gc.calls) != 1 {
			t.Fatalf("gemini calls = %d", gc.calls)
		}
	})

	t.Run("priority 0 with ollama model stays on ollama", func(t *testing.T) {
		g, gc := fakeGemini(t, http.StatusOK, `{}`, false)
		o, oc := fakeOllamaChat(t)
		s := newGeminiTestServer(t, g.URL, o.URL, 0, "llama3.2")
		rec := postChat(s, `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`)
		if rec.Code != http.StatusOK || atomic.LoadInt32(&gc.calls) != 0 || atomic.LoadInt32(oc) != 1 {
			t.Fatalf("status %d, gemini calls %d, ollama calls %d", rec.Code, gc.calls, *oc)
		}
	})
}
