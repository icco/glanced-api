package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
	"go.icco.me/glanced-api/internal/syncer"
)

const (
	alice = "0199a000-0000-7000-8000-00000000000a"
	bob   = "0199a000-0000-7000-8000-00000000000b"
)

func ptr(s string) *string { return &s }

func line(id, text string, at int64, dev string) db.Line {
	return db.Line{ID: id, CardID: "next", Position: 0, Text: text, Signal: "empty", UpdatedAtMs: at, DeviceID: dev}
}

func card(id string, at int64) db.Card {
	return db.Card{ID: id, Kind: "next", UpdatedAtMs: at, DeviceID: "a"}
}

func push(t *testing.T, g *gorm.DB, user string, cards []db.Card, lines []db.Line) int {
	t.Helper()
	n, err := syncer.Push(context.Background(), g, user, cards, lines)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	return n
}

func storedL1(t *testing.T, g *gorm.DB) db.Line {
	t.Helper()
	var l db.Line
	if err := g.Where("user_id = ? AND id = ?", alice, "l1").First(&l).Error; err != nil {
		t.Fatal(err)
	}
	return l
}

func TestPushNewerWins(t *testing.T) {
	g := dbtest.New(t)
	push(t, g, alice, nil, []db.Line{line("l1", "old", 100, "a")})
	if n := push(t, g, alice, nil, []db.Line{line("l1", "new", 200, "a")}); n != 1 {
		t.Errorf("accepted = %d", n)
	}
	if got := storedL1(t, g).Text; got != "new" {
		t.Errorf("text = %q", got)
	}
}

func TestPushOlderLoses(t *testing.T) {
	g := dbtest.New(t)
	push(t, g, alice, nil, []db.Line{line("l1", "keep", 200, "a")})
	if n := push(t, g, alice, nil, []db.Line{line("l1", "stale", 100, "a")}); n != 0 {
		t.Errorf("accepted = %d", n)
	}
	if got := storedL1(t, g).Text; got != "keep" {
		t.Errorf("text = %q", got)
	}
}

func TestPushTieBreaksOnDeviceID(t *testing.T) {
	g := dbtest.New(t)
	push(t, g, alice, nil, []db.Line{line("l1", "from b", 100, "b")})
	push(t, g, alice, nil, []db.Line{line("l1", "from a", 100, "a")})
	if got := storedL1(t, g).Text; got != "from b" {
		t.Errorf("text = %q", got)
	}
	push(t, g, alice, nil, []db.Line{line("l1", "from c", 100, "c")})
	if got := storedL1(t, g).Text; got != "from c" {
		t.Errorf("text = %q", got)
	}
}

func TestPushIdempotent(t *testing.T) {
	g := dbtest.New(t)
	batch := []db.Line{line("l1", "x", 100, "a"), line("l2", "y", 100, "a")}
	push(t, g, alice, []db.Card{card("next", 100)}, batch)
	s1 := storedL1(t, g).ServerSeq
	if n := push(t, g, alice, []db.Card{card("next", 100)}, batch); n != 0 {
		t.Errorf("second push accepted = %d", n)
	}
	if s2 := storedL1(t, g).ServerSeq; s2 != s1 {
		t.Errorf("server_seq changed %d -> %d", s1, s2)
	}
}

func TestPushIsolatedByUser(t *testing.T) {
	g := dbtest.New(t)
	push(t, g, alice, []db.Card{{ID: "next", Kind: "next", CardSignal: 1, UpdatedAtMs: 100, DeviceID: "a"}}, nil)
	push(t, g, bob, []db.Card{{ID: "next", Kind: "next", CardSignal: 2, UpdatedAtMs: 50, DeviceID: "a"}}, nil)
	for user, want := range map[string]int{alice: 1, bob: 2} {
		p, err := syncer.Pull(context.Background(), g, user, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Cards) != 1 || p.Cards[0].CardSignal != want {
			t.Errorf("%s cards = %+v", user, p.Cards)
		}
	}
}

