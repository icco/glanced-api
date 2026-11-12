package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"go.icco.me/gutil/logging"
	"go.icco.me/gutil/render"
	"gorm.io/gorm"
)

type ctxKey struct{}

type tokenKey struct{}

// Middleware rejects requests without a valid bearer token and stores the
// caller's user ID in the request context.
func Middleware(g *gorm.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logging.FromContext(r.Context())
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				render.JSON(log, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			u, err := UserForToken(r.Context(), g, token)
			if errors.Is(err, ErrInvalidCredentials) {
				render.JSON(log, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			if err != nil {
				log.Errorw("look up session", "error", err)
				render.JSON(log, w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, u.ID)
			ctx = context.WithValue(ctx, tokenKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID returns the authenticated user's ID, or "" outside Middleware.
func UserID(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

// Token returns the caller's bearer token, or "" outside Middleware.
func Token(ctx context.Context) string {
	s, _ := ctx.Value(tokenKey{}).(string)
	return s
}
