package admin

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/keystore"
	"github.com/kalle/local-model-router/internal/ollama"
)

// The test prefix must not contain "admin", so a leftover "/admin" literal can't hide in it.
func TestPagesUseConfiguredPath(t *testing.T) {
	store, err := keystore.NewSQLiteStore(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	_, key, err := store.Create(context.Background(), keystore.CreateKeyRequest{Name: "k"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	custom := NewHandler(&config.AdminConfig{Path: "/ops/console"}, store, ollama.NewModelCache(nil, 0), nil, nil)
	def := NewHandler(&config.AdminConfig{Path: "/admin"}, store, ollama.NewModelCache(nil, 0), nil, nil)

	tests := []struct {
		name      string
		fn        http.HandlerFunc
		want      []string
		anyOf     []string // at least one must appear
		noDefault bool     // body must not contain "/admin"
	}{
		{
			name: "index custom",
			fn:   custom.handleIndex,
			want: []string{
				`hx-get="/ops/console/api/status"`,
				`hx-get="/ops/console/api/keys"`,
				`hx-post="/ops/console/api/keys"`,
				`href="/ops/console/chat"`,
			},
			noDefault: true,
		},
		{
			name:      "chat custom",
			fn:        custom.handleChatPage,
			anyOf:     []string{`adminPath: "/ops/console"`, `adminPath: "\/ops\/console"`},
			noDefault: true,
		},
		{
			name:      "keys custom",
			fn:        custom.handleListKeys,
			want:      []string{`hx-delete="/ops/console/api/keys/` + key.ID + `"`},
			noDefault: true,
		},
		{
			name: "index default",
			fn:   def.handleIndex,
			want: []string{`hx-get="/admin/api/status"`, `hx-post="/admin/api/keys"`, `href="/admin/chat"`},
		},
		{
			name: "keys default",
			fn:   def.handleListKeys,
			want: []string{`hx-delete="/admin/api/keys/` + key.ID + `"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := serve(t, tt.fn)
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("body missing %s", w)
				}
			}
			if len(tt.anyOf) > 0 {
				found := false
				for _, w := range tt.anyOf {
					found = found || strings.Contains(body, w)
				}
				if !found {
					t.Errorf("body missing any of %v", tt.anyOf)
				}
			}
			if tt.noDefault && strings.Contains(body, "/admin") {
				t.Errorf("body contains /admin:\n%s", body)
			}
		})
	}
}
