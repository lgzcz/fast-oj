package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxClipboards    = 50
	MaxClipboardName = 60
	MaxClipboardBody = 65536
)

type Clipboard struct {
	ID        int64
	Name      string
	Body      string
	CreatedAt int64
	UpdatedAt int64
}

func (db *DB) ListClipboards(ctx context.Context, userID int64) ([]Clipboard, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, name, body, created_at, updated_at FROM clipboards
		 WHERE user_id=? ORDER BY updated_at DESC, id DESC LIMIT ?`, userID, MaxClipboards)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Clipboard
	for rows.Next() {
		var c Clipboard
		if err := rows.Scan(&c.ID, &c.Name, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func validateClipboard(name, body string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxClipboardName {
		return "", fmt.Errorf("名字要 1..%d 个字", MaxClipboardName)
	}
	if utf8.RuneCountInString(body) > MaxClipboardBody {
		return "", fmt.Errorf("内容最长 %d 个字", MaxClipboardBody)
	}
	return name, nil
}

func (db *DB) CreateClipboard(ctx context.Context, userID int64, name, body string) (Clipboard, error) {
	name, err := validateClipboard(name, body)
	if err != nil {
		return Clipboard{}, err
	}
	var n int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM clipboards WHERE user_id=?`, userID).Scan(&n); err != nil {
		return Clipboard{}, err
	}
	if n >= MaxClipboards {
		return Clipboard{}, fmt.Errorf("最多存 %d 条剪贴板", MaxClipboards)
	}
	now := nowMS()
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO clipboards (user_id, name, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, body, now, now)
	if err != nil {
		return Clipboard{}, err
	}
	id, _ := res.LastInsertId()
	return Clipboard{ID: id, Name: name, Body: body, CreatedAt: now, UpdatedAt: now}, nil
}

func (db *DB) UpdateClipboard(ctx context.Context, id, userID int64, name, body string) error {
	name, err := validateClipboard(name, body)
	if err != nil {
		return err
	}
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE clipboards SET name=?, body=?, updated_at=? WHERE id=? AND user_id=?`,
		name, body, nowMS(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) DeleteClipboard(ctx context.Context, id, userID int64) (bool, error) {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM clipboards WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
