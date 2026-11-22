package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 客观题是单选自测：一行一个问题，选项用 | 分隔，最后一个字段是答案字母。
// 评分在服务端做；没有判机、没有比赛归属、没有部分分。每题 1 分。
const (
	MaxObjectiveTitle     = 200
	MaxObjectiveBody      = 32768
	MaxObjectiveQuestions = 100
	MaxObjectivePrompt    = 500
	MaxObjectiveOption    = 200
	MaxObjectiveOptions   = 8
	MaxObjectiveSets      = 200
)

type ObjectiveSet struct {
	ID          int64
	Title       string
	Body        string
	OwnerID     int64
	OwnerHandle string
	OwnerName   string
	Visible     bool
	Questions   int
	CreatedAt   int64
}

type ObjectiveQuestion struct {
	Ordinal int
	Prompt  string
	Options []string
	Answer  int
}

type ObjectiveAttempt struct {
	Answers  []int
	Score    int
	Total    int
	Attempts int
	Updated  int64
}

// ParseObjectiveQuestions reads the create/edit textarea. Every non-empty line is
// prompt|option|option|...|answer, where answer is A..H matching the option order.
func ParseObjectiveQuestions(raw string) ([]ObjectiveQuestion, error) {
	var out []ObjectiveQuestion
	lines := strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 4 {
			return nil, fmt.Errorf("第 %d 行至少要「题目|选项|选项|答案」", i+1)
		}
		prompt := strings.TrimSpace(fields[0])
		if prompt == "" || utf8.RuneCountInString(prompt) > MaxObjectivePrompt {
			return nil, fmt.Errorf("第 %d 行题目要 1..%d 个字", i+1, MaxObjectivePrompt)
		}
		answerRaw := strings.ToUpper(strings.TrimSpace(fields[len(fields)-1]))
		if len(answerRaw) != 1 || answerRaw[0] < 'A' || answerRaw[0] > 'H' {
			return nil, fmt.Errorf("第 %d 行答案要是 A..H 的一个字母", i+1)
		}
		answer := int(answerRaw[0] - 'A')
		options := make([]string, 0, len(fields)-2)
		for _, opt := range fields[1 : len(fields)-1] {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				return nil, fmt.Errorf("第 %d 行有空选项", i+1)
			}
			if utf8.RuneCountInString(opt) > MaxObjectiveOption {
				return nil, fmt.Errorf("第 %d 行选项最长 %d 个字", i+1, MaxObjectiveOption)
			}
			options = append(options, opt)
		}
		if len(options) < 2 || len(options) > MaxObjectiveOptions {
			return nil, fmt.Errorf("第 %d 行选项要 2..%d 个", i+1, MaxObjectiveOptions)
		}
		if answer >= len(options) {
			return nil, fmt.Errorf("第 %d 行答案 %s 超出了选项范围", i+1, answerRaw)
		}
		out = append(out, ObjectiveQuestion{Ordinal: len(out) + 1, Prompt: prompt, Options: options, Answer: answer})
		if len(out) > MaxObjectiveQuestions {
			return nil, fmt.Errorf("最多 %d 道题", MaxObjectiveQuestions)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("至少要有一道题")
	}
	return out, nil
}

// FormatObjectiveQuestions is the inverse of ParseObjectiveQuestions, for the edit
// form: the author sees exactly the syntax they typed.
func FormatObjectiveQuestions(items []ObjectiveQuestion) string {
	var b strings.Builder
	for _, q := range items {
		b.WriteString(q.Prompt)
		for _, opt := range q.Options {
			b.WriteString("|")
			b.WriteString(opt)
		}
		b.WriteString("|")
		b.WriteByte(byte('A' + q.Answer))
		b.WriteString("\n")
	}
	return b.String()
}

func validateObjectiveSet(title, body string, questions []ObjectiveQuestion) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > MaxObjectiveTitle {
		return "", fmt.Errorf("标题要 1..%d 个字", MaxObjectiveTitle)
	}
	if utf8.RuneCountInString(body) > MaxObjectiveBody {
		return "", fmt.Errorf("说明最长 %d 个字", MaxObjectiveBody)
	}
	if len(questions) == 0 || len(questions) > MaxObjectiveQuestions {
		return "", fmt.Errorf("题目数量要在 1..%d 之间", MaxObjectiveQuestions)
	}
	return title, nil
}

func insertObjectiveQuestions(ctx context.Context, tx *sql.Tx, setID int64, questions []ObjectiveQuestion, now int64) error {
	for _, q := range questions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO objective_questions (set_id, ordinal, prompt, options, answer_index, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			setID, q.Ordinal, q.Prompt, strings.Join(q.Options, "\n"), q.Answer, now); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) CreateObjectiveSet(ctx context.Context, ownerID int64, title, body string, visible bool, questions []ObjectiveQuestion) (ObjectiveSet, error) {
	title, err := validateObjectiveSet(title, body, questions)
	if err != nil {
		return ObjectiveSet{}, err
	}
	var n int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM objective_sets WHERE owner_id=?`, ownerID).Scan(&n); err != nil {
		return ObjectiveSet{}, err
	}
	if n >= MaxObjectiveSets {
		return ObjectiveSet{}, fmt.Errorf("每人最多 %d 套客观题", MaxObjectiveSets)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return ObjectiveSet{}, err
	}
	defer tx.Rollback()
	now := nowMS()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO objective_sets (title, body, owner_id, visible, created_at) VALUES (?, ?, ?, ?, ?)`,
		title, body, ownerID, boolInt(visible), now)
	if err != nil {
		return ObjectiveSet{}, err
	}
	id, _ := res.LastInsertId()
	if err := insertObjectiveQuestions(ctx, tx, id, questions, now); err != nil {
		return ObjectiveSet{}, err
	}
	if err := tx.Commit(); err != nil {
		return ObjectiveSet{}, err
	}
	return db.GetObjectiveSet(ctx, id)
}

