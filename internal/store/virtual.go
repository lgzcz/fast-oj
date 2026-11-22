package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// 虚拟比赛是给自己重放一场已经结束的比赛：按原时长计时，只影响自己的练习页，
// 不进正式榜、不参与 rating。提交仍是普通练习提交。
type VirtualContest struct {
	ID          int64
	UserID      int64
	ContestID   int64
	CID         string
	Title       string
	Mode        string
	StartAt     int64
	EndAt       int64
	FinishedAt  int64
	CreatedAt   int64
}

func (v VirtualContest) Running(now int64) bool {
	return v.FinishedAt == 0 && now < v.EndAt
}

func (v VirtualContest) Duration() time.Duration { return time.Duration(v.EndAt-v.StartAt) * time.Millisecond }

func (db *DB) StartVirtualContest(ctx context.Context, userID, contestID int64) (VirtualContest, error) {
	c, err := db.GetContestByID(ctx, contestID)
	if err != nil {
		return VirtualContest{}, err
	}
	if c.Phase(nowMS()) != PhaseOver {
		return VirtualContest{}, fmt.Errorf("比赛还没结束，不能开虚拟赛")
	}
	// 同一场比赛只保留一条正在进行的虚拟赛，重复点开始就回到原来那场。
	var id int64
	err = db.SQL.QueryRowContext(ctx,
		`SELECT id FROM virtual_contests WHERE user_id=? AND contest_id=? AND finished_at=0 ORDER BY id DESC LIMIT 1`,
		userID, contestID).Scan(&id)
	if err == nil {
		return db.GetVirtualContest(ctx, id, userID)
	}
	if err != sql.ErrNoRows {
		return VirtualContest{}, err
	}
	now := nowMS()
	duration := c.EndAt - c.StartAt
	if duration <= 0 {
		return VirtualContest{}, fmt.Errorf("这场比赛的时间不完整")
	}
	// 原时长，但封顶 6 小时：复制一场三天的比赛没有练习意义，还会把页面计时撑坏。
	const capMS = int64(6 * time.Hour / time.Millisecond)
	if duration > capMS {
		duration = capMS
	}
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO virtual_contests (user_id, contest_id, start_at, end_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`, userID, contestID, now, now+duration, now)
	if err != nil {
		return VirtualContest{}, err
	}
	id, _ = res.LastInsertId()
	return db.GetVirtualContest(ctx, id, userID)
}

func (db *DB) GetVirtualContest(ctx context.Context, id, userID int64) (VirtualContest, error) {
	var v VirtualContest
	err := db.SQL.QueryRowContext(ctx,
		`SELECT v.id, v.user_id, v.contest_id, c.cid, c.title, c.mode, v.start_at, v.end_at,
		        v.finished_at, v.created_at
		 FROM virtual_contests v JOIN contests c ON c.id=v.contest_id
		 WHERE v.id=? AND v.user_id=?`, id, userID).
		Scan(&v.ID, &v.UserID, &v.ContestID, &v.CID, &v.Title, &v.Mode,
			&v.StartAt, &v.EndAt, &v.FinishedAt, &v.CreatedAt)
	return v, notFound(err)
}

func (db *DB) CurrentVirtualContest(ctx context.Context, userID, contestID int64) (VirtualContest, bool, error) {
	var id int64
	err := db.SQL.QueryRowContext(ctx,
		`SELECT id FROM virtual_contests WHERE user_id=? AND contest_id=? AND finished_at=0
		 ORDER BY id DESC LIMIT 1`, userID, contestID).Scan(&id)
	if err == sql.ErrNoRows {
		return VirtualContest{}, false, nil
	}
	if err != nil {
		return VirtualContest{}, false, err
	}
	v, err := db.GetVirtualContest(ctx, id, userID)
	return v, err == nil, err
}

func (db *DB) FinishVirtualContest(ctx context.Context, id, userID int64) error {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE virtual_contests SET finished_at=? WHERE id=? AND user_id=? AND finished_at=0`,
		nowMS(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) ListVirtualContests(ctx context.Context, userID int64, limit int) ([]VirtualContest, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT v.id, v.user_id, v.contest_id, c.cid, c.title, c.mode, v.start_at, v.end_at,
		        v.finished_at, v.created_at
		 FROM virtual_contests v JOIN contests c ON c.id=v.contest_id
		 WHERE v.user_id=? ORDER BY v.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VirtualContest
	for rows.Next() {
		var v VirtualContest
		if err := rows.Scan(&v.ID, &v.UserID, &v.ContestID, &v.CID, &v.Title, &v.Mode,
			&v.StartAt, &v.EndAt, &v.FinishedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type VirtualProblemScore struct {
	PID       string
	Title     string
	Points    int
	Max       int
	Solved    bool
	Attempts  int
}

type VirtualScore struct {
	Problems []VirtualProblemScore
	Total    int
	Solved   int
	MaxTotal int
}

// VirtualScore 只看开始之后、截止之前的普通提交：虚拟赛没有自己的提交归属，
// 所以按"这段时间里你在这几道题上做了什么"来算。
func (db *DB) VirtualScore(ctx context.Context, v VirtualContest) (VirtualScore, error) {
	c, err := db.GetContestByID(ctx, v.ContestID)
	if err != nil {
		return VirtualScore{}, err
	}
	mounted, err := db.ListContestProblems(ctx, v.ContestID)
	if err != nil {
		return VirtualScore{}, err
	}
	until := v.EndAt
	if v.FinishedAt != 0 && v.FinishedAt < until {
		until = v.FinishedAt
	}
	var out VirtualScore
	for _, cp := range mounted {
		var score VirtualProblemScore
		score.PID, score.Title = cp.PID, cp.Title
		score.Max = contestMax(cp)
		rows, err := db.SQL.QueryContext(ctx,
			`SELECT verdict, score, created_at FROM submissions
			 WHERE user_id=? AND problem_id=? AND IFNULL(contest_id, 0)=0
			   AND created_at>=? AND created_at<=? AND status='done'
			 ORDER BY id`, v.UserID, cp.ProblemID, v.StartAt, until)
		if err != nil {
			return out, err
		}
		last := 0
		for rows.Next() {
			var verdict string
			var raw int
			var at int64
			if err := rows.Scan(&verdict, &raw, &at); err != nil {
				rows.Close()
				return out, err
			}
			score.Attempts++
			if verdict == "AC" {
				score.Solved = true
			}
			last = ContestAward(raw, cp.Points, cp.TotalScore)
			if verdict == "AC" && last < score.Max {
				// ACM 的通过就是满分；OI/IOI 仍按实际得分。
				if c.Mode == ContestACM {
					last = score.Max
				}
			}
		}
		rows.Close()
		score.Points = last
		if c.Mode != ContestOI && score.Solved && score.Points < score.Max {
			score.Points = score.Max
		}
		out.Total += score.Points
		out.MaxTotal += score.Max
		if score.Solved {
			out.Solved++
		}
		out.Problems = append(out.Problems, score)
	}
	return out, nil
}
