package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientEmbedding(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    string
		wantVecs   [][]float64
		wantTokens int
	}{
		{
			name:       "ok",
			status:     http.StatusOK,
			body:       `{"model":"m","embeddings":[[0.1,0.2],[0.3,0.4]],"prompt_eval_count":9}`,
			wantVecs:   [][]float64{{0.1, 0.2}, {0.3, 0.4}},
			wantTokens: 9,
		},
		{name: "non-200", status: http.StatusInternalServerError, body: `boom`, wantErr: "500"},
		{name: "bad json", status: http.StatusOK, body: `{not json`, wantErr: "decode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotPath string
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, 5*time.Second, 1)
			resp, err := c.Embedding(context.Background(), &EmbeddingRequest{Model: "m", Input: []string{"a", "b"}})

			if gotMethod != http.MethodPost || gotPath != "/api/embed" {
				t.Fatalf("request = %s %s, want POST /api/embed", gotMethod, gotPath)
			}
			wantBody := map[string]any{"model": "m", "input": []any{"a", "b"}}
			if !reflect.DeepEqual(gotBody, wantBody) {
				t.Fatalf("body = %v, want %v", gotBody, wantBody)
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !reflect.DeepEqual(resp.Embeddings, tt.wantVecs) {
				t.Fatalf("embeddings = %v, want %v", resp.Embeddings, tt.wantVecs)
			}
			if resp.PromptEvalCount != tt.wantTokens {
				t.Fatalf("prompt_eval_count = %d, want %d", resp.PromptEvalCount, tt.wantTokens)
			}
		})
	}
}
