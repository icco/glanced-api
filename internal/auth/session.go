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

// ErrBadSessionID is returned for a session ID that is not hex.
var ErrBadSessionID = errors.New("bad session id")

// ErrEmailTaken is returned when an account already uses the email.
var ErrEmailTaken = errors.New("an account with that email already exists")

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
	var n int64
	if err := g.WithContext(ctx).Model(&db.User{}).Where("email = ?", email).Count(&n).Error; err != nil {
		return nil, fmt.Errorf("check email: %w", err)
	}
	if n > 0 {
		return nil, ErrEmailTaken
	}
	if err := g.WithContext(ctx).Create(u).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "SQLSTATE 23505") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// Login checks credentials and returns a new session token.
func Login(ctx context.Context, g *gorm.DB, email, pw string) (string, error) {
	return LoginAgent(ctx, g, email, pw, "")
}

// LoginAgent is Login that labels the session with the client's user agent.
func LoginAgent(ctx context.Context, g *gorm.DB, email, pw, userAgent string) (string, error) {
	var u db.User
	err := g.WithContext(ctx).Where("email = ?", normalizeEmail(email)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if _, verr := VerifyPassword(dummyHash(), pw); verr != nil {
			return "", verr
		}
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
	s := db.Session{TokenHash: hashToken(token), UserID: u.ID, ExpiresAt: time.Now().Add(SessionTTL), UserAgent: truncate(userAgent, 200)}
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
