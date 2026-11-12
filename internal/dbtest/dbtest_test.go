package dbtest_test

import (
	"strings"
	"testing"

	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
)

func TestNewStartsEmpty(t *testing.T) {
	g := dbtest.New(t)
	if err := g.Create(&db.User{ID: "0199a000-0000-7000-8000-00000000000a", Email: "ada@example.com", PasswordHash: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	g = dbtest.New(t)
	var n int64
	if err := g.Model(&db.User{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}

func TestURL(t *testing.T) {
	if u := dbtest.URL(t); !strings.HasPrefix(u, "postgres://") {
		t.Errorf("URL = %q", u)
	}
}
