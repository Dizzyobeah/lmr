package keystore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordUsageSetsLastUsedAt(t *testing.T) {
	ctx := context.Background()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	_, key, err := s.Create(ctx, CreateKeyRequest{Name: "k"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	lastUsed := func() *time.Time {
		keys, err := s.List(ctx)
		if err != nil || len(keys) != 1 {
			t.Fatalf("List: keys=%v err=%v", keys, err)
		}
		return keys[0].LastUsedAt
	}

	if got := lastUsed(); got != nil {
		t.Fatalf("LastUsedAt before use = %v, want nil", got)
	}
	if err := s.RecordUsage(ctx, key.ID); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	got := lastUsed()
	if got == nil {
		t.Fatal("LastUsedAt after RecordUsage = nil")
	}
	if d := time.Since(*got); d < -5*time.Second || d > 5*time.Second {
		t.Errorf("LastUsedAt = %v, not within 5s of now (diff %v)", got, d)
	}
}
