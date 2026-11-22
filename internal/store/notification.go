package store

import (
	"context"
	"database/sql"
)

// 站内通知：一条一行，收件人只读自己的。通知不是权威数据，删掉帖子之后
// 那几条旧通知指向的链接会 404，但它们的正文仍然是"谁在什么时候说过什么"，
// 不值得为它加一条跨表的级联。
type Notification struct {
	ID          int64
	Kind        string // reply / mention
	ActorHandle string
	ActorName   string
	Summary     string
	Link        string
	CreatedAt   int64
	Read        bool
}

type NotificationInput struct {
	UserID      int64
	ActorID     int64
	ActorHandle string
	ActorName   string
	Kind        string
	Summary     string
	Link        string
}

// AddNotifications 一次写完一批（一条评论可能同时通知楼主与若干被 @ 的人），
// 全部落库或全部不落；通知失败不该让评论本身回滚，所以调用方只记录错误。
func (db *DB) AddNotifications(ctx context.Context, items []NotificationInput) error {
	if len(items) == 0 {
		return nil
	}
	now := nowMS()
	return db.InTx(ctx, func(tx *sql.Tx) error {
		for _, n := range items {
			if n.UserID == 0 || n.Summary == "" || n.Link == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO notifications (user_id, actor_id, actor_handle, actor_name, kind, summary, link, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				n.UserID, nullInt64(n.ActorID), n.ActorHandle, n.ActorName, n.Kind, n.Summary, n.Link, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *DB) CountNotificationsUnread(ctx context.Context, userID int64) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (db *DB) ListNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT id, kind, actor_handle, actor_name, summary, link, created_at, read_at IS NOT NULL
		 FROM notifications WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.ActorHandle, &n.ActorName,
			&n.Summary, &n.Link, &n.CreatedAt, &n.Read); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (db *DB) MarkNotificationsRead(ctx context.Context, userID int64) (int64, error) {
	res, err := db.SQL.ExecContext(ctx,
		`UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, nowMS(), userID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
