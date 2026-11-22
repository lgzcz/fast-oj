package store

import (
	"context"
	"database/sql"
)

// 收藏是私人的：题目后来被藏起来，收藏行还在，但打开题面仍按题目的可见性走。
func (db *DB) ToggleFavorite(ctx context.Context, userID, problemID int64) (bool, error) {
	var one int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT 1 FROM problems WHERE id=?`, problemID).Scan(&one); err != nil {
		return false, notFound(err)
	}
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM favorites WHERE user_id=? AND problem_id=?`, userID, problemID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return false, nil
	}
	_, err = db.SQL.ExecContext(ctx,
		`INSERT INTO favorites (user_id, problem_id, created_at) VALUES (?, ?, ?)`,
		userID, problemID, nowMS())
	return true, err
}

func (db *DB) IsFavorite(ctx context.Context, userID, problemID int64) (bool, error) {
	var one int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT 1 FROM favorites WHERE user_id=? AND problem_id=?`, userID, problemID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (db *DB) ListFavoriteProblems(ctx context.Context, userID int64, limit int) ([]Problem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT p.id, p.pid, p.title, p.statement, p.time_limit_ms, p.mem_limit_kb, p.languages,
			p.visible, IFNULL(p.owner_id, 0), p.created_at, p.updated_at, IFNULL(p.detail_level, 'diff'),
			p.difficulty, IFNULL(p.source, ''), IFNULL(p.tags, ''), IFNULL(p.io_input, ''), IFNULL(p.io_output, '')
		 FROM favorites f JOIN problems p ON p.id=f.problem_id
		 WHERE f.user_id=? ORDER BY f.created_at DESC, f.problem_id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Problem
	for rows.Next() {
		p, err := scanProblem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
