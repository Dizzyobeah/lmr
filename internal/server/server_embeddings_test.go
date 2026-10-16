package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
	"github.com/kalle/local-model-router/pkg/openai"
)

// fakeOllamaEmbed starts a fake Ollama answering /api/embed with respBody.
// It counts calls and records the last decoded request.
func fakeOllamaEmbed(t *testing.T, respBody string) (*httptest.Server, *int32, *ollama.EmbeddingRequest) {
	t.Helper()
	var calls int32
	var last ollama.EmbeddingRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&last)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &last
}

func newEmbeddingsTestServer(ollamaURL string, models ...string) *Server {
	cache := ollama.NewModelCache(nil, 0)
	var ms []ollama.Model
	for _, m := range models {
		ms = append(ms, ollama.Model{Name: m})
	}
	cache.SetModels(ms)

	return &Server{
		config:       config.DefaultConfig(),
		modelCache:   cache,
		ollamaClient: ollama.NewClient(ollamaURL, 5*time.Second, 1),
	}
}

func postEmbeddings(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleEmbeddings(rec, req)
	return rec
}

func TestHandleEmbeddings(t *testing.T) {
	tests := []struct {
		name       string
		models     []string
		reqBody    string
		ollamaResp string
		wantStatus int
		wantCalls  int32
		wantModel  string // model sent to Ollama
		wantVecs   [][]float64
		wantTokens int
	}{
		{
			name:       "batch uses one call and real token count",
			models:     []string{"nomic-embed-text"},
			reqBody:    `{"model":"nomic-embed-text","input":["a","b","c"]}`,
			ollamaResp: `{"embeddings":[[1,0],[0,1],[0.5,0.5]],"prompt_eval_count":17}`,
			wantStatus: http.StatusOK,
			wantCalls:  1,
			wantModel:  "nomic-embed-text",
			wantVecs:   [][]float64{{1, 0}, {0, 1}, {0.5, 0.5}},
			wantTokens: 17,
		},
		{
			name:       "missing prompt_eval_count falls back to len/4",
			models:     []string{"nomic-embed-text"},
			reqBody:    `{"model":"nomic-embed-text","input":["abcdefgh","abcd"]}`,
			ollamaResp: `{"embeddings":[[1],[2]]}`,
			wantStatus: http.StatusOK,
			wantCalls:  1,
			wantModel:  "nomic-embed-text",
			wantVecs:   [][]float64{{1}, {2}},
			wantTokens: 3, // 8/4 + 4/4
		},
		{
			name:       "embedding count mismatch is 500",
			models:     []string{"nomic-embed-text"},
			reqBody:    `{"model":"nomic-embed-text","input":["a","b","c"]}`,
			ollamaResp: `{"embeddings":[[1],[2]],"prompt_eval_count":3}`,
			wantStatus: http.StatusInternalServerError,
			wantCalls:  1,
			wantModel:  "nomic-embed-text",
		},
		{
			name:       "empty model picks installed embedding model",
			models:     []string{"llama3.2", "nomic-embed-text"},
			reqBody:    `{"model":"","input":"hello"}`,
			ollamaResp: `{"embeddings":[[0.1]],"prompt_eval_count":1}`,
			wantStatus: http.StatusOK,
			wantCalls:  1,
			wantModel:  "nomic-embed-text",
			wantVecs:   [][]float64{{0.1}},
			wantTokens: 1,
		},
		{
			name:       "no embedding model installed is 404 without Ollama call",
			models:     []string{"llama3.2"},
			reqBody:    `{"model":"","input":"hello"}`,
			wantStatus: http.StatusNotFound,
			wantCalls:  0,
		},
		{
			name:       "empty input skips Ollama",
			models:     []string{"nomic-embed-text"},
			reqBody:    `{"model":"nomic-embed-text","input":[]}`,
			wantStatus: http.StatusOK,
			wantCalls:  0,
			wantVecs:   [][]float64{},
			wantTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls, last := fakeOllamaEmbed(t, tt.ollamaResp)
			s := newEmbeddingsTestServer(srv.URL, tt.models...)

			rec := postEmbeddings(s, tt.reqBody)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if got := atomic.LoadInt32(calls); got != tt.wantCalls {
				t.Fatalf("ollama calls = %d, want %d", got, tt.wantCalls)
			}
			if tt.wantCalls > 0 && last.Model != tt.wantModel {
				t.Fatalf("ollama model = %q, want %q", last.Model, tt.wantModel)
			}

			if tt.wantStatus != http.StatusOK {
				var errResp openai.APIError
				if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil || errResp.Detail.Message == "" {
					t.Fatalf("expected OpenAI error shape, got %s (err %v)", rec.Body, err)
				}
				return
			}

			var resp openai.EmbeddingResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Data) != len(tt.wantVecs) {
				t.Fatalf("data len = %d, want %d", len(resp.Data), len(tt.wantVecs))
			}
			for i, d := range resp.Data {
				if d.Index != i || !reflect.DeepEqual(d.Embedding, tt.wantVecs[i]) {
					t.Fatalf("data[%d] = {index %d, %v}, want {index %d, %v}", i, d.Index, d.Embedding, i, tt.wantVecs[i])
				}
			}
			if resp.Usage.PromptTokens != tt.wantTokens || resp.Usage.TotalTokens != tt.wantTokens {
				t.Fatalf("usage = %+v, want %d", resp.Usage, tt.wantTokens)
			}
		})
	}
}
