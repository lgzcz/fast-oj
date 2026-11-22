package store

import "context"

// 错题本不另存一张表：它就是"在这道题上提交过、但还没有 AC"的现算结果。
// 一旦 AC 就从本子里消失；重判把 AC 改掉又会自动回来。
type ReviewProblem struct {
	Problem
	Attempts    int
	LastAt      int64
	LastVerdict string
}

func (db *DB) ListReviewProblems(ctx context.Context, userID int64, limit int) ([]ReviewProblem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT p.id, p.pid, p.title, p.statement, p.time_limit_ms, p.mem_limit_kb, p.languages,
			p.visible, IFNULL(p.owner_id, 0), p.created_at, p.updated_at, IFNULL(p.detail_level, 'diff'),
			p.difficulty, IFNULL(p.source, ''), IFNULL(p.tags, ''), IFNULL(p.io_input, ''), IFNULL(p.io_output, ''),
			COUNT(s.id), MAX(s.created_at),
			(SELECT s2.verdict FROM submissions s2
			  WHERE s2.user_id=? AND s2.problem_id=p.id
			  ORDER BY s2.id DESC LIMIT 1)
		 FROM submissions s JOIN problems p ON p.id=s.problem_id
		 WHERE s.user_id=? AND p.visible=1
		 GROUP BY p.id
		 HAVING SUM(CASE WHEN s.verdict='AC' THEN 1 ELSE 0 END)=0
		 ORDER BY MAX(s.created_at) DESC, p.id DESC LIMIT ?`, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewProblem
	for rows.Next() {
		var r ReviewProblem
		var langs, tags string
		var visible int
		if err := rows.Scan(&r.ID, &r.PID, &r.Title, &r.Statement, &r.TimeLimitMS, &r.MemLimitKB,
			&langs, &visible, &r.OwnerID, &r.CreatedAt, &r.UpdatedAt, &r.DetailLevel,
			&r.Difficulty, &r.Source, &tags, &r.InputFile, &r.OutputFile,
			&r.Attempts, &r.LastAt, &r.LastVerdict); err != nil {
			return nil, err
		}
		r.Languages = splitList(langs)
		r.Tags = splitList(tags)
		r.Visible = visible != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
