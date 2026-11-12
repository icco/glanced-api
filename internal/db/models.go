// Package db defines the Postgres schema and connection for glanced-api.
package db

import "time"

// User is an account that owns cards.
type User struct {
	ID           string `gorm:"primaryKey;type:uuid"`
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	CreatedAt    time.Time
}

// Session is a login token, stored only as a SHA-256 hash.
type Session struct {
	TokenHash []byte    `gorm:"primaryKey"`
	UserID    string    `gorm:"index;not null;type:uuid"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

// Card is one Today, Next, or Someday card.
type Card struct {
	UserID      string  `gorm:"primaryKey;type:uuid" json:"-"`
	ID          string  `gorm:"primaryKey" json:"id"`
	Kind        string  `gorm:"not null" json:"kind"`
	Date        *string `json:"date"`
	CardSignal  int     `gorm:"not null" json:"card_signal"`
	UpdatedAtMs int64   `gorm:"column:updated_at;not null;autoUpdateTime:false" json:"updated_at"`
	DeviceID    string  `gorm:"not null" json:"device_id"`
	Deleted     bool    `gorm:"not null" json:"deleted"`
	ServerSeq   int64   `gorm:"index;not null" json:"-"`
}

// Line is one task line on a card.
type Line struct {
	UserID      string  `gorm:"primaryKey;type:uuid" json:"-"`
	ID          string  `gorm:"primaryKey" json:"id"`
	CardID      string  `gorm:"not null" json:"card_id"`
	Position    int     `gorm:"not null" json:"position"`
	Text        string  `gorm:"not null" json:"text"`
	Signal      string  `gorm:"not null" json:"signal"`
	MigratedTo  *string `json:"migrated_to"`
	UpdatedAtMs int64   `gorm:"column:updated_at;not null;autoUpdateTime:false" json:"updated_at"`
	DeviceID    string  `gorm:"not null" json:"device_id"`
	Deleted     bool    `gorm:"not null" json:"deleted"`
	ServerSeq   int64   `gorm:"index;not null" json:"-"`
}
