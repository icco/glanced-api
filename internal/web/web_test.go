package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/dbtest"
	"go.icco.me/glanced-api/internal/web"
)

const pw = "correct horse battery staple"

type result struct {
	code     int
	body     string
	location string
	cookie   *http.Cookie
}

func do(t *testing.T, h http.Handler, method, path, cookie string, form url.Values) result {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", "Firefox")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: web.CookieName, Value: cookie, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := result{code: rec.Code, body: rec.Body.String(), location: rec.Header().Get("Location")}
	for _, c := range rec.Result().Cookies() {
		if c.Name == web.CookieName {
			res.cookie = c
		}
	}
	return res
}

func signup(t *testing.T, h http.Handler) string {
	t.Helper()
	res := do(t, h, http.MethodPost, "/signup", "", url.Values{"email": {"ada@example.com"}, "password": {pw}})
	if res.code != http.StatusSeeOther || res.location != "/account" || res.cookie == nil {
		t.Fatalf("signup = %d %q cookie=%v body=%s", res.code, res.location, res.cookie, res.body)
	}
	if !res.cookie.HttpOnly || !res.cookie.Secure || res.cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie flags: %+v", res.cookie)
	}
	return res.cookie.Value
}

func setup(t *testing.T) (*gorm.DB, http.Handler) {
	t.Helper()
	g := dbtest.New(t)
	return g, web.New(g)
}

func TestPagesRender(t *testing.T) {
	_, h := setup(t)
	for _, p := range []string{"/signup", "/login"} {
		if res := do(t, h, http.MethodGet, p, "", nil); res.code != http.StatusOK || !strings.Contains(res.body, "<form") {
			t.Errorf("%s = %d", p, res.code)
		}
	}
	if res := do(t, h, http.MethodGet, "/", "", nil); res.code != http.StatusSeeOther || res.location != "/login" {
		t.Errorf("/ = %d %q", res.code, res.location)
	}
}

func TestSignupCreatesAccountAndSignsIn(t *testing.T) {
	_, h := setup(t)
	tok := signup(t, h)
	res := do(t, h, http.MethodGet, "/account", tok, nil)
	if res.code != http.StatusOK || !strings.Contains(res.body, "ada@example.com") || !strings.Contains(res.body, "Firefox") {
		t.Errorf("account = %d %s", res.code, res.body)
	}
}

func TestSignupErrors(t *testing.T) {
	_, h := setup(t)
	signup(t, h)
	cases := map[string]url.Values{
		"at least 12 characters": {"email": {"bob@example.com"}, "password": {"short"}},
		"already exists":         {"email": {"ADA@example.com"}, "password": {pw}},
		"Enter a valid email":    {"email": {"nope"}, "password": {pw}},
	}
	for want, form := range cases {
		res := do(t, h, http.MethodPost, "/signup", "", form)
		if res.code != http.StatusBadRequest || !strings.Contains(res.body, want) || res.cookie != nil {
			t.Errorf("%s: %d %s", want, res.code, res.body)
		}
	}
}

func TestLogin(t *testing.T) {
	_, h := setup(t)
	signup(t, h)
	bad := do(t, h, http.MethodPost, "/login", "", url.Values{"email": {"ada@example.com"}, "password": {"wrong password"}})
	if bad.code != http.StatusUnauthorized || !strings.Contains(bad.body, "Wrong email or password") {
		t.Errorf("bad login = %d %s", bad.code, bad.body)
	}
	good := do(t, h, http.MethodPost, "/login", "", url.Values{"email": {"ada@example.com"}, "password": {pw}})
	if good.code != http.StatusSeeOther || good.cookie == nil {
		t.Errorf("good login = %d", good.code)
	}
}

func TestAccountRequiresLogin(t *testing.T) {
	_, h := setup(t)
	for _, tok := range []string{"", "bogus"} {
		if res := do(t, h, http.MethodGet, "/account", tok, nil); res.code != http.StatusSeeOther || res.location != "/login" {
			t.Errorf("token %q: %d %q", tok, res.code, res.location)
		}
	}
}

func TestChangePasswordPage(t *testing.T) {
	g, h := setup(t)
	tok := signup(t, h)
	bad := do(t, h, http.MethodPost, "/account/password", tok, url.Values{"current": {"nope nope nope"}, "new": {"another long passphrase"}})
	if bad.code != http.StatusBadRequest || !strings.Contains(bad.body, "Current password is wrong") {
		t.Errorf("bad = %d %s", bad.code, bad.body)
	}
	ok := do(t, h, http.MethodPost, "/account/password", tok, url.Values{"current": {pw}, "new": {"another long passphrase"}})
	if ok.code != http.StatusSeeOther {
		t.Errorf("ok = %d %s", ok.code, ok.body)
	}
	if _, err := auth.Login(context.Background(), g, "ada@example.com", "another long passphrase"); err != nil {
		t.Errorf("new password: %v", err)
	}
}

