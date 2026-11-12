package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
	"go.icco.me/glanced-api/internal/server"
)

const pw = "correct horse battery staple"

type result struct {
	code int
	body []byte
}

func (r result) compact() []byte {
	return bytes.ReplaceAll(bytes.ReplaceAll(r.body, []byte(" "), nil), []byte("\n"), nil)
}

func (r result) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func setup(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	g := dbtest.New(t)
	if _, err := auth.CreateUser(context.Background(), g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(g, zap.NewNop()))
	t.Cleanup(ts.Close)
	res := do(t, ts, http.MethodPost, "/v1/auth/login", "", `{"email":"ada@example.com","password":"`+pw+`"}`)
	if res.code != http.StatusOK {
		t.Fatalf("login = %d", res.code)
	}
	var out struct{ Token string }
	res.decode(t, &out)
	return ts, out.Token
}

func do(t *testing.T, ts *httptest.Server, method, path, token, body string) result {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return result{code: resp.StatusCode, body: b}
}

const pushBody = `{"device_id":"dev-a","cards":[{"id":"next","kind":"next","date":null,"card_signal":0,"updated_at":100,"device_id":"dev-a","deleted":false}],"lines":[{"id":"l1","card_id":"next","position":0,"text":"Write the analytical engine notes","signal":"empty","migrated_to":null,"updated_at":100,"device_id":"dev-a","deleted":false}]}`

func TestHealthz(t *testing.T) {
	ts, _ := setup(t)
	if res := do(t, ts, http.MethodGet, "/healthz", "", ""); res.code != http.StatusOK {
		t.Errorf("healthz = %d", res.code)
	}
}

func TestLoginBadPassword(t *testing.T) {
	ts, _ := setup(t)
	if res := do(t, ts, http.MethodPost, "/v1/auth/login", "", `{"email":"ada@example.com","password":"nope"}`); res.code != http.StatusUnauthorized {
		t.Errorf("code = %d", res.code)
	}
}

func TestLoginBadJSON(t *testing.T) {
	ts, _ := setup(t)
	if res := do(t, ts, http.MethodPost, "/v1/auth/login", "", `{`); res.code != http.StatusBadRequest {
		t.Errorf("code = %d", res.code)
	}
}

func TestLoginThenPushPull(t *testing.T) {
	ts, tok := setup(t)
	res := do(t, ts, http.MethodPost, "/v1/sync/push", tok, pushBody)
	var pushed struct{ Accepted int }
	res.decode(t, &pushed)
	if res.code != http.StatusOK || pushed.Accepted != 2 {
		t.Fatalf("push = %d %+v", res.code, pushed)
	}
	var page struct {
		Cards  []db.Card
		Lines  []db.Line
		Cursor int64
		More   bool
	}
	do(t, ts, http.MethodGet, "/v1/sync/pull?cursor=0", tok, "").decode(t, &page)
	if len(page.Cards) != 1 || len(page.Lines) != 1 || page.Lines[0].Text != "Write the analytical engine notes" || page.Cursor == 0 || page.More {
		t.Errorf("page = %+v", page)
	}
}

func TestPushRejectsInvalidRow(t *testing.T) {
	ts, tok := setup(t)
	bad := strings.Replace(pushBody, `"signal":"empty"`, `"signal":"maybe"`, 1)
	res := do(t, ts, http.MethodPost, "/v1/sync/push", tok, bad)
	var out map[string]string
	res.decode(t, &out)
	if res.code != http.StatusBadRequest || out["id"] != "l1" || out["error"] != "invalid row" {
		t.Errorf("code = %d body = %v", res.code, out)
	}
	if got := do(t, ts, http.MethodGet, "/v1/sync/pull", tok, "").compact(); !bytes.Contains(got, []byte(`"cards":[]`)) {
		t.Errorf("stored rows after invalid push: %s", got)
	}
}

func TestPushRejectsBadJSON(t *testing.T) {
	ts, tok := setup(t)
	if res := do(t, ts, http.MethodPost, "/v1/sync/push", tok, "{"); res.code != http.StatusBadRequest {
		t.Errorf("code = %d", res.code)
	}
}

func TestUnauthenticatedPull401(t *testing.T) {
	ts, _ := setup(t)
	if res := do(t, ts, http.MethodGet, "/v1/sync/pull", "", ""); res.code != http.StatusUnauthorized {
		t.Errorf("code = %d", res.code)
	}
}

func TestLogoutRevokes(t *testing.T) {
	ts, tok := setup(t)
	if res := do(t, ts, http.MethodPost, "/v1/auth/logout", tok, ""); res.code != http.StatusNoContent {
		t.Fatalf("logout = %d", res.code)
	}
	if res := do(t, ts, http.MethodGet, "/v1/sync/pull", tok, ""); res.code != http.StatusUnauthorized {
		t.Errorf("pull after logout = %d", res.code)
	}
}

func TestPullEmptyListsAreArrays(t *testing.T) {
	ts, tok := setup(t)
	got := do(t, ts, http.MethodGet, "/v1/sync/pull?cursor=0&limit=10", tok, "").compact()
	if !bytes.Contains(got, []byte(`"cards":[]`)) || !bytes.Contains(got, []byte(`"lines":[]`)) {
		t.Errorf("body = %s", got)
	}
}

func TestPullBadParams(t *testing.T) {
	ts, tok := setup(t)
	for _, q := range []string{"cursor=abc", "cursor=-1", "limit=x"} {
		if res := do(t, ts, http.MethodGet, "/v1/sync/pull?"+q, tok, ""); res.code != http.StatusBadRequest {
			t.Errorf("%s: code = %d", q, res.code)
		}
	}
}

func TestDatabaseDownIs500(t *testing.T) {
	g := dbtest.New(t)
	if _, err := auth.CreateUser(context.Background(), g, "ada@example.com", pw); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(g, zap.NewNop()))
	t.Cleanup(ts.Close)
	var out struct{ Token string }
	do(t, ts, http.MethodPost, "/v1/auth/login", "", `{"email":"ada@example.com","password":"`+pw+`"}`).decode(t, &out)
	s, err := g.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/sync/pull", "/v1/auth/login"} {
		method := http.MethodGet
		body := ""
		if path == "/v1/auth/login" {
			method, body = http.MethodPost, `{"email":"ada@example.com","password":"x"}`
		}
		if res := do(t, ts, method, path, out.Token, body); res.code != http.StatusInternalServerError {
			t.Errorf("%s: code = %d", path, res.code)
		}
	}
}
