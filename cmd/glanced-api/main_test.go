package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/dbtest"
)

func TestUserCreate(t *testing.T) {
	g := dbtest.New(t)
	var out bytes.Buffer
	err := userCreate(context.Background(), g, []string{"create", "--email", "ada@example.com"}, strings.NewReader("correct horse battery staple\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("no user id printed")
	}
	if _, err := auth.Login(context.Background(), g, "ada@example.com", "correct horse battery staple"); err != nil {
		t.Errorf("login: %v", err)
	}
}

func TestUserCreateNeedsEmail(t *testing.T) {
	g := dbtest.New(t)
	if err := userCreate(context.Background(), g, []string{"create"}, strings.NewReader("x\n"), &bytes.Buffer{}); err == nil {
		t.Error("want error without --email")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if err := run(context.Background(), []string{"bogus"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Error("want error")
	}
}

func TestRunNeedsDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(context.Background(), []string{"serve"}, strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("err = %v", err)
	}
}

func TestRunUserCreate(t *testing.T) {
	t.Setenv("DATABASE_URL", dbtest.URL(t))
	var out bytes.Buffer
	if err := run(context.Background(), []string{"user", "create", "--email", "bob@example.com"}, strings.NewReader("correct horse battery staple"), &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("no user id printed")
	}
}

func TestRunUserBadSubcommand(t *testing.T) {
	t.Setenv("DATABASE_URL", dbtest.URL(t))
	if err := run(context.Background(), []string{"user", "delete"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Error("want usage error")
	}
}

func TestServeHealthzAndShutdown(t *testing.T) {
	t.Setenv("DATABASE_URL", dbtest.URL(t))
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, strings.NewReader(""), &bytes.Buffer{}) }()
	var code int
	for range 100 {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			code = resp.StatusCode
			_ = resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code != http.StatusOK {
		t.Errorf("healthz = %d", code)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown: %v", err)
	}
}
