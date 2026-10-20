package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestAdminPathNormalised(t *testing.T) {
	base := "auth:\n  enabled: false\nadmin:\n  enabled: false\n"
	tests := []struct {
		name string
		path string // appended under admin:, empty means unset
		want string
	}{
		{"default", "", "/admin"},
		{"trailing slash", "  path: /ops/admin/\n", "/ops/admin"},
		{"no leading slash", "  path: ops/admin\n", "/ops/admin"},
		{"empty", "  path: \"\"\n", "/admin"},
		{"root", "  path: /\n", "/admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(base+tt.path), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Admin.Path != tt.want {
				t.Fatalf("Admin.Path = %q, want %q", cfg.Admin.Path, tt.want)
			}
		})
	}
}

func TestGeminiConfig(t *testing.T) {
	base := "auth:\n  enabled: false\nadmin:\n  enabled: false\n"
	tests := []struct {
		name     string
		yaml     string
		key      string
		wantErr  string
		endpoint string
	}{
		{"disabled default no key", "", "", "", "https://generativelanguage.googleapis.com/v1beta/openai"},
		{"enabled with env key", "gemini:\n  enabled: true\n", "k", "", "https://generativelanguage.googleapis.com/v1beta/openai"},
		{"env key trimmed", "gemini:\n  enabled: true\n", " k\n", "", "https://generativelanguage.googleapis.com/v1beta/openai"},
		{"enabled no key", "gemini:\n  enabled: true\n", "", "GEMINI_API_KEY", ""},
		{"trailing slash trimmed", "gemini:\n  enabled: true\n  endpoint: https://x/v1/\n", "k", "", "https://x/v1"},
		{"empty model", "gemini:\n  enabled: true\n  model: \"\"\n", "k", "gemini.model", ""},
		{"http endpoint", "gemini:\n  enabled: true\n  endpoint: http://x/v1\n", "k", "gemini.endpoint", ""},
		{"userinfo endpoint", "gemini:\n  enabled: true\n  endpoint: https://u:p@x/v1\n", "k", "gemini.endpoint", ""},
		{"query endpoint", "gemini:\n  enabled: true\n  endpoint: https://x/v1?key=k\n", "k", "gemini.endpoint", ""},
		{"force query endpoint", "gemini:\n  enabled: true\n  endpoint: https://x/v1?\n", "k", "gemini.endpoint", ""},
		{"fragment endpoint", "gemini:\n  enabled: true\n  endpoint: https://x/v1#f\n", "k", "gemini.endpoint", ""},
		{"ftp endpoint", "gemini:\n  enabled: true\n  endpoint: ftp://x/v1\n", "k", "gemini.endpoint", ""},
		{"no host endpoint", "gemini:\n  enabled: true\n  endpoint: https:///v1\n", "k", "gemini.endpoint", ""},
		{"disabled http endpoint not checked", "gemini:\n  endpoint: http://x/v1\n", "", "", "http://x/v1"},
		{"key in file", "gemini:\n  enabled: true\n  api_key: filekey\n", "k", "gemini.api_key must not be set in the config file; use GEMINI_API_KEY", ""},
		{"key in file while disabled", "gemini:\n  api_key: filekey\n", "", "gemini.api_key must not be set in the config file; use GEMINI_API_KEY", ""},
		{"whitespace key enabled", "gemini:\n  enabled: true\n", "  ", "GEMINI_API_KEY", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GEMINI_API_KEY", tt.key)
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(base+tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load err = %v, want error containing %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Gemini.APIKey != strings.TrimSpace(tt.key) {
				t.Fatalf("APIKey = %q, want %q", cfg.Gemini.APIKey, tt.key)
			}
			if cfg.Gemini.Endpoint != tt.endpoint || cfg.Gemini.Model != "gemini-2.5-flash" || cfg.Gemini.Priority != 0 || cfg.Gemini.Timeout != 120*time.Second {
				t.Fatalf("defaults wrong: %+v", cfg.Gemini)
			}
		})
	}
}