func (db *DB) UpdateObjectiveSet(ctx context.Context, id int64, title, body string, visible bool, questions []ObjectiveQuestion) error {
	title, err := validateObjectiveSet(title, body, questions)
	if err != nil {
		return err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE objective_sets SET title=?, body=?, visible=? WHERE id=?`,
		title, body, boolInt(visible), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM objective_questions WHERE set_id=?`, id); err != nil {
		return err
	}
	if err := insertObjectiveQuestions(ctx, tx, id, questions, nowMS()); err != nil {
		return err
	}
	// 题目整组换了，旧作答按新题重算：保留"做过"这件事，但不让旧得分对应新题。
	rows, err := tx.QueryContext(ctx, `SELECT user_id, answers FROM objective_attempts WHERE set_id=?`, id)
	if err != nil {
		return err
	}
	type retally struct {
		userID       int64
		score, total int
	}
	var pending []retally
	for rows.Next() {
		var userID int64
		var raw string
		if err := rows.Scan(&userID, &raw); err != nil {
			rows.Close()
			return err
		}
		answers := parseObjectiveAnswers(raw)
		score, total := ScoreObjective(questions, answers)
		pending = append(pending, retally{userID: userID, score: score, total: total})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, r := range pending {
		if _, err := tx.ExecContext(ctx,
			`UPDATE objective_attempts SET score=?, total=? WHERE set_id=? AND user_id=?`,
			r.score, r.total, id, r.userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) DeleteObjectiveSet(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM objective_sets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const objectiveSetColumns = `s.id, s.title, s.body, s.owner_id, IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
	(s.visible=1), (SELECT COUNT(*) FROM objective_questions q WHERE q.set_id=s.id), s.created_at`

func scanObjectiveSet(row scanner) (ObjectiveSet, error) {
	var o ObjectiveSet
	err := row.Scan(&o.ID, &o.Title, &o.Body, &o.OwnerID, &o.OwnerHandle, &o.OwnerName,
		&o.Visible, &o.Questions, &o.CreatedAt)
	return o, notFound(err)
}

func (db *DB) GetObjectiveSet(ctx context.Context, id int64) (ObjectiveSet, error) {
	return scanObjectiveSet(db.SQL.QueryRowContext(ctx,
		`SELECT `+objectiveSetColumns+` FROM objective_sets s LEFT JOIN users u ON u.id=s.owner_id WHERE s.id=?`, id))
}

func (db *DB) ListObjectiveSets(ctx context.Context, viewerID int64, seeHidden bool, limit int) ([]ObjectiveSet, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT ` + objectiveSetColumns + ` FROM objective_sets s LEFT JOIN users u ON u.id=s.owner_id WHERE 1=1`
	args := []any{}
	if !seeHidden {
		q += ` AND (s.visible=1 OR s.owner_id=?)`
		args = append(args, viewerID)
	}
	q += ` ORDER BY s.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObjectiveSet
	for rows.Next() {
		o, err := scanObjectiveSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (db *DB) ObjectiveQuestions(ctx context.Context, setID int64) ([]ObjectiveQuestion, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT ordinal, prompt, options, answer_index FROM objective_questions WHERE set_id=? ORDER BY ordinal`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObjectiveQuestion
	for rows.Next() {
		var q ObjectiveQuestion
		var raw string
		if err := rows.Scan(&q.Ordinal, &q.Prompt, &raw, &q.Answer); err != nil {
			return nil, err
		}
		q.Options = strings.Split(raw, "\n")
		out = append(out, q)
	}
	return out, rows.Err()
}

func (db *DB) ObjectiveAttemptFor(ctx context.Context, setID, userID int64) (ObjectiveAttempt, bool, error) {
	var a ObjectiveAttempt
	var raw string
	err := db.SQL.QueryRowContext(ctx,
		`SELECT answers, score, total, attempts, updated_at FROM objective_attempts WHERE set_id=? AND user_id=?`,
		setID, userID).Scan(&raw, &a.Score, &a.Total, &a.Attempts, &a.Updated)
	if err != nil {
		if err == sql.ErrNoRows {
			return a, false, nil
		}
		return a, false, err
	}
	a.Answers = parseObjectiveAnswers(raw)
	return a, true, nil
}

func parseObjectiveAnswers(raw string) []int {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = -1
		}
		out = append(out, n)
	}
	return out
}

func (db *DB) SaveObjectiveAttempt(ctx context.Context, setID, userID int64, answers []int, score, total int) error {
	parts := make([]string, len(answers))
	for i, n := range answers {
		parts[i] = strconv.Itoa(n)
	}
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO objective_attempts (set_id, user_id, answers, score, total, attempts, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?)
		 ON CONFLICT(set_id, user_id) DO UPDATE SET
		   answers=excluded.answers, score=excluded.score, total=excluded.total,
		   attempts=objective_attempts.attempts+1, updated_at=excluded.updated_at`,
		setID, userID, strings.Join(parts, ","), score, total, nowMS())
	return err
}

// ScoreObjective compares the chosen indexes with the stored answers. Unanswered
// (-1) is wrong; score is one point per question.
func ScoreObjective(questions []ObjectiveQuestion, answers []int) (score, total int) {
	for i, q := range questions {
		total++
		if i < len(answers) && answers[i] == q.Answer {
			score++
		}
	}
	return score, total
}
