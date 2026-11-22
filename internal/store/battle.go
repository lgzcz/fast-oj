package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 对抗赛围绕一道题，窗口内按最好得分排名；它不写 rating，也不进正式比赛榜。
const (
	MaxBattleTitle = 100
	MaxBattlePeople = 100
)

type Battle struct {
	ID          int64
	Title       string
	ProblemID   int64
	PID         string
	ProblemTitle string
	OwnerID     int64
	OwnerHandle string
	OwnerName   string
	StartAt     int64
	EndAt       int64
	Visible     bool
	CreatedAt   int64
	Joined      bool
	Participants int
}

const battleColumns = `b.id, b.title, b.problem_id, p.pid, p.title, IFNULL(b.owner_id, 0),
	IFNULL(u.handle, ''), IFNULL(u.display_name, ''), b.start_at, b.end_at, b.visible, b.created_at,
	EXISTS(SELECT 1 FROM battle_participants bp WHERE bp.battle_id=b.id AND bp.user_id=?),
	(SELECT COUNT(*) FROM battle_participants bp WHERE bp.battle_id=b.id)`

func scanBattle(row scanner) (Battle, error) {
	var b Battle
	var visible, joined int
	err := row.Scan(&b.ID, &b.Title, &b.ProblemID, &b.PID, &b.ProblemTitle, &b.OwnerID,
		&b.OwnerHandle, &b.OwnerName, &b.StartAt, &b.EndAt, &visible, &b.CreatedAt,
		&joined, &b.Participants)
	b.Visible, b.Joined = visible != 0, joined != 0
	return b, notFound(err)
}

func (db *DB) CreateBattle(ctx context.Context, ownerID, problemID int64, title string, start, end int64, visible bool) (Battle, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > MaxBattleTitle {
		return Battle{}, fmt.Errorf("标题要 1..%d 个字", MaxBattleTitle)
	}
	if end <= start {
		return Battle{}, fmt.Errorf("结束时间必须晚于开始时间")
	}
	var one int
	if err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM problems WHERE id=?`, problemID).Scan(&one); err != nil {
		return Battle{}, notFound(err)
	}
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO battles (title, problem_id, owner_id, start_at, end_at, visible, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`, title, problemID, ownerID, start, end, boolInt(visible), nowMS())
	if err != nil {
		return Battle{}, err
	}
	id, _ := res.LastInsertId()
	return db.GetBattle(ctx, id, ownerID)
}

func (db *DB) GetBattle(ctx context.Context, id, viewerID int64) (Battle, error) {
	return scanBattle(db.SQL.QueryRowContext(ctx,
		`SELECT `+battleColumns+` FROM battles b JOIN problems p ON p.id=b.problem_id
		 LEFT JOIN users u ON u.id=b.owner_id WHERE b.id=?`, nullInt64(viewerID), id))
}

type BattleFilter struct {
	ViewerID int64
	All      bool
	After    int64
	Limit    int
}

func (db *DB) ListBattles(ctx context.Context, f BattleFilter) ([]Battle, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	q := `SELECT ` + battleColumns + ` FROM battles b JOIN problems p ON p.id=b.problem_id
		LEFT JOIN users u ON u.id=b.owner_id WHERE 1=1`
	args := []any{nullInt64(f.ViewerID)}
	if !f.All {
		q += ` AND (b.visible=1 OR b.owner_id=?)`
		args = append(args, nullInt64(f.ViewerID))
	}
	if f.After > 0 {
		q += ` AND b.id<?`
		args = append(args, f.After)
	}
	q += ` ORDER BY b.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Battle
	for rows.Next() {
		b, err := scanBattle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (db *DB) DeleteBattle(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM battles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) IsBattleParticipant(ctx context.Context, battleID, userID int64) (bool, error) {
	var one int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT 1 FROM battle_participants WHERE battle_id=? AND user_id=?`, battleID, userID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (db *DB) JoinBattle(ctx context.Context, battleID, userID int64, now int64) (bool, error) {
	b, err := db.GetBattle(ctx, battleID, userID)
	if err != nil {
		return false, err
	}
	if b.Joined {
		return false, nil
	}
	if now >= b.EndAt {
		return false, fmt.Errorf("这场对抗已经结束")
	}
	if b.Participants >= MaxBattlePeople {
		return false, fmt.Errorf("对抗赛最多 %d 人", MaxBattlePeople)
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO battle_participants (battle_id, user_id, joined_at) VALUES (?, ?, ?)
		 ON CONFLICT(battle_id, user_id) DO NOTHING`, battleID, userID, now)
	return err == nil, err
}

func (db *DB) LeaveBattle(ctx context.Context, battleID, userID, now int64) (bool, error) {
	b, err := db.GetBattle(ctx, battleID, userID)
	if err != nil {
		return false, err
	}
	if now >= b.EndAt {
		return false, fmt.Errorf("已经结束了，成绩保留")
	}
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM battle_participants WHERE battle_id=? AND user_id=?`, battleID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

type BattleRow struct {
	UserID      int64
	Handle      string
	Name        string
	Color       string
	Attempts    int
	Best        int
	Solved      bool
	// Rank is assigned by the caller after sorting: best score desc, attempts asc.
	Rank int
}

// BattleBoard reads the participant list plus their judged practice submissions
// inside the battle window. The problem itself is known, so there is one aggregate
// subquery instead of one query per person.
func (db *DB) BattleBoard(ctx context.Context, b Battle) ([]BattleRow, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT u.id, u.handle, IFNULL(u.display_name, ''), IFNULL(u.color, ''),
		        IFNULL(agg.attempts, 0), IFNULL(agg.best, 0), IFNULL(agg.solved, 0)
		 FROM battle_participants bp
		 JOIN users u ON u.id=bp.user_id
		 LEFT JOIN (
		   SELECT user_id, COUNT(*) AS attempts, MAX(IFNULL(score, 0)) AS best,
		          MAX(CASE WHEN verdict='AC' THEN 1 ELSE 0 END) AS solved
		   FROM submissions
		   WHERE problem_id=? AND IFNULL(contest_id, 0)=0 AND status='done'
		     AND created_at>=? AND created_at<=?
		   GROUP BY user_id
		 ) agg ON agg.user_id=u.id
		 WHERE bp.battle_id=?`, b.ProblemID, b.StartAt, b.EndAt, b.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BattleRow
	for rows.Next() {
		var row BattleRow
		var solved int
		if err := rows.Scan(&row.UserID, &row.Handle, &row.Name, &row.Color,
			&row.Attempts, &row.Best, &solved); err != nil {
			return nil, err
		}
		row.Solved = solved != 0
		out = append(out, row)
	}
	return out, rows.Err()
}
