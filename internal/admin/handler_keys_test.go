package admin

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/keystore"
)

func newKeysTestHandler(t *testing.T) (*Handler, *keystore.SQLiteStore) {
	t.Helper()
	store, err := keystore.NewSQLiteStore(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewHandler(&config.AdminConfig{}, store, nil, nil, nil), store
}

func listKeysBody(t *testing.T, h *Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleListKeys(rec, httptest.NewRequest("GET", "/admin/api/keys", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

var thRe = regexp.MustCompile(`<th>([^<]*)</th>`)

func TestHandleListKeys(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *keystore.SQLiteStore) string // returns expected cell text
	}{
		{
			name:  "no keys",
			setup: func(t *testing.T, s *keystore.SQLiteStore) string { return `<td colspan="8">No API keys yet</td>` },
		},
		{
			name: "never used",
			setup: func(t *testing.T, s *keystore.SQLiteStore) string {
				if _, _, err := s.Create(context.Background(), keystore.CreateKeyRequest{Name: "k"}); err != nil {
					t.Fatalf("Create: %v", err)
				}
				return "<td>Never</td>"
			},
		},
		{
			name: "used",
			setup: func(t *testing.T, s *keystore.SQLiteStore) string {
				ctx := context.Background()
				_, key, err := s.Create(ctx, keystore.CreateKeyRequest{Name: "k"})
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				if err := s.RecordUsage(ctx, key.ID); err != nil {
					t.Fatalf("RecordUsage: %v", err)
				}
				keys, err := s.List(ctx)
				if err != nil || len(keys) != 1 || keys[0].LastUsedAt == nil {
					t.Fatalf("List after RecordUsage: keys=%v err=%v", keys, err)
				}
				return "<td>" + keys[0].LastUsedAt.Format("2006-01-02 15:04") + "</td>"
			},
		},
	}
	wantHeaders := []string{"ID", "Name", "Scopes", "Rate Limit", "Usage", "Last Used", "Status", "Actions"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, store := newKeysTestHandler(t)
			want := tt.setup(t, store)
			body := listKeysBody(t, h)

			thead, _, ok := strings.Cut(body, "</thead>")
			if !ok {
				t.Fatalf("no </thead> in body: %s", body)
			}
			var got []string
			for _, m := range thRe.FindAllStringSubmatch(thead, -1) {
				got = append(got, m[1])
			}
			if strings.Join(got, "|") != strings.Join(wantHeaders, "|") {
				t.Errorf("headers = %v, want %v", got, wantHeaders)
			}
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q:\n%s", want, body)
			}
		})
	}
}

// The static placeholder in admin.html must match the server-rendered header,
// or the table jumps when htmx swaps it in.
func TestAdminTemplateKeysTableHeader(t *testing.T) {
	b, err := templatesFS.ReadFile("templates/admin.html")
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(b), `hx-get="{{.AdminPath}}/api/keys"`)
	if !ok {
		t.Fatal("keys table not found in admin.html")
	}
	table, _, _ = strings.Cut(table, "</table>")
	var got []string
	for _, m := range thRe.FindAllStringSubmatch(table, -1) {
		got = append(got, m[1])
	}
	want := "ID|Name|Scopes|Rate Limit|Usage|Last Used|Status|Actions"
	if strings.Join(got, "|") != want {
		t.Errorf("admin.html keys headers = %v, want %s", got, want)
	}
	if !strings.Contains(table, `colspan="8" class="loading"`) {
		t.Errorf("admin.html keys loading row should have colspan=\"8\"")
	}
}
