package store

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxChatName  = 60
	MaxChatBody  = 2000
	MaxChatRooms = 100
)

type ChatRoom struct {
	ID          int64
	Name        string
	OwnerID     int64
	OwnerHandle string
	OwnerName   string
	Messages    int
	CreatedAt   int64
}

type ChatMessage struct {
	ID        int64
	RoomID    int64
	UserID    int64
	Handle    string
	Name      string
	Color     string
	Body      string
	CreatedAt int64
}

func (db *DB) CreateChatRoom(ctx context.Context, ownerID int64, name string) (ChatRoom, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxChatName {
		return ChatRoom{}, fmt.Errorf("房间名要 1..%d 个字", MaxChatName)
	}
	var n int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_rooms`).Scan(&n); err != nil {
		return ChatRoom{}, err
	}
	if n >= MaxChatRooms {
		return ChatRoom{}, fmt.Errorf("房间最多 %d 个", MaxChatRooms)
	}
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO chat_rooms (name, owner_id, created_at) VALUES (?, ?, ?)`, name, ownerID, nowMS())
	if err != nil {
		return ChatRoom{}, err
	}
	id, _ := res.LastInsertId()
	return db.GetChatRoom(ctx, id)
}

func (db *DB) GetChatRoom(ctx context.Context, id int64) (ChatRoom, error) {
	var r ChatRoom
	err := db.SQL.QueryRowContext(ctx,
		`SELECT r.id, r.name, IFNULL(r.owner_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
		        (SELECT COUNT(*) FROM chat_messages m WHERE m.room_id=r.id), r.created_at
		 FROM chat_rooms r LEFT JOIN users u ON u.id=r.owner_id WHERE r.id=?`, id).
		Scan(&r.ID, &r.Name, &r.OwnerID, &r.OwnerHandle, &r.OwnerName, &r.Messages, &r.CreatedAt)
	return r, notFound(err)
}

func (db *DB) ListChatRooms(ctx context.Context, limit int) ([]ChatRoom, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT r.id, r.name, IFNULL(r.owner_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
		        (SELECT COUNT(*) FROM chat_messages m WHERE m.room_id=r.id), r.created_at
		 FROM chat_rooms r LEFT JOIN users u ON u.id=r.owner_id ORDER BY r.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatRoom
	for rows.Next() {
		var r ChatRoom
		if err := rows.Scan(&r.ID, &r.Name, &r.OwnerID, &r.OwnerHandle, &r.OwnerName, &r.Messages, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) DeleteChatRoom(ctx context.Context, id, ownerID int64) error {
	res, err := db.SQL.ExecContext(ctx,
		`DELETE FROM chat_rooms WHERE id=? AND owner_id=?`, id, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChatRoomAny is the moderator path: the caller has already checked the
// permission, so the ownership predicate would only get in the way.
func (db *DB) DeleteChatRoomAny(ctx context.Context, id int64) error {
	res, err := db.SQL.ExecContext(ctx, `DELETE FROM chat_rooms WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (db *DB) ListChatMessages(ctx context.Context, roomID, after int64, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := `SELECT m.id, m.room_id, IFNULL(m.user_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
		IFNULL(u.color, ''), m.body, m.created_at
		FROM chat_messages m LEFT JOIN users u ON u.id=m.user_id WHERE m.room_id=?`
	args := []any{roomID}
	if after > 0 {
		q += ` AND m.id>?`
		args = append(args, after)
	}
	q += ` ORDER BY m.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.RoomID, &m.UserID, &m.Handle, &m.Name, &m.Color, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) AddChatMessage(ctx context.Context, roomID, userID int64, body string) (ChatMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > MaxChatBody {
		return ChatMessage{}, fmt.Errorf("消息要 1..%d 个字", MaxChatBody)
	}
	var one int
	if err := db.SQL.QueryRowContext(ctx, `SELECT 1 FROM chat_rooms WHERE id=?`, roomID).Scan(&one); err != nil {
		return ChatMessage{}, notFound(err)
	}
	res, err := db.SQL.ExecContext(ctx,
		`INSERT INTO chat_messages (room_id, user_id, body, created_at) VALUES (?, ?, ?, ?)`,
		roomID, userID, body, nowMS())
	if err != nil {
		return ChatMessage{}, err
	}
	id, _ := res.LastInsertId()
	var m ChatMessage
	err = db.SQL.QueryRowContext(ctx,
		`SELECT m.id, m.room_id, IFNULL(m.user_id, 0), IFNULL(u.handle, ''), IFNULL(u.display_name, ''),
		        IFNULL(u.color, ''), m.body, m.created_at
		 FROM chat_messages m LEFT JOIN users u ON u.id=m.user_id WHERE m.id=?`, id).
		Scan(&m.ID, &m.RoomID, &m.UserID, &m.Handle, &m.Name, &m.Color, &m.Body, &m.CreatedAt)
	return m, err
}

func (db *DB) DeleteChatMessage(ctx context.Context, messageID, userID int64, moderator bool) (int64, bool, error) {
	var roomID int64
	var author int64
	err := db.SQL.QueryRowContext(ctx,
		`SELECT room_id, IFNULL(user_id, 0) FROM chat_messages WHERE id=?`, messageID).Scan(&roomID, &author)
	if err != nil {
		return 0, false, notFound(err)
	}
	if author != userID && !moderator {
		return roomID, false, nil
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM chat_messages WHERE id=?`, messageID); err != nil {
		return roomID, false, err
	}
	return roomID, true, nil
}
