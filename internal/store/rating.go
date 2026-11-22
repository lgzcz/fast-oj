package store

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"time"
)

// Elo 的参数故意写得保守：K=32 是棋类与多数 OJ 的常见档位，起始 1200。
// 只有 host 明确勾了「计入 rating」的比赛才结算，而且每场只结算一次。
const (
	DefaultRating = 1200
	ratingK       = 32.0
	minRating     = 100
	maxRating     = 4000
)

type ContestRating struct {
	ContestID int64
	UserID    int64
	Before    int
	After     int
	Delta     int
	CreatedAt int64
}

func (db *DB) pendingRatedContests(ctx context.Context, limit int) ([]Contest, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT `+contestColumns+` FROM contests
		 WHERE IFNULL(rated, 0)=1 AND IFNULL(rating_done, 0)=0 AND end_at<=?
		 ORDER BY end_at LIMIT ?`, nowMS(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contest
	for rows.Next() {
		c, err := scanContest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ApplyContestRatings scores one ended contest. It is safe to call twice: the
// transaction re-checks rating_done before touching any row.
func (db *DB) ApplyContestRatings(ctx context.Context, c Contest) (int, error) {
	rows, err := db.ContestScoreboard(ctx, c)
	if err != nil {
		return 0, err
	}
	// 同一个人可能只能出现一次；队伍模式下整队共享名次。
	rank := map[int64]int{}
	for _, r := range rows {
		if r.Rank <= 0 {
			continue
		}
		for _, uid := range r.UserIDs {
			if uid != 0 {
				rank[uid] = r.Rank
			}
		}
	}
	applied := 0
	err = db.InTx(ctx, func(tx *sql.Tx) error {
		var done int
		if err := tx.QueryRowContext(ctx,
			`SELECT IFNULL(rating_done, 0) FROM contests WHERE id=?`, c.ID).Scan(&done); err != nil {
			return err
		}
		if done != 0 {
			return nil
		}
		if len(rank) < 2 {
			_, err := tx.ExecContext(ctx, `UPDATE contests SET rating_done=1 WHERE id=?`, c.ID)
			return err
		}
		before := make(map[int64]int, len(rank))
		for uid := range rank {
			var r int
			if err := tx.QueryRowContext(ctx, `SELECT IFNULL(rating, ?) FROM users WHERE id=?`, DefaultRating, uid).
				Scan(&r); err != nil {
				return err
			}
			before[uid] = r
		}
		now := nowMS()
		for uid, myRank := range rank {
			sum := 0.0
			for other, theirRank := range rank {
				if other == uid {
					continue
				}
				expected := 1.0 / (1.0 + math.Pow(10, float64(before[other]-before[uid])/400.0))
				actual := 0.5
				if myRank < theirRank {
					actual = 1
				} else if myRank > theirRank {
					actual = 0
				}
				sum += actual - expected
			}
			delta := int(math.Round(ratingK * sum / float64(len(rank)-1)))
			after := before[uid] + delta
			if after < minRating {
				after = minRating
			}
			if after > maxRating {
				after = maxRating
			}
			delta = after - before[uid]
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO contest_ratings (contest_id, user_id, before_rating, after_rating, delta, created_at)
				 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(contest_id, user_id) DO NOTHING`,
				c.ID, uid, before[uid], after, delta, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET rating=? WHERE id=?`, after, uid); err != nil {
				return err
			}
			applied++
		}
		_, err := tx.ExecContext(ctx, `UPDATE contests SET rating_done=1 WHERE id=?`, c.ID)
		return err
	})
	return applied, err
}

func (db *DB) ApplyPendingRatings(ctx context.Context, limit int) (int, error) {
	contests, err := db.pendingRatedContests(ctx, limit)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, c := range contests {
		n, err := db.ApplyContestRatings(ctx, c)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// RunRatingWorker settles rated contests shortly after their end time.
func (db *DB) RunRatingWorker(ctx context.Context, every time.Duration, log *slog.Logger) {
	if every <= 0 {
		every = time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := db.ApplyPendingRatings(ctx, 20)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("apply contest ratings failed", "err", err)
				}
				continue
			}
			if n > 0 {
				log.Info("contest ratings applied", "accounts", n)
			}
		}
	}
}

func (db *DB) ContestRatingFor(ctx context.Context, contestID, userID int64) (ContestRating, bool, error) {
	var r ContestRating
	err := db.SQL.QueryRowContext(ctx,
		`SELECT contest_id, user_id, before_rating, after_rating, delta, created_at
		 FROM contest_ratings WHERE contest_id=? AND user_id=?`, contestID, userID).
		Scan(&r.ContestID, &r.UserID, &r.Before, &r.After, &r.Delta, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	return r, err == nil, err
}

// ListWeeklyContests collects the contests marked 周赛; it does not invent a
// schedule. A host still creates each week's round.
func (db *DB) ListWeeklyContests(ctx context.Context, after int64, limit int, staff bool) (ContestPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := `SELECT ` + listCols + ` FROM contests WHERE IFNULL(weekly, 0)=1`
	args := []any{}
	if after > 0 {
		q += ` AND id<?`
		args = append(args, after)
	}
	if !staff {
		q += ` AND visible=1`
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return ContestPage{}, err
	}
	defer rows.Close()
	var page ContestPage
	for rows.Next() {
		var (
			c                                      Contest
			standing, visible, rated, done, weekly int
		)
		err := rows.Scan(&c.ID, &c.CID, &c.Title, &c.Statement, &c.StartAt, &c.EndAt, &c.Mode,
			&standing, &visible, &c.TeamSize, &c.PenaltyMS, &c.OwnerID, &c.CreatedAt, &c.UpdatedAt,
			&rated, &done, &weekly)
		if err != nil {
			return page, err
		}
		c.StandingOpen, c.Visible = standing != 0, visible != 0
		c.Rated, c.RatingDone, c.Weekly = rated != 0, done != 0, weekly != 0
		page.Items = append(page.Items, ContestSummary{Contest: c})
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	page.Total = len(page.Items)
	return page, nil
}
