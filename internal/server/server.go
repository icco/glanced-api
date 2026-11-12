// Package server wires the glanced-api HTTP routes.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.icco.me/gutil/logging"
	"go.icco.me/gutil/render"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/auth"
	"go.icco.me/glanced-api/internal/db"
	"go.icco.me/glanced-api/internal/syncer"
	"go.icco.me/glanced-api/internal/web"
)

const (
	maxBody          = 5 << 20
	defaultPullLimit = 500
)

type handler struct {
	g *gorm.DB
}

// New returns the API handler.
func New(g *gorm.DB, log *zap.Logger) http.Handler {
	h := &handler{g: g}
	r := chi.NewRouter()
	r.Use(logging.Middleware(log))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		render.JSON(logging.FromContext(r.Context()), w, http.StatusOK, map[string]bool{"ok": true})
	})
	r.Post("/v1/auth/login", h.login)
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(g))
		r.Post("/v1/auth/logout", h.logout)
		r.Post("/v1/sync/push", h.push)
		r.Get("/v1/sync/pull", h.pull)
	})
	r.Mount("/", web.New(g))
	return r
}

func fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	render.JSON(logging.FromContext(r.Context()), w, status, map[string]string{"error": msg})
}

func internal(w http.ResponseWriter, r *http.Request, err error) {
	logging.FromContext(r.Context()).Errorw("request failed", "error", err)
	fail(w, r, http.StatusInternalServerError, "internal error")
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&in); err != nil {
		fail(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	token, err := auth.LoginAgent(r.Context(), h.g, in.Email, in.Password, r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		fail(w, r, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	render.JSON(logging.FromContext(r.Context()), w, http.StatusOK, map[string]string{"token": token})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := auth.Logout(r.Context(), h.g, auth.Token(r.Context())); err != nil {
		internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string    `json:"device_id"`
		Cards    []db.Card `json:"cards"`
		Lines    []db.Line `json:"lines"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&in); err != nil {
		fail(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := syncer.Push(r.Context(), h.g, auth.UserID(r.Context()), in.Cards, in.Lines)
	var inv *syncer.InvalidRowError
	if errors.As(err, &inv) {
		render.JSON(logging.FromContext(r.Context()), w, http.StatusBadRequest, map[string]string{"error": "invalid row", "id": inv.ID, "reason": inv.Reason})
		return
	}
	if err != nil {
		internal(w, r, err)
		return
	}
	render.JSON(logging.FromContext(r.Context()), w, http.StatusOK, map[string]int{"accepted": n})
}

func intParam(r *http.Request, name string, def int64) (int64, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func (h *handler) pull(w http.ResponseWriter, r *http.Request) {
	cursor, err := intParam(r, "cursor", 0)
	if err != nil || cursor < 0 {
		fail(w, r, http.StatusBadRequest, "invalid cursor")
		return
	}
	limit := defaultPullLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
	}
	if err != nil {
		fail(w, r, http.StatusBadRequest, "invalid limit")
		return
	}
	p, err := syncer.Pull(r.Context(), h.g, auth.UserID(r.Context()), cursor, limit)
	if err != nil {
		internal(w, r, err)
		return
	}
	render.JSON(logging.FromContext(r.Context()), w, http.StatusOK, map[string]any{
		"cards": p.Cards, "lines": p.Lines, "cursor": p.Cursor, "more": p.More,
	})
}
