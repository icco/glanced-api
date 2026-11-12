package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
)

const pw = "correct horse battery staple"

func TestHashVerifyRoundTrip(t *testing.T) {
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=1,p=4$") {
		t.Errorf("hash = %q", h)
	}
	if ok, err := auth.VerifyPassword(h, pw); err != nil || !ok {
		t.Errorf("verify correct = %v, %v", ok, err)
	}
	if ok, err := auth.VerifyPassword(h, "wrong"); err != nil || ok {
		t.Errorf("verify wrong = %v, %v", ok, err)
	}
}

func TestCreateUserRejectsWeakPassword(t *testing.T) {
	g := dbtest.New(t)
	if _, err := auth.CreateUser(context.Background(), g, "ada@example.com", "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("err = %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Login(ctx, g, "ada@example.com", "nope nope nope"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("err = %v", err)
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	g := dbtest.New(t)
	if _, err := auth.Login(context.Background(), g, "bob@example.com", pw); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("err = %v", err)
	}
}

func TestEmailCaseInsensitive(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, g, "Ada@Example.com", pw); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Login(ctx, g, "ada@example.com", pw); err != nil {
		t.Errorf("login: %v", err)
	}
}

func TestMiddlewareSetsUserID(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Login(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = auth.UserID(r.Context()) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	auth.Middleware(g)(inner).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || seen != u.ID {
		t.Errorf("code = %d, user = %q, want %q", rec.Code, seen, u.ID)
	}
}

func TestMiddlewareRejectsMissing(t *testing.T) {
	g := dbtest.New(t)
	rec := httptest.NewRecorder()
	auth.Middleware(g)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"unauthorized"`) {
		t.Errorf("code = %d body = %s", rec.Code, rec.Body)
	}
}

func TestMiddlewareRejectsExpired(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Login(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Model(&db.Session{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	auth.Middleware(g)(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	g := dbtest.New(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	tok, err := auth.Login(ctx, g, "ada@example.com", pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, g, tok); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	auth.Middleware(g)(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", rec.Code)
	}
}