func TestRevokeSessionPage(t *testing.T) {
	g, h := setup(t)
	tok := signup(t, h)
	phone, err := auth.LoginAgent(context.Background(), g, "ada@example.com", pw, "Glanced Android")
	if err != nil {
		t.Fatal(err)
	}
	u, err := auth.UserForToken(context.Background(), g, tok)
	if err != nil {
		t.Fatal(err)
	}
	list, err := auth.ListSessions(context.Background(), g, u.ID, tok)
	if err != nil {
		t.Fatal(err)
	}
	var phoneID string
	for _, s := range list {
		if !s.Current {
			phoneID = s.ID
		}
	}
	if res := do(t, h, http.MethodPost, "/account/sessions/revoke", tok, url.Values{"id": {phoneID}}); res.code != http.StatusSeeOther {
		t.Fatalf("revoke = %d %s", res.code, res.body)
	}
	if _, err := auth.UserForToken(context.Background(), g, phone); err == nil {
		t.Error("phone session still valid")
	}
}

func TestDeleteAccountPage(t *testing.T) {
	g, h := setup(t)
	tok := signup(t, h)
	if res := do(t, h, http.MethodPost, "/account/delete", tok, url.Values{"password": {"wrong password!"}}); res.code != http.StatusBadRequest {
		t.Errorf("wrong password = %d", res.code)
	}
	res := do(t, h, http.MethodPost, "/account/delete", tok, url.Values{"password": {pw}})
	if res.code != http.StatusSeeOther || res.location != "/signup" || res.cookie == nil || res.cookie.MaxAge >= 0 {
		t.Errorf("delete = %d %q %+v", res.code, res.location, res.cookie)
	}
	var n int64
	g.Model(&db.User{}).Count(&n)
	if n != 0 {
		t.Errorf("users left = %d", n)
	}
}

func TestLogoutPage(t *testing.T) {
	g, h := setup(t)
	tok := signup(t, h)
	res := do(t, h, http.MethodPost, "/logout", tok, url.Values{})
	if res.code != http.StatusSeeOther || res.cookie == nil || res.cookie.MaxAge >= 0 {
		t.Errorf("logout = %d %+v", res.code, res.cookie)
	}
	if _, err := auth.UserForToken(context.Background(), g, tok); err == nil {
		t.Error("session still valid")
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	_, h := setup(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/signup", strings.NewReader("email=eve%40example.com&password=correct+horse+battery"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestSignedInRootAndNotices(t *testing.T) {
	_, h := setup(t)
	tok := signup(t, h)
	if res := do(t, h, http.MethodGet, "/", tok, nil); res.location != "/account" {
		t.Errorf("/ = %q", res.location)
	}
	for q, want := range map[string]string{"password": "Password changed.", "revoked": "Device signed out."} {
		if res := do(t, h, http.MethodGet, "/account?done="+q, tok, nil); !strings.Contains(res.body, want) {
			t.Errorf("%s notice missing", q)
		}
	}
	weak := do(t, h, http.MethodPost, "/account/password", tok, url.Values{"current": {pw}, "new": {"short"}})
	if weak.code != http.StatusBadRequest || !strings.Contains(weak.body, "at least 12") {
		t.Errorf("weak = %d", weak.code)
	}
}

func TestRevokeBadIDIs400(t *testing.T) {
	_, h := setup(t)
	tok := signup(t, h)
	if res := do(t, h, http.MethodPost, "/account/sessions/revoke", tok, url.Values{"id": {"zz"}}); res.code != http.StatusBadRequest {
		t.Errorf("code = %d", res.code)
	}
}

func TestSignedOutPostsRedirect(t *testing.T) {
	_, h := setup(t)
	for _, p := range []string{"/account/password", "/account/sessions/revoke", "/account/delete"} {
		if res := do(t, h, http.MethodPost, p, "", url.Values{}); res.code != http.StatusSeeOther || res.location != "/login" {
			t.Errorf("%s = %d %q", p, res.code, res.location)
		}
	}
	if res := do(t, h, http.MethodPost, "/logout", "", url.Values{}); res.code != http.StatusSeeOther {
		t.Errorf("logout = %d", res.code)
	}
}

func TestSignupRateLimitedPerIP(t *testing.T) {
	_, h := setup(t)
	post := func(i int, ip string) int {
		form := url.Values{"email": {fmt.Sprintf("user%d@example.com", i)}, "password": {pw}}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := range web.SignupsPerHour {
		if code := post(i, "192.0.2.1"); code != http.StatusSeeOther {
			t.Fatalf("signup %d = %d", i, code)
		}
	}
	if code := post(100, "192.0.2.1"); code != http.StatusTooManyRequests {
		t.Errorf("over limit = %d", code)
	}
	if code := post(102, "203.0.113.9, 192.0.2.1"); code != http.StatusTooManyRequests {
		t.Errorf("forged left entry escaped the limit: %d", code)
	}
	if code := post(101, "198.51.100.7"); code != http.StatusSeeOther {
		t.Errorf("other ip = %d", code)
	}
}

func TestClientIP(t *testing.T) {
	cases := map[string]struct{ xff, remote, want string }{
		"no header":      {"", "192.0.2.5:4000", "192.0.2.5"},
		"single":         {"198.51.100.7", "10.0.0.2:4000", "198.51.100.7"},
		"rightmost wins": {"203.0.113.9, 198.51.100.7", "10.0.0.2:4000", "198.51.100.7"},
		"garbage":        {"not-an-ip", "192.0.2.5:4000", "192.0.2.5"},
	}
	for name, tc := range cases {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remote
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		got, err := web.ClientIP(req)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q %v, want %q", name, got, err, tc.want)
		}
	}
}
