package store

import (
	"context"
	"fmt"
)

// 题单只负责"有哪些题、按什么顺序"。做题进度不落库：AC 与否本来就写在
// submissions 里，再存一份只会两边不一致。
type Training struct {
	ID          int64
	Title       string
	Intro       string
	Visible     bool
	OwnerID     int64
	OwnerHandle string
	OwnerName   string
	Problems    int
	CreatedAt   int64
	UpdatedAt   int64
}

type TrainingProblem struct {
	Problem
	Position int
	Solved   bool
}

const (
	MaxTrainingTitle    = 100
	MaxTrainingIntro    = 20000
	MaxTrainingProblems = 500
	// 每个人的题单数量上限：这不是权限，是防止脚本一夜之间把列表刷满。
	MaxTrainingsPerUser = 100
)

func (db *DB) CreateTraining(ctx context.Context, ownerID int64, title, intro string, visible bool) (Training, error) {
	now := nowMS()
	var count int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM trainings WHERE owner_id=?`, ownerID).Scan(&count); err != nil {
		return Training{}, err
	}
	if count >= MaxTrainingsPerUser {
		return Training{}, fmt.Errorf("每个人最多建 %d 个题单", MaxTrainingsPerUser)
	}
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO trainings (title, intro, visible, owner_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`, title, intro, boolInt(visible), ownerID, now, now)
	if err != nil {
		return Training{}, err
	}
	id, _ := res.LastInsertId()
	return db.GetTraining(ctx, id)
}

const trainingColumns = `t.id, t.title, t.intro, t.visible, IFNULL(t.owner_id, 0),
	IFNULL(u.handle, ''), IFNULL(u.display_name, ''), t.created_at, t.updated_at,
	(SELECT COUNT(*) FROM training_problems tp WHERE tp.training_id=t.id)`

func scanTraining(row scanner) (Training, error) {
	var t Training
	var visible int
	err := row.Scan(&t.ID, &t.Title, &t.Intro, &visible, &t.OwnerID,
		&t.OwnerHandle, &t.OwnerName, &t.CreatedAt, &t.UpdatedAt, &t.Problems)
	t.Visible = visible != 0
	return t, notFound(err)
}

func (db *DB) GetTraining(ctx context.Context, id int64) (Training, error) {
	return scanTraining(db.SQL.QueryRowContext(ctx,
		`SELECT `+trainingColumns+` FROM trainings t LEFT JOIN users u ON u.id=t.owner_id WHERE t.id=?`, id))
}

type TrainingFilter struct {
	// ViewerID 用来把"我自己建的隐藏题单"放回列表；0 表示匿名。
	ViewerID int64
	// All 是持有 problem.edit.any 的人的视角：隐藏题单也一并列出。
	All    bool
	Search string
	After  int64
	Limit  int
}

func (db *DB) ListTrainings(ctx context.Context, f TrainingFilter) ([]Training, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	q := `SELECT ` + trainingColumns + ` FROM trainings t LEFT JOIN users u ON u.id=t.owner_id WHERE 1=1`
	args := []any{}
	if !f.All {
		q += ` AND (t.visible=1 OR t.owner_id=?)`
		args = append(args, nullInt64(f.ViewerID))
	}
	if f.Search != "" {
		like := "%" + escapeLike(f.Search) + "%"
		q += ` AND (t.title LIKE ? ESCAPE '\' OR t.intro LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	if f.After > 0 {
		q += ` AND t.id < ?`
		args = append(args, f.After)
	}
	q += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Training
	for rows.Next() {
		t, err := scanTraining(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) UpdateTraining(ctx context.Context, id int64, title, intro string, visible bool) error {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE trainings SET title=?, intro=?, visible=?, updated_at=? WHERE id=?`,
		title, intro, boolInt(visible), nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteTraining(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM trainings WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TrainingProblems 按 position 返回题单里的题。隐藏题只对能编辑它的人露出：
// 一份公开题单里混进未公开题，不等于把题面一起公开。
func (db *DB) TrainingProblems(ctx context.Context, trainingID, viewerID int64, includeHidden bool) ([]TrainingProblem, error) {
	q := `SELECT p.id, p.pid, p.title, p.statement, p.time_limit_ms, p.mem_limit_kb, p.languages,
		p.visible, IFNULL(p.owner_id, 0), p.created_at, p.updated_at, IFNULL(p.detail_level, 'diff'),
		p.difficulty, IFNULL(p.source, ''), IFNULL(p.tags, ''), IFNULL(p.io_input, ''), IFNULL(p.io_output, ''),
		tp.position,
		EXISTS(SELECT 1 FROM submissions s WHERE s.user_id=? AND s.problem_id=p.id AND s.verdict='AC')
		FROM training_problems tp JOIN problems p ON p.id=tp.problem_id
		WHERE tp.training_id=?`
	args := []any{viewerID, trainingID}
	if !includeHidden {
		q += ` AND p.visible=1`
	}
	q += ` ORDER BY tp.position, p.id`
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrainingProblem
	for rows.Next() {
		var tp TrainingProblem
		var langs, tags string
		var visible, solved int
		if err := rows.Scan(&tp.ID, &tp.PID, &tp.Title, &tp.Statement, &tp.TimeLimitMS, &tp.MemLimitKB,
			&langs, &visible, &tp.OwnerID, &tp.CreatedAt, &tp.UpdatedAt, &tp.DetailLevel,
			&tp.Difficulty, &tp.Source, &tags, &tp.InputFile, &tp.OutputFile,
			&tp.Position, &solved); err != nil {
			return nil, err
		}
		tp.Languages = splitList(langs)
		tp.Tags = splitList(tags)
		tp.Visible = visible != 0
		tp.Solved = solved != 0
		out = append(out, tp)
	}
	return out, rows.Err()
}

func (db *DB) CountTrainingProblems(ctx context.Context, trainingID int64) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM training_problems WHERE training_id=?`, trainingID).Scan(&n)
	return n, err
}

// AddTrainingProblem appends one problem. A repeat is a no-op rather than an
// error: the owner may click twice after a slow response.
func (db *DB) AddTrainingProblem(ctx context.Context, trainingID, problemID int64) error {
	var exists int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT 1 FROM problems WHERE id=?`, problemID).Scan(&exists); err != nil {
		return notFound(err)
	}
	var count int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM training_problems WHERE training_id=?`, trainingID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxTrainingProblems {
		return fmt.Errorf("一个题单最多 %d 道题", MaxTrainingProblems)
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO training_problems (training_id, problem_id, position)
		 SELECT ?, ?, COALESCE(MAX(position), 0) + 1 FROM training_problems WHERE training_id=?
		 ON CONFLICT(training_id, problem_id) DO NOTHING`, trainingID, problemID, trainingID)
	return err
}

func (db *DB) RemoveTrainingProblem(ctx context.Context, trainingID, problemID int64) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM training_problems WHERE training_id=? AND problem_id=?`, trainingID, problemID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
