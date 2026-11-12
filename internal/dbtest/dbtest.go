// Package dbtest provides a migrated Postgres database for tests.
package dbtest

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/db"
)

var (
	once    sync.Once
	baseURL string
	errInit error
)

func start() {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		errInit = err
		return
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		errInit = err
		return
	}
	dir, err := os.MkdirTemp("", "glanced-pg-")
	if err != nil {
		errInit = err
		return
	}
	cfg := embeddedpostgres.DefaultConfig().
		Port(uint32(port)). //nolint:gosec // port comes from the OS and fits in uint32
		RuntimePath(filepath.Join(dir, "runtime")).
		DataPath(filepath.Join(dir, "data")).
		BinariesPath(filepath.Join(dir, "bin")).
		Encoding("UTF8").
		Database("glanced").
		Logger(nil)
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		errInit = err
		return
	}
	baseURL = fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/glanced?sslmode=disable", port)
}

// New returns a migrated database with every table emptied. The embedded
// Postgres starts once per test binary and is left for the OS to reap.
func New(t *testing.T) *gorm.DB {
	t.Helper()
	once.Do(start)
	if errInit != nil {
		t.Fatalf("start embedded postgres: %v", errInit)
	}
	g, err := db.Open(context.Background(), baseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if s, err := g.DB(); err == nil {
			_ = s.Close()
		}
	})
	if err := g.Exec("TRUNCATE users, sessions, cards, lines").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return g
}
