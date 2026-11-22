package store

import "context"

// 查重读的是已 AC 提交的源码：这是管理员本来就有的阅读面，
// 不需要把源码复制到另一张表里。
type SourceSubmission struct {
	ID        int64
	UserID    int64
	Handle    string
	Name      string
	Language  string
	Source    string
	CreatedAt int64
}

func (db *DB) ListAcceptedSources(ctx context.Context, problemID int64, limit int) ([]SourceSubmission, error) {
	if limit <= 0 || limit > 500 {
		limit = 300
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT s.id, s.user_id, u.handle, IFNULL(u.display_name, ''), s.language, s.source, s.created_at
		 FROM submissions s JOIN users u ON u.id=s.user_id
		 WHERE s.problem_id=? AND s.status='done' AND s.verdict='AC'
		 ORDER BY s.id DESC LIMIT ?`, problemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceSubmission
	for rows.Next() {
		var s SourceSubmission
		if err := rows.Scan(&s.ID, &s.UserID, &s.Handle, &s.Name, &s.Language, &s.Source, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
