package syncer

import (
	"context"
	"testing"

	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
)

func TestDelayedNewerUpdateGetsLaterSeq(t *testing.T) {
	g := dbtest.New(t)
	const user = "0199a000-0000-7000-8000-00000000000a"
	if err := g.Create(&db.User{ID: user, Email: "ada@example.com", PasswordHash: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	row := func(at int64) db.Line {
		return db.Line{UserID: user, ID: "l1", CardID: "next", Signal: "empty", Text: "x", UpdatedAtMs: at, DeviceID: "a"}
	}
	if _, err := Push(context.Background(), g, user, nil, []db.Line{row(100)}); err != nil {
		t.Fatal(err)
	}
	newer := row(200)
	if err := reserve(g, &newer.ServerSeq); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(context.Background(), g, user, nil, []db.Line{row(150)}); err != nil {
		t.Fatal(err)
	}
	var older db.Line
	if err := g.Where("id = ?", "l1").First(&older).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store(g, "lines", &newer); err != nil {
		t.Fatal(err)
	}
	var got db.Line
	if err := g.Where("id = ?", "l1").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.UpdatedAtMs != 200 || got.ServerSeq <= older.ServerSeq {
		t.Errorf("stored updated_at=%d server_seq=%d, older push had server_seq=%d", got.UpdatedAtMs, got.ServerSeq, older.ServerSeq)
	}
}
