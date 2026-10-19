package admin

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/ollama"
)

const (
	htmxTag   = `<script src="https://unpkg.com/htmx.org@1.9.10/dist/htmx.min.js" integrity="sha384-D1Kt99CQMDuVetoL1lrYwg5t+9QdHe7NLX/SoJYkXDFfX37iInKRy5xLSi8nO7UC" crossorigin="anonymous"></script>`
	alpineTag = `<script src="https://unpkg.com/alpinejs@3.13.5/dist/cdn.min.js" integrity="sha384-BxpSbjbDhVKwnC1UfcjsNEuMuxg4af5IXOaSi1Iq5rASQ/9a7uslhEXbP9UI/fXo" crossorigin="anonymous" defer></script>`
)

var scriptTagRE = regexp.MustCompile(`<script\b[^>]*>`)

func TestScriptsHaveSRI(t *testing.T) {
	h := NewHandler(&config.AdminConfig{Path: "/admin"}, nil, ollama.NewModelCache(nil, 0), nil, nil)

	tests := []struct {
		name string
		fn   http.HandlerFunc
		want []string
	}{
		{name: "index", fn: h.handleIndex, want: []string{htmxTag, alpineTag}},
		{name: "chat", fn: h.handleChatPage, want: []string{alpineTag}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := serve(t, tt.fn)
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body missing %s", w)
				}
			}
			external := 0
			for _, tag := range scriptTagRE.FindAllString(body, -1) {
				if !strings.Contains(tag, `src="http`) {
					continue
				}
				external++
				if !strings.Contains(tag, `integrity="sha384-`) || !strings.Contains(tag, `crossorigin="anonymous"`) {
					t.Errorf("external script without SRI: %s", tag)
				}
			}
			if external != len(tt.want) {
				t.Errorf("external scripts = %d, want %d", external, len(tt.want))
			}
		})
	}
}
