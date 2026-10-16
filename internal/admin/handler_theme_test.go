package admin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
)

func TestThemeCSSServedWithoutAuth(t *testing.T) {
	h := NewHandler(&config.AdminConfig{Path: "/admin"}, nil, nil, nil, nil)
	router := chi.NewRouter()
	router.Mount("/admin", h.Router()) // as internal/server does

	tests := []struct {
		path     string
		wantCode int
	}{
		{"/admin/theme.css", http.StatusOK},
		{"/admin/", http.StatusFound},
		{"/admin/chat", http.StatusFound},
		{"/admin/theme.css/x", http.StatusFound},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", tt.path, nil))
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusFound {
				if loc := rec.Header().Get("Location"); loc != "/admin/login" {
					t.Errorf("Location = %q, want /admin/login", loc)
				}
				return
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
				t.Errorf("Content-Type = %q, want text/css", ct)
			}
			body := rec.Body.String()
			for _, want := range []string{
				"--color-canvas-dark: #010120",
				"--color-hairline: #ebebeb",
				"--color-accent-orange: #fc4c02",
				"--color-accent-magenta: #ef2cc1",
				"--color-accent-periwinkle: #bdbbff",
				"--color-accent-mint: #c8f6f9",
				"--color-body: #999999",
				"--radius-sm: 4px",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("theme.css missing %q", want)
				}
			}
		})
	}
}

var (
	styleBlockRe = regexp.MustCompile(`(?s)<style>.*?</style>`)
	tokenDefRe   = regexp.MustCompile(`--color-[a-z-]+\s*:`)
)

func TestPagesUseTheme(t *testing.T) {
	// A non-default admin.path: the stylesheet must still resolve to {Path}/theme.css.
	// The login page uses a relative href, which resolves from {Path}/login.
	h := NewHandler(&config.AdminConfig{Path: "/ops/admin"}, nil, ollama.NewModelCache(nil, 0), nil, nil)
	tests := []struct {
		name     string
		fn       http.HandlerFunc
		wantHref string
	}{
		{"index", h.handleIndex, "/ops/admin/theme.css"},
		{"chat", h.handleChatPage, "/ops/admin/theme.css"},
		{"login", h.handleLoginPage, "theme.css"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := serve(t, tt.fn)
			if want := `<link rel="stylesheet" href="` + tt.wantHref + `"`; !strings.Contains(body, want) {
				t.Errorf("body missing %s", want)
			}
			if n := strings.Count(body, `class="brand-ribbon"`); n != 1 {
				t.Errorf("brand-ribbon count = %d, want 1", n)
			}
			for _, s := range styleBlockRe.FindAllString(body, -1) {
				if tokenDefRe.MatchString(s) {
					t.Errorf("inline <style> defines tokens: %s", s)
				}
			}
			if strings.Contains(strings.ToLower(body), "together") {
				t.Error("page mentions together")
			}
		})
	}

	css, err := templatesFS.ReadFile("templates/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(css), "var(--gradient-brand)"); n != 1 {
		t.Errorf("var(--gradient-brand) used %d times, want 1", n)
	}
}
