package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go.icco.me/glanced-api/internal/db"
)

// SessionInfo describes one signed-in device for display.
type SessionInfo struct {
	ID        string
	UserAgent string
	CreatedAt time.Time
	Current   bool
}

func truncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// UserForToken returns the user owning an unexpired session token, or
// ErrInvalidCredentials.
func UserForToken(ctx context.Context, g *gorm.DB, token string) (*db.User, error) {
	var u db.User
	err := g.WithContext(ctx).
		Joins("JOIN sessions ON sessions.user_id = users.id").
		Where("sessions.token_hash = ? AND sessions.expires_at > ?", hashToken(token), time.Now()).
		First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("look up session: %w", err)
	}
	return &u, nil
}

// ListSessions returns the user's unexpired sessions, newest first, marking
// the one for currentToken.
func ListSessions(ctx context.Context, g *gorm.DB, userID, currentToken string) ([]SessionInfo, error) {
	var rows []db.Session
	if err := g.WithContext(ctx).Where("user_id = ? AND expires_at > ?", userID, time.Now()).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	cur := hex.EncodeToString(hashToken(currentToken))
	out := make([]SessionInfo, 0, len(rows))
	for _, s := range rows {
		id := hex.EncodeToString(s.TokenHash)
		out = append(out, SessionInfo{ID: id, UserAgent: s.UserAgent, CreatedAt: s.CreatedAt, Current: id == cur})
	}
	return out, nil
}

// RevokeSession deletes one of the user's sessions by the ID from ListSessions.
func RevokeSession(ctx context.Context, g *gorm.DB, userID, id string) error {
	h, err := hex.DecodeString(id)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadSessionID, err)
	}
	if err := g.WithContext(ctx).Where("user_id = ? AND token_hash = ?", userID, h).Delete(&db.Session{}).Error; err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func checkPassword(ctx context.Context, g *gorm.DB, userID, pw string) error {
	var u db.User
	if err := g.WithContext(ctx).Where("id = ?", userID).First(&u).Error; err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	ok, err := VerifyPassword(u.PasswordHash, pw)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	return nil
}

// ChangePassword replaces the password after checking the current one.
func ChangePassword(ctx context.Context, g *gorm.DB, userID, current, next string) error {
	if err := checkPassword(ctx, g, userID, current); err != nil {
		return err
	}
	if len(next) < MinPasswordLen {
		return ErrWeakPassword
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := g.WithContext(ctx).Model(&db.User{}).Where("id = ?", userID).Update("password_hash", h).Error; err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// DeleteUser removes the user with all their sessions, cards, and lines
// after checking the password.
func DeleteUser(ctx context.Context, g *gorm.DB, userID, pw string) error {
	if err := checkPassword(ctx, g, userID, pw); err != nil {
		return err
	}
	return g.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked db.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&locked).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		for _, m := range []any{&db.Line{}, &db.Card{}, &db.Session{}} {
			if err := tx.Where("user_id = ?", userID).Delete(m).Error; err != nil {
				return fmt.Errorf("delete %T: %w", m, err)
			}
		}
		if err := tx.Where("id = ?", userID).Delete(&db.User{}).Error; err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		return nil
	})
}
