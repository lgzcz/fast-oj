package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	ArticleSolution = "solution"
	ArticleColumn   = "column"

	MaxArticleTitle = 200
	MaxArticleBody  = 65536
)

// 题解与专栏是一张表的两面：前者 problem_id 必填，后者为空。
// 正文只有详情页才读，列表不拖整篇文章。
type Article struct {
	ID         int64
	Kind       string
	ProblemID  int64
	ProblemPID string
	ProblemTitle string
	// ProblemVisible 是题目自己的可见性；column 恒为 true。
	ProblemVisible bool
	ProblemOwnerID int64
	AuthorID      int64
	AuthorHandle  string
	AuthorName    string
	AuthorColor   string
	Title         string
	Body          string
	Visible       bool
	CreatedAt     int64
	UpdatedAt     int64
}

func ValidArticleKind(kind string) bool {
	return kind == ArticleSolution || kind == ArticleColumn
}

const articleListColumns = `a.id, a.kind, IFNULL(a.problem_id, 0), IFNULL(p.pid, ''), IFNULL(p.title, ''),
	IFNULL(p.visible, 1), IFNULL(p.owner_id, 0), IFNULL(a.author_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
	IFNULL(u.color, ''), a.title, a.visible, a.created_at, a.updated_at`

func scanArticleList(row scanner) (Article, error) {
	var a Article
	var visible, problemVisible int
	err := row.Scan(&a.ID, &a.Kind, &a.ProblemID, &a.ProblemPID, &a.ProblemTitle,
		&problemVisible, &a.ProblemOwnerID, &a.AuthorID, &a.AuthorHandle, &a.AuthorName, &a.AuthorColor,
		&a.Title, &visible, &a.CreatedAt, &a.UpdatedAt)
	a.Visible, a.ProblemVisible = visible != 0, problemVisible != 0
	return a, notFound(err)
}

func (db *DB) GetArticle(ctx context.Context, id int64) (Article, error) {
	row := db.SQL.QueryRowContext(ctx,
		`SELECT `+articleListColumns+`, a.body
		 FROM articles a LEFT JOIN problems p ON p.id=a.problem_id
		 LEFT JOIN users u ON u.id=a.author_id WHERE a.id=?`, id)
	var a Article
	var visible, problemVisible int
	err := row.Scan(&a.ID, &a.Kind, &a.ProblemID, &a.ProblemPID, &a.ProblemTitle,
		&problemVisible, &a.ProblemOwnerID, &a.AuthorID, &a.AuthorHandle, &a.AuthorName, &a.AuthorColor,
		&a.Title, &visible, &a.CreatedAt, &a.UpdatedAt, &a.Body)
	a.Visible, a.ProblemVisible = visible != 0, problemVisible != 0
	return a, notFound(err)
}

type ArticleFilter struct {
	Kind      string
	ProblemID int64
	// ViewerID 让作者看到自己的草稿；All 是审核者的视角（topic.moderate）。
	ViewerID int64
	All      bool
	Search   string
	After    int64
	Limit    int
}

func (db *DB) ListArticles(ctx context.Context, f ArticleFilter) ([]Article, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	q := `SELECT ` + articleListColumns + `
		FROM articles a LEFT JOIN problems p ON p.id=a.problem_id
		LEFT JOIN users u ON u.id=a.author_id WHERE 1=1`
	args := []any{}
	if f.Kind != "" {
		q += ` AND a.kind=?`
		args = append(args, f.Kind)
	}
	if f.ProblemID != 0 {
		q += ` AND a.problem_id=?`
		args = append(args, f.ProblemID)
	}
	if !f.All {
		q += ` AND (a.visible=1 OR a.author_id=?)`
		args = append(args, nullInt64(f.ViewerID))
	}
	if f.Search != "" {
		like := "%" + escapeLike(f.Search) + "%"
		q += ` AND (a.title LIKE ? ESCAPE '\' OR a.body LIKE ? ESCAPE '\')`
		args = append(args, like, like)
	}
	if f.After > 0 {
		q += ` AND a.id < ?`
		args = append(args, f.After)
	}
	q += ` ORDER BY a.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		a, err := scanArticleList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateArticle 把 kind 与 problem 的配对再验一遍：HTTP 层验过，但存储层不该
// 依赖调用方守规矩。
func (db *DB) CreateArticle(ctx context.Context, kind string, problemID, authorID int64, title, body string, visible bool) (Article, error) {
	if !ValidArticleKind(kind) {
		return Article{}, fmt.Errorf("未知的文章类型 %q", kind)
	}
	if utf8.RuneCountInString(title) == 0 || utf8.RuneCountInString(title) > MaxArticleTitle {
		return Article{}, fmt.Errorf("标题要 1..%d 个字", MaxArticleTitle)
	}
	if utf8.RuneCountInString(body) > MaxArticleBody {
		return Article{}, fmt.Errorf("正文最长 %d 个字", MaxArticleBody)
	}
	if kind == ArticleSolution {
		if problemID == 0 {
			return Article{}, fmt.Errorf("题解要挂在某道题上")
		}
		var one int
		if err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM problems WHERE id=?`, problemID).Scan(&one); err != nil {
			return Article{}, notFound(err)
		}
	} else {
		problemID = 0
	}
	now := nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO articles (kind, problem_id, author_id, title, body, visible, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		kind, nullInt64(problemID), authorID, title, body, boolInt(visible), now, now)
	if err != nil {
		return Article{}, err
	}
	id, _ := res.LastInsertId()
	return db.GetArticle(ctx, id)
}

func (db *DB) UpdateArticle(ctx context.Context, id int64, title, body string, visible bool) error {
	if utf8.RuneCountInString(title) == 0 || utf8.RuneCountInString(title) > MaxArticleTitle {
		return fmt.Errorf("标题要 1..%d 个字", MaxArticleTitle)
	}
	if utf8.RuneCountInString(body) > MaxArticleBody {
		return fmt.Errorf("正文最长 %d 个字", MaxArticleBody)
	}
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE articles SET title=?, body=?, visible=?, updated_at=? WHERE id=?`,
		title, body, boolInt(visible), nowMS(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteArticle(ctx context.Context, id int64) (string, error) {
	var kind string
	if err := db.SQL.QueryRowContext(ctx, `SELECT kind FROM articles WHERE id=?`, id).Scan(&kind); err != nil {
		return "", notFound(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM articles WHERE id=?`, id); err != nil {
		return "", err
	}
	return kind, nil
}

// ArticleExcerpt 给列表用：去掉换行，截前 n 个字，不改变原文。
func ArticleExcerpt(body string, n int) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r", ""))
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	r := []rune(body)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
