package syncer

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go.icco.me/glanced-api/internal/db"
)

// Page is one pull response.
type Page struct {
	Cards  []db.Card
	Lines  []db.Line
	Cursor int64
	More   bool
}

func wins(inAt int64, inDev string, curAt int64, curDev string) bool {
	return inAt > curAt || (inAt == curAt && inDev > curDev)
}

// Push stores each row that is newer than the stored copy and returns how
// many were accepted. Nothing is stored if any row is invalid.
func Push(ctx context.Context, g *gorm.DB, userID string, cards []db.Card, lines []db.Line) (int, error) {
	if err := Validate(cards, lines); err != nil {
		return 0, err
	}
	accepted := 0
	err := g.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range cards {
			c.UserID = userID
			var cur db.Card
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id = ?", userID, c.ID).First(&cur).Error
			ok, err := shouldWrite(err, func() bool { return wins(c.UpdatedAtMs, c.DeviceID, cur.UpdatedAtMs, cur.DeviceID) })
			if err != nil || !ok {
				if err != nil {
					return fmt.Errorf("load card %q: %w", c.ID, err)
				}
				continue
			}
			if err := upsert(tx, &c, &c.ServerSeq); err != nil {
				return fmt.Errorf("store card %q: %w", c.ID, err)
			}
			accepted++
		}
		for _, l := range lines {
			l.UserID = userID
			var cur db.Line
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND id = ?", userID, l.ID).First(&cur).Error
			ok, err := shouldWrite(err, func() bool { return wins(l.UpdatedAtMs, l.DeviceID, cur.UpdatedAtMs, cur.DeviceID) })
			if err != nil || !ok {
				if err != nil {
					return fmt.Errorf("load line %q: %w", l.ID, err)
				}
				continue
			}
			if err := upsert(tx, &l, &l.ServerSeq); err != nil {
				return fmt.Errorf("store line %q: %w", l.ID, err)
			}
			accepted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return accepted, nil
}

func shouldWrite(loadErr error, newer func() bool) (bool, error) {
	if errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if loadErr != nil {
		return false, loadErr
	}
	return newer(), nil
}

func upsert(tx *gorm.DB, row any, seq *int64) error {
	if err := tx.Raw("SELECT nextval('glanced_seq')").Scan(seq).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error
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
