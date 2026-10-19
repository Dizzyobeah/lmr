package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNumCtx(t *testing.T) {
	if got := DefaultConfig().Ollama.NumCtx; got != 32768 {
		t.Fatalf("DefaultConfig NumCtx = %d, want 32768", got)
	}

	base := "auth:\n  enabled: false\nadmin:\n  enabled: false\n"
	tests := []struct {
		name    string
		ollama  string
		want    int
		wantErr bool
	}{
		{"default", "", 32768, false},
		{"custom", "ollama:\n  num_ctx: 8192\n", 8192, false},
		{"zero", "ollama:\n  num_ctx: 0\n", 0, true},
		{"negative", "ollama:\n  num_ctx: -1\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(base+tt.ollama), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "ollama.num_ctx") {
					t.Fatalf("Load err = %v, want error containing ollama.num_ctx", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Ollama.NumCtx != tt.want {
				t.Fatalf("NumCtx = %d, want %d", cfg.Ollama.NumCtx, tt.want)
			}
		})
	}
}

func TestValidateNumCtx(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.Admin.Enabled = false
	cfg.Ollama.NumCtx = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ollama.num_ctx") {
		t.Fatalf("Validate err = %v, want error containing ollama.num_ctx", err)
	}
}

func TestMaxRequestBodyBytes(t *testing.T) {
	if got := DefaultConfig().Server.MaxRequestBodyBytes; got != 32<<20 {
		t.Fatalf("DefaultConfig MaxRequestBodyBytes = %d, want %d", got, 32<<20)
	}

	base := "auth:\n  enabled: false\nadmin:\n  enabled: false\n"
	tests := []struct {
		name    string
		server  string
		want    int64
		wantErr bool
	}{
		{"default", "", 33554432, false},
		{"custom", "server:\n  max_request_body_bytes: 1048576\n", 1048576, false},
		{"zero", "server:\n  max_request_body_bytes: 0\n", 0, true},
		{"negative", "server:\n  max_request_body_bytes: -1\n", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(base+tt.server), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "server.max_request_body_bytes") {
					t.Fatalf("Load err = %v, want error containing server.max_request_body_bytes", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Server.MaxRequestBodyBytes != tt.want {
				t.Fatalf("MaxRequestBodyBytes = %d, want %d", cfg.Server.MaxRequestBodyBytes, tt.want)
			}
		})
	}
}
