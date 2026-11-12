// Package web serves the sign-up, sign-in, and account pages.
package web

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.icco.me/gutil/logging"
	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
)

// CookieName is the session cookie set by the web pages.
const CookieName = "glanced_session"

//go:embed templates/*.html
var files embed.FS

var pages = map[string]*template.Template{}

func init() {
	for _, name := range []string{"signup", "login", "account"} {
		pages[name] = template.Must(template.ParseFS(files, "templates/layout.html", "templates/"+name+".html"))
	}
}

type view struct {
	Title    string
	Error    string
	Notice   string
	Email    string
	Sessions []auth.SessionInfo
}

type handler struct {
	g *gorm.DB
}

// New returns the web handler. Cross-origin form posts are rejected.
func New(g *gorm.DB) http.Handler {
	h := &handler{g: g}
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := h.current(r); ok {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	r.Get("/signup", func(w http.ResponseWriter, r *http.Request) {
		h.render(w, r, http.StatusOK, "signup", view{Title: "Create account"})
	})
	r.Post("/signup", h.signup)
	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		h.render(w, r, http.StatusOK, "login", view{Title: "Sign in"})
	})
	r.Post("/login", h.login)
	r.Post("/logout", h.logout)
	r.Get("/account", h.account)
	r.Post("/account/password", h.changePassword)
	r.Post("/account/sessions/revoke", h.revoke)
	r.Post("/account/delete", h.deleteAccount)
	return http.NewCrossOriginProtection().Handler(r)
}

func (h *handler) render(w http.ResponseWriter, r *http.Request, status int, page string, v view) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := pages[page].ExecuteTemplate(w, "layout", v); err != nil {
		logging.FromContext(r.Context()).Errorw("render page", "page", page, "error", err)
	}
}

func (h *handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	logging.FromContext(r.Context()).Errorw("web request failed", "error", err)
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}

func setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.SessionTTL),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func (h *handler) current(r *http.Request) (*db.User, string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, "", false
	}
	u, err := auth.UserForToken(r.Context(), h.g, c.Value)
	if err != nil {
		return nil, "", false
	}
	return u, c.Value, true
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Name == "" && a.Address == s
}

func (h *handler) signup(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	pw := r.PostFormValue("password")
	v := view{Title: "Create account", Email: email}
	if !validEmail(email) {
		v.Error = "Enter a valid email address."
		h.render(w, r, http.StatusBadRequest, "signup", v)
		return
	}
	_, err := auth.CreateUser(r.Context(), h.g, email, pw)
	switch {
	case errors.Is(err, auth.ErrWeakPassword):
		v.Error = "Use a password of at least 12 characters."
	case errors.Is(err, auth.ErrEmailTaken):
		v.Error = "An account with that email already exists."
	case err != nil:
		h.internal(w, r, err)
		return
	}
	if v.Error != "" {
		h.render(w, r, http.StatusBadRequest, "signup", v)
		return
	}
	token, err := auth.LoginAgent(r.Context(), h.g, email, pw, r.UserAgent())
	if err != nil {
		h.internal(w, r, err)
		return
	}
	setCookie(w, token)
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	token, err := auth.LoginAgent(r.Context(), h.g, email, r.PostFormValue("password"), r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		h.render(w, r, http.StatusUnauthorized, "login", view{Title: "Sign in", Email: email, Error: "Wrong email or password."})
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	setCookie(w, token)
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if _, token, ok := h.current(r); ok {
		if err := auth.Logout(r.Context(), h.g, token); err != nil {
			h.internal(w, r, err)
			return
		}
	}
	clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *handler) accountView(r *http.Request, u *db.User, token string) (view, error) {
	sessions, err := auth.ListSessions(r.Context(), h.g, u.ID, token)
	return view{Title: "Your account", Email: u.Email, Sessions: sessions}, err
}

func (h *handler) account(w http.ResponseWriter, r *http.Request) {
	u, token, ok := h.current(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	v, err := h.accountView(r, u, token)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	switch r.URL.Query().Get("done") {
	case "password":
		v.Notice = "Password changed."
	case "revoked":
		v.Notice = "Device signed out."
	}
	h.render(w, r, http.StatusOK, "account", v)
}

func (h *handler) accountError(w http.ResponseWriter, r *http.Request, u *db.User, token, msg string) {
	v, err := h.accountView(r, u, token)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	v.Error = msg
	h.render(w, r, http.StatusBadRequest, "account", v)
}

func (h *handler) changePassword(w http.ResponseWriter, r *http.Request) {
	u, token, ok := h.current(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	err := auth.ChangePassword(r.Context(), h.g, u.ID, r.PostFormValue("current"), r.PostFormValue("new"))
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		h.accountError(w, r, u, token, "Current password is wrong.")
	case errors.Is(err, auth.ErrWeakPassword):
		h.accountError(w, r, u, token, "Use a new password of at least 12 characters.")
	case err != nil:
		h.internal(w, r, err)
	default:
		http.Redirect(w, r, "/account?done=password", http.StatusSeeOther)
	}
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	u, _, ok := h.current(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	err := auth.RevokeSession(r.Context(), h.g, u.ID, r.PostFormValue("id"))
	if errors.Is(err, auth.ErrBadSessionID) {
		http.Error(w, "Unknown device.", http.StatusBadRequest)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?done=revoked", http.StatusSeeOther)
}

func (h *handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, token, ok := h.current(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	err := auth.DeleteUser(r.Context(), h.g, u.ID, r.PostFormValue("password"))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		h.accountError(w, r, u, token, "Password is wrong; account not deleted.")
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	clearCookie(w)
	http.Redirect(w, r, "/signup", http.StatusSeeOther)
}