func TestTombstoneSyncs(t *testing.T) {
	g := dbtest.New(t)
	push(t, g, alice, nil, []db.Line{line("l1", "x", 100, "a")})
	del := line("l1", "x", 200, "a")
	del.Deleted = true
	push(t, g, alice, nil, []db.Line{del})
	p, err := syncer.Pull(context.Background(), g, alice, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 1 || !p.Lines[0].Deleted {
		t.Errorf("lines = %+v", p.Lines)
	}
}

func TestPullPagesAcrossTables(t *testing.T) {
	g := dbtest.New(t)
	want := map[string]bool{}
	for i := range 8 {
		id := fmt.Sprintf("l%d", i)
		push(t, g, alice, nil, []db.Line{line(id, "x", 100, "a")})
		want[id] = true
		if i < 7 {
			cid := fmt.Sprintf("today:2026-11-%02d", i+1)
			push(t, g, alice, []db.Card{{ID: cid, Kind: "today", Date: ptr(fmt.Sprintf("2026-11-%02d", i+1)), UpdatedAtMs: 100, DeviceID: "a"}}, nil)
			want[cid] = true
		}
	}
	seen := map[string]int{}
	var cursor int64
	for range 20 {
		p, err := syncer.Pull(context.Background(), g, alice, cursor, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Cards)+len(p.Lines) > 4 {
			t.Fatalf("page too big: %d", len(p.Cards)+len(p.Lines))
		}
		for _, c := range p.Cards {
			seen[c.ID]++
		}
		for _, l := range p.Lines {
			seen[l.ID]++
		}
		cursor = p.Cursor
		if !p.More {
			break
		}
	}
	if len(seen) != len(want) {
		t.Errorf("saw %d ids, want %d", len(seen), len(want))
	}
	for id, n := range seen {
		if n != 1 || !want[id] {
			t.Errorf("id %s seen %d times", id, n)
		}
	}
}

func TestValidate(t *testing.T) {
	good := db.Line{ID: "l", CardID: "next", Signal: "empty", DeviceID: "a"}
	tests := map[string]struct {
		cards []db.Card
		lines []db.Line
	}{
		"bad kind":        {cards: []db.Card{{ID: "c", Kind: "later", DeviceID: "a"}}},
		"bad signal":      {lines: []db.Line{{ID: "l", CardID: "next", Signal: "maybe", DeviceID: "a"}}},
		"bad date":        {cards: []db.Card{{ID: "c", Kind: "today", Date: ptr("11/12/2026"), DeviceID: "a"}}},
		"today nil date":  {cards: []db.Card{{ID: "c", Kind: "today", DeviceID: "a"}}},
		"next with date":  {cards: []db.Card{{ID: "c", Kind: "next", Date: ptr("2026-11-12"), DeviceID: "a"}}},
		"negative pos":    {lines: []db.Line{{ID: "l", CardID: "next", Signal: "empty", Position: -1, DeviceID: "a"}}},
		"card signal 4":   {cards: []db.Card{{ID: "c", Kind: "next", CardSignal: 4, DeviceID: "a"}}},
		"empty device id": {lines: []db.Line{{ID: "l", CardID: "next", Signal: "empty"}}},
		"empty line id":   {lines: []db.Line{{CardID: "next", Signal: "empty", DeviceID: "a"}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var inv *syncer.InvalidRowError
			if err := syncer.Validate(tc.cards, tc.lines); !errors.As(err, &inv) {
				t.Errorf("err = %v", err)
			}
		})
	}
	if err := syncer.Validate([]db.Card{{ID: "today:2026-11-12", Kind: "today", Date: ptr("2026-11-12"), CardSignal: 3, DeviceID: "a"}}, []db.Line{good}); err != nil {
		t.Errorf("valid rows: %v", err)
	}
}

func TestPushInvalidStoresNothing(t *testing.T) {
	g := dbtest.New(t)
	_, err := syncer.Push(context.Background(), g, alice, nil, []db.Line{line("l1", "x", 1, "a"), {ID: "bad", Signal: "nope", DeviceID: "a"}})
	var inv *syncer.InvalidRowError
	if !errors.As(err, &inv) || inv.ID != "bad" {
		t.Fatalf("err = %v", err)
	}
	var n int64
	g.Model(&db.Line{}).Count(&n)
	if n != 0 {
		t.Errorf("stored %d lines", n)
	}
}

func TestInvalidRowErrorMessage(t *testing.T) {
	err := &syncer.InvalidRowError{ID: "l9", Reason: "position must be >= 0"}
	if got := err.Error(); got != `invalid row "l9": position must be >= 0` {
		t.Errorf("Error() = %q", got)
	}
}
