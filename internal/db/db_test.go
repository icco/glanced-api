package db_test

import (
	"encoding/json"
	"slices"
	"testing"

	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
)

func TestOpenMigrates(t *testing.T) {
	g := dbtest.New(t)
	for _, m := range []any{&db.User{}, &db.Session{}, &db.Card{}, &db.Line{}} {
		if !g.Migrator().HasTable(m) {
			t.Errorf("missing table for %T", m)
		}
	}
	var seq int64
	if err := g.Raw("SELECT nextval('glanced_seq')").Scan(&seq).Error; err != nil || seq <= 0 {
		t.Fatalf("nextval = %d, %v", seq, err)
	}
}

func keys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestWireKeys(t *testing.T) {
	cardKeys := []string{"card_signal", "date", "deleted", "device_id", "id", "kind", "updated_at"}
	if got := keys(t, db.Card{}); !slices.Equal(got, cardKeys) {
		t.Errorf("card keys = %v, want %v", got, cardKeys)
	}
	lineKeys := []string{"card_id", "deleted", "device_id", "id", "migrated_to", "position", "signal", "text", "updated_at"}
	if got := keys(t, db.Line{}); !slices.Equal(got, lineKeys) {
		t.Errorf("line keys = %v, want %v", got, lineKeys)
	}
}
