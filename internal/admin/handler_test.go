package admin

import (
	"testing"

	"github.com/kalle/local-model-router/internal/config"
	"github.com/kalle/local-model-router/internal/crypto"
)

func TestCheckCredentials(t *testing.T) {
	hash, err := crypto.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	for name, stored := range map[string]string{"plain": "secret", "bcrypt": hash} {
		h := &Handler{config: &config.AdminConfig{Username: "admin", Password: stored}}
		cases := []struct {
			user, pass string
			want       bool
		}{
			{"admin", "secret", true},
			{"other", "secret", false}, // wrong username, right password
			{"admin", "wrong", false},
			{"", "", false},
		}
		for _, c := range cases {
			if got := h.checkCredentials(c.user, c.pass); got != c.want {
				t.Errorf("%s: checkCredentials(%q, %q) = %v, want %v", name, c.user, c.pass, got, c.want)
			}
		}
	}
}
