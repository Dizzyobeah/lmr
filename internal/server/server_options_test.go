package server

import (
	"testing"

	"github.com/kalle/local-model-router/pkg/openai"
)

func TestBuildOllamaOptionsNumCtx(t *testing.T) {
	temp := 0.2
	req := openai.ChatCompletionRequest{Temperature: &temp}

	opts := buildOllamaOptions(&req, 8192)

	if opts["num_ctx"] != 8192 {
		t.Errorf("num_ctx = %v, want 8192", opts["num_ctx"])
	}
	if opts["temperature"] != 0.2 {
		t.Errorf("temperature = %v, want 0.2", opts["temperature"])
	}
}
