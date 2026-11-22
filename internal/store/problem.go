package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var ErrConflict = errors.New("already exists")

func conflict(err error, what string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %s", ErrConflict, what)
	}
	return err
}

// escapeLike neutralises LIKE wildcards so a search term stays a literal.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// nullInt64 turns a zero id into SQL NULL so the foreign key stays quiet.
func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// TransferProblemOwner moves authorship, which is how a problem survives its
// author leaving. Only someone with the manage right may call it.
func (db *DB) TransferProblemOwner(ctx context.Context, problemID, ownerID int64) error {
	res, err := db.SQL.ExecContext(ctx, `UPDATE problems SET owner_id=? WHERE id=?`, nullInt64(ownerID), problemID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const problemColumns = `id, pid, title, statement, time_limit_ms, mem_limit_kb, languages,
		visible, IFNULL(owner_id, 0), created_at, updated_at, IFNULL(detail_level, 'diff'),
			difficulty, IFNULL(source, ''), IFNULL(tags, '')`

func scanProblem(row scanner) (Problem, error) {
	var (
		p           Problem
		langs       string
		tags        string
		visibleFlag int
	)
	err := row.Scan(&p.ID, &p.PID, &p.Title, &p.Statement, &p.TimeLimitMS, &p.MemLimitKB,
		&langs, &visibleFlag, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt, &p.DetailLevel,
		&p.Difficulty, &p.Source, &tags)
	p.Languages = splitList(langs)
	p.Tags = splitList(tags)
	p.Visible = visibleFlag != 0
	return p, notFound(err)
}

// problemDifficulties 是题库页显示的六档（0=未标）。名字是 OI 圈常见的说法，
// 不在这里发明新分级。
var problemDifficulties = []string{"未标", "入门", "普及", "提高", "省选", "NOI"}

func DifficultyLabel(n int) string {
	if n < 0 || n >= len(problemDifficulties) {
		return problemDifficulties[0]
	}
	return problemDifficulties[n]
}

func DifficultyOptions() []string { return append([]string(nil), problemDifficulties...) }

const (
	MaxProblemTags   = 8
	MaxProblemTagLen = 20
)

// NormalizeProblemTags 把表单原文变成库里的逗号串：去重、trim、中文逗号归一化。
// 不合格就报错而不是静默丢——出题人以为贴上了标签，题面上却没有，比拒绝更糟。
func NormalizeProblemTags(raw string) (string, error) {
	parts := SplitTags(raw)
	if len(parts) > MaxProblemTags {
		return "", fmt.Errorf("标签最多 %d 个", MaxProblemTags)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if utf8.RuneCountInString(p) > MaxProblemTagLen || len(p) > 128 {
			return "", fmt.Errorf("单个标签最长 %d 个字", MaxProblemTagLen)
		}
		if strings.ContainsFunc(p, func(r rune) bool {
			return r < 0x20 || r == 0x7f || r == '[' || r == ']' || r == ',' || r == '，'
		}) {
			return "", errors.New("标签里不能有方括号、逗号或控制字符")
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return strings.Join(out, ","), nil
}

func (db *DB) CreateProblem(ctx context.Context, p *Problem) (Problem, error) {
	now := nowMS()
	p.CreatedAt = now
	p.UpdatedAt = now
	p.DetailLevel = detailColumn(p.DetailLevel)
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO problems (pid, title, statement, time_limit_ms, mem_limit_kb, languages, visible, owner_id, created_at, updated_at, detail_level, difficulty, source, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.PID, p.Title, p.Statement, p.TimeLimitMS, p.MemLimitKB,
		joinList(p.Languages), boolInt(p.Visible), nullInt64(p.OwnerID), p.CreatedAt, p.UpdatedAt,
		detailColumn(p.DetailLevel), p.Difficulty, p.Source, joinList(p.Tags))
	if err != nil {
		return Problem{}, conflict(err, "pid "+p.PID)
	}
	p.ID, _ = res.LastInsertId()
	return *p, nil
}

func (db *DB) UpdateProblem(ctx context.Context, p Problem) (Problem, error) {
	p.UpdatedAt = nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE problems SET pid=?, title=?, statement=?, time_limit_ms=?, mem_limit_kb=?, languages=?, visible=?, updated_at=?, detail_level=?,
		   difficulty=?, source=?, tags=?
		 WHERE id=?`,
		p.PID, p.Title, p.Statement, p.TimeLimitMS, p.MemLimitKB,
		joinList(p.Languages), boolInt(p.Visible), p.UpdatedAt, detailColumn(p.DetailLevel),
		p.Difficulty, p.Source, joinList(p.Tags), p.ID)
	if err != nil {
		return Problem{}, conflict(err, "pid "+p.PID)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Problem{}, ErrNotFound
	}
	return db.GetProblemByID(ctx, p.ID)
}

func (db *DB) GetProblemByID(ctx context.Context, id int64) (Problem, error) {
	return scanProblem(db.SQL.QueryRowContext(ctx,
		`SELECT `+problemColumns+` FROM problems WHERE id=?`, id))
}

// FindProblem accepts either the numeric row id or the human readable pid.
func (db *DB) FindProblem(ctx context.Context, key string) (Problem, error) {
	var id int64
	if _, err := fmt.Sscanf(key, "%d", &id); err == nil && strings.TrimLeft(key, "0123456789") == "" {
		return db.GetProblemByID(ctx, id)
	}
	return scanProblem(db.SQL.QueryRowContext(ctx,
		`SELECT `+problemColumns+` FROM problems WHERE pid=? COLLATE NOCASE`, key))
}

// ProblemSort 是题库列表的三条顺序：编号升序（默认）、编号降序、最新在前。
// 时间序用自增 id 当游标：problems 只在这套代码里创建，id 的顺序就是创建顺序。
type ProblemSort int

const (
	ProblemSortPID ProblemSort = iota
	ProblemSortPIDDesc
	ProblemSortTimeDesc
)

// ProblemCursor 是翻页锚点。编号序用 PID，时间序用 ID，两者只会有一个生效。
type ProblemCursor struct {
	ID  int64
	PID string
}

type ProblemPage struct {
	Items     []Problem `json:"items"`
	NextAfter int64     `json:"next_after,omitempty"`
	NextPID   string    `json:"next_pid,omitempty"`
	Total     int       `json:"total"`
}

// ProblemFilter narrows the bank by the three browsable fields. Zero values mean
// "don't narrow", so the ordinary list keeps its old shape.
type ProblemFilter struct {
	Difficulty int
	Tag        string
	Source     string
}

// ListProblems pages over the sort key with a cursor instead of OFFSET, so deep
// pages keep the same cost as the first one.
func (db *DB) ListProblems(ctx context.Context, cursor ProblemCursor, limit int, search string, visibleOnly bool, sort ProblemSort) (ProblemPage, error) {
	return db.ListProblemsFiltered(ctx, cursor, limit, search, visibleOnly, sort, ProblemFilter{})
}

func (db *DB) ListProblemsFiltered(ctx context.Context, cursor ProblemCursor, limit int, search string, visibleOnly bool, sort ProblemSort, filter ProblemFilter) (ProblemPage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	q := `SELECT ` + problemColumns + ` FROM problems WHERE 1=1`
	args := []any{}
	switch sort {
	case ProblemSortPIDDesc:
		if cursor.PID != "" {
			q += ` AND pid < ?`
			args = append(args, cursor.PID)
		}
	case ProblemSortTimeDesc:
		if cursor.ID > 0 {
			q += ` AND id < ?`
			args = append(args, cursor.ID)
		}
	default:
		if cursor.PID != "" {
			q += ` AND pid > ?`
			args = append(args, cursor.PID)
		}
	}
	if visibleOnly {
		q += ` AND visible=1`
	}
	if search != "" {
		like := "%" + escapeLike(search) + "%"
		q += ` AND (pid LIKE ? ESCAPE '\' OR title LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\' OR source LIKE ? ESCAPE '\')`
		args = append(args, like, like, like, like)
	}
	if filter.Difficulty > 0 {
		q += ` AND difficulty=?`
		args = append(args, filter.Difficulty)
	}
	if filter.Tag != "" {
		// tags 是逗号串；前后补逗号再 instr，避免 "dp" 命中 "graphdp"。
		q += ` AND instr(','||tags||',', ','||?||',') > 0`
		args = append(args, filter.Tag)
	}
	if filter.Source != "" {
		q += ` AND source LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(filter.Source)+"%")
	}
	switch sort {
	case ProblemSortPIDDesc:
		q += ` ORDER BY pid DESC`
	case ProblemSortTimeDesc:
		q += ` ORDER BY id DESC`
	default:
		q += ` ORDER BY pid ASC`
	}
	q += ` LIMIT ?`
	args = append(args, limit+1)

	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return ProblemPage{}, err
	}
	defer rows.Close()

	var page ProblemPage
	for rows.Next() {
		p, err := scanProblem(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, p)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		last := page.Items[limit-1]
		if sort == ProblemSortTimeDesc {
			page.NextAfter = last.ID
		} else {
			page.NextPID = last.PID
		}
		page.Items = page.Items[:limit]
	}

	where := "WHERE 1=1"
	if visibleOnly {
		where += " AND visible=1"
	}
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM problems `+where).Scan(&page.Total); err != nil {
		return page, err
	}
	return page, nil
}

// CountProblems is the cheap total for page headers, where pulling a page of
// rows just to read .Total would be silly.
func (db *DB) CountProblems(ctx context.Context, visibleOnly bool) (int, error) {
	q := `SELECT COUNT(*) FROM problems`
	if visibleOnly {
		q += ` WHERE visible=1`
	}
	var n int
	err := db.SQL.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}

// 题库页每题旁边的三个状态。
const (
	ProblemUntried = "untried"
	ProblemTried   = "tried"
	ProblemSolved  = "solved"
)

// ProblemStatuses answers "this page of problems, did I try/solve them" in one
// grouped query. Problems with no submission are simply absent, which the caller
// reads as untried.
func (db *DB) ProblemStatuses(ctx context.Context, userID int64, problemIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(problemIDs))
	if userID == 0 || len(problemIDs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(problemIDs)), ",")
	args := make([]any, 0, len(problemIDs)+1)
	args = append(args, userID)
	for _, id := range problemIDs {
		args = append(args, id)
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT problem_id, MAX(CASE WHEN verdict='AC' THEN 1 ELSE 0 END)
		   FROM submissions
		  WHERE user_id=? AND problem_id IN (`+placeholders+`)
		  GROUP BY problem_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id     int64
			solved int
		)
		if err := rows.Scan(&id, &solved); err != nil {
			return nil, err
		}
		if solved != 0 {
			out[id] = ProblemSolved
		} else {
			out[id] = ProblemTried
		}
	}
	return out, rows.Err()
}

// CountSolved counts distinct problems this user has AC'd.
func (db *DB) CountSolved(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT problem_id) FROM submissions WHERE user_id=? AND verdict='AC'`, userID).Scan(&n)
	return n, err
}

func (db *DB) DeleteProblem(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM problems WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Detail levels. They are stored as text so the console can show them without a
// join, and an unknown value falls back to the middle of the range.
const (
	DetailNone    = "none"
	DetailCase    = "case"
	DetailDiff    = "diff"
	DetailFull    = "full"
	DetailMax     = DetailFull
	defaultDetail = DetailDiff
)

// detailColumn keeps a zero value from writing an empty string into a column with
// a CHECK constraint.
func detailColumn(level string) string {
	switch level {
	case DetailNone, DetailCase, DetailDiff, DetailFull:
		return level
	}
	return defaultDetail
}

// ValidDetailLevel is what the authoring form validates against.
func ValidDetailLevel(level string) bool {
	switch level {
	case DetailNone, DetailCase, DetailDiff, DetailFull:
		return true
	}
	return false
}
