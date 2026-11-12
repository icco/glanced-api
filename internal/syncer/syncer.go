package syncer

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go.icco.me/glanced-api/internal/db"
)

// ErrUnknownUser is returned when pushing for a user that no longer exists.
var ErrUnknownUser = errors.New("unknown user")

// Page is one pull response.
type Page struct {
	Cards  []db.Card
	Lines  []db.Line
	Cursor int64
	More   bool
}

// Push stores each row that is newer than the stored copy and returns how
// many were accepted. Nothing is stored if any row is invalid.
func Push(ctx context.Context, g *gorm.DB, userID string, cards []db.Card, lines []db.Line) (int, error) {
	if err := Validate(cards, lines); err != nil {
		return 0, err
	}
	accepted := 0
	err := g.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var owner db.User
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", userID).First(&owner).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnknownUser
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		for _, c := range cards {
			c.UserID = userID
			n, err := upsert(tx, "cards", &c, &c.ServerSeq)
			if err != nil {
				return fmt.Errorf("store card %q: %w", c.ID, err)
			}
			accepted += n
		}
		for _, l := range lines {
			l.UserID = userID
			n, err := upsert(tx, "lines", &l, &l.ServerSeq)
			if err != nil {
				return fmt.Errorf("store line %q: %w", l.ID, err)
			}
			accepted += n
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return accepted, nil
}

// upsert inserts row, or replaces the stored copy only when row is newer by
// updated_at, then device_id. The check runs inside the statement, so a
// concurrent first insert of the same row cannot overwrite a newer one.
func upsert(tx *gorm.DB, table string, row any, seq *int64) (int, error) {
	if err := tx.Raw("SELECT nextval('glanced_seq')").Scan(seq).Error; err != nil {
		return 0, err
	}
	newer := fmt.Sprintf("excluded.updated_at > %[1]s.updated_at OR (excluded.updated_at = %[1]s.updated_at AND excluded.device_id > %[1]s.device_id)", table)
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "id"}},
		UpdateAll: true,
		Where:     clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: newer}}},
	}).Create(row)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// MaxPullLimit caps how many rows one pull returns.
const MaxPullLimit = 1000

// Pull returns up to limit rows changed after cursor, oldest first.
func Pull(ctx context.Context, g *gorm.DB, userID string, cursor int64, limit int) (Page, error) {
	limit = max(1, min(limit, MaxPullLimit))
	p := Page{Cards: []db.Card{}, Lines: []db.Line{}, Cursor: cursor}
	var cards []db.Card
	var lines []db.Line
	q := g.WithContext(ctx).Where("user_id = ? AND server_seq > ?", userID, cursor).Order("server_seq").Limit(limit + 1)
	if err := q.Find(&cards).Error; err != nil {
		return p, fmt.Errorf("pull cards: %w", err)
	}
	q = g.WithContext(ctx).Where("user_id = ? AND server_seq > ?", userID, cursor).Order("server_seq").Limit(limit + 1)
	if err := q.Find(&lines).Error; err != nil {
		return p, fmt.Errorf("pull lines: %w", err)
	}
	i, j := 0, 0
	for len(p.Cards)+len(p.Lines) < limit && (i < len(cards) || j < len(lines)) {
		if j >= len(lines) || (i < len(cards) && cards[i].ServerSeq < lines[j].ServerSeq) {
			p.Cards = append(p.Cards, cards[i])
			p.Cursor = cards[i].ServerSeq
			i++
		} else {
			p.Lines = append(p.Lines, lines[j])
			p.Cursor = lines[j].ServerSeq
			j++
		}
	}
	p.More = i < len(cards) || j < len(lines)
	return p, nil
}
