// Package syncer applies pushed card and line rows with last-write-wins and
// pages changed rows back out by server sequence.
package syncer

import (
	"fmt"
	"time"

	"go.icco.me/glanced-api/internal/db"
)

// InvalidRowError names the first pushed row that failed validation.
type InvalidRowError struct {
	ID     string
	Reason string
}

func (e *InvalidRowError) Error() string {
	return fmt.Sprintf("invalid row %q: %s", e.ID, e.Reason)
}

var (
	kinds   = map[string]bool{"today": true, "next": true, "someday": true}
	signals = map[string]bool{"empty": true, "half": true, "done": true, "delegated": true, "migrated": true}
)

// Validate checks every row and returns an *InvalidRowError for the first bad one.
func Validate(cards []db.Card, lines []db.Line) error {
	for _, c := range cards {
		if reason := validateCard(c); reason != "" {
			return &InvalidRowError{ID: c.ID, Reason: reason}
		}
	}
	for _, l := range lines {
		if reason := validateLine(l); reason != "" {
			return &InvalidRowError{ID: l.ID, Reason: reason}
		}
	}
	return nil
}

func validateCard(c db.Card) string {
	switch {
	case c.ID == "":
		return "id is required"
	case c.DeviceID == "":
		return "device_id is required"
	case !kinds[c.Kind]:
		return "kind must be today, next, or someday"
	case c.CardSignal < 0 || c.CardSignal > 3:
		return "card_signal must be 0 to 3"
	case c.Kind == "today" && c.Date == nil:
		return "today cards need a date"
	case c.Kind != "today" && c.Date != nil:
		return "only today cards have a date"
	}
	if c.Date != nil {
		if _, err := time.Parse(time.DateOnly, *c.Date); err != nil {
			return "date must be YYYY-MM-DD"
		}
	}
	return ""
}

func validateLine(l db.Line) string {
	switch {
	case l.ID == "":
		return "id is required"
	case l.DeviceID == "":
		return "device_id is required"
	case l.CardID == "":
		return "card_id is required"
	case !signals[l.Signal]:
		return "signal must be empty, half, done, delegated, or migrated"
	case l.Position < 0:
		return "position must be >= 0"
	}
	return ""
}
