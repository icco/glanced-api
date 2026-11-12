package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go.icco.me/glanced-api/internal/db"
)

// SessionTTL is how long a login token stays valid.
const SessionTTL = 180 * 24 * time.Hour

// MinPasswordLen is the shortest password CreateUser accepts.
const MinPasswordLen = 12

// ErrWeakPassword is returned for passwords shorter than MinPasswordLen.
var ErrWeakPassword = errors.New("password must be at least 12 characters")

// ErrInvalidCredentials is returned for an unknown email or a wrong password.
var ErrInvalidCredentials = errors.New("invalid credentials")

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// CreateUser stores a new user with a hashed password.
func CreateUser(ctx context.Context, g *gorm.DB, email, pw string) (*db.User, error) {
	if len(pw) < MinPasswordLen {
		return nil, ErrWeakPassword
	}
	email = normalizeEmail(email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	h, err := HashPassword(pw)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("user id: %w", err)
	}
	u := &db.User{ID: id.String(), Email: email, PasswordHash: h}
	if err := g.WithContext(ctx).Create(u).Error; err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// Login checks credentials and returns a new session token.
func Login(ctx context.Context, g *gorm.DB, email, pw string) (string, error) {
	var u db.User
	err := g.WithContext(ctx).Where("email = ?", normalizeEmail(email)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	ok, err := VerifyPassword(u.PasswordHash, pw)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrInvalidCredentials
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s := db.Session{TokenHash: hashToken(token), UserID: u.ID, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := g.WithContext(ctx).Create(&s).Error; err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// Logout deletes the session for token.
func Logout(ctx context.Context, g *gorm.DB, token string) error {
	if err := g.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Delete(&db.Session{}).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func userForToken(ctx context.Context, g *gorm.DB, token string) (string, error) {
	var s db.Session
	err := g.WithContext(ctx).Where("token_hash = ? AND expires_at > ?", hashToken(token), time.Now()).First(&s).Error
	if err != nil {
		return "", err
	}
	return s.UserID, nil
}
